package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/muxover/snare/v2/capture"
)

const maxWSFrameBody = 8 << 20

func isWebSocketUpgrade(req *http.Request) bool {
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return false
	}
	if req.Header.Get("Sec-WebSocket-Key") == "" {
		return false
	}
	for _, v := range req.Header.Values("Connection") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return true
			}
		}
	}
	return false
}

func isWebSocketAcceptResponse(resp *http.Response) bool {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return false
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return resp.Header.Get("Sec-WebSocket-Accept") != ""
}

func isH2ExtendedWebSocket(req *http.Request) bool {
	if req.ProtoMajor != 2 || req.Method != http.MethodConnect {
		return false
	}
	p := req.Header.Get(":protocol")
	if p == "" {
		p = req.Header.Get("Protocol")
	}
	return strings.EqualFold(strings.TrimSpace(p), "websocket")
}

func copyWebSocketClientHeaders(from, to *http.Request) {
	for k, vv := range from.Header {
		kl := strings.ToLower(k)
		if strings.HasPrefix(kl, "sec-websocket") || kl == "origin" || kl == "host" || kl == "user-agent" || kl == "cookie" {
			for _, v := range vv {
				to.Header.Add(k, v)
			}
		}
	}
}

func readWSFrame(br *bufio.Reader, expectMasked bool) (fin bool, opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, errors.New("ws: unsupported rsv bits")
	}
	opcode = h[0] & 0x0f
	masked := h[1]&0x80 != 0
	if masked != expectMasked {
		return false, 0, nil, errors.New("ws: unexpected mask bit")
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var x [2]byte
		if _, err := io.ReadFull(br, x[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(x[:]))
	case 127:
		var x [8]byte
		if _, err := io.ReadFull(br, x[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(x[:])
	}
	if n > maxWSFrameBody {
		return false, 0, nil, errors.New("ws: frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(br, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(br, payload); err != nil {
			return false, 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return fin, opcode, payload, nil
}

func writeWSFrame(w io.Writer, masked bool, fin bool, opcode byte, payload []byte) error {
	var b0 byte = opcode & 0x0f
	if fin {
		b0 |= 0x80
	}
	n := len(payload)
	var b1 byte
	if masked {
		b1 = 0x80
	}
	var lenExtra []byte
	switch {
	case n < 126:
		b1 |= byte(n)
	case n <= 0xffff:
		b1 |= 126
		var x [2]byte
		binary.BigEndian.PutUint16(x[:], uint16(n))
		lenExtra = x[:]
	default:
		b1 |= 127
		var x [8]byte
		binary.BigEndian.PutUint64(x[:], uint64(n))
		lenExtra = x[:]
	}
	if _, err := w.Write([]byte{b0, b1}); err != nil {
		return err
	}
	if len(lenExtra) > 0 {
		if _, err := w.Write(lenExtra); err != nil {
			return err
		}
	}
	out := payload
	if masked {
		var mk [4]byte
		if _, err := rand.Read(mk[:]); err != nil {
			return err
		}
		if _, err := w.Write(mk[:]); err != nil {
			return err
		}
		scratch := make([]byte, len(payload))
		for i := range payload {
			scratch[i] = payload[i] ^ mk[i%4]
		}
		out = scratch
	}
	if len(out) > 0 {
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) relayWebSocketCapture(
	cap *capture.Capture,
	clientBR *bufio.Reader, clientConn net.Conn,
	originBR *bufio.Reader, originConn net.Conn,
) {
	h.relayWebSocketRFC6455(cap, clientBR, clientConn, originBR, originConn, true, func() {
		_ = clientConn.Close()
		_ = originConn.Close()
	})
}

func (h *Handler) relayWebSocketRFC6455(
	cap *capture.Capture,
	clientBR *bufio.Reader,
	clientWrite io.Writer,
	originBR *bufio.Reader,
	originConn net.Conn,
	readClientMasked bool,
	shutdown func(),
) {
	cw := bufio.NewWriter(clientWrite)
	ow := bufio.NewWriter(originConn)

	var mu sync.Mutex
	var frames []capture.WSFrame
	t0 := time.Now()

	var wg sync.WaitGroup
	var closeOnce sync.Once
	doShutdown := func() {
		closeOnce.Do(shutdown)
	}

	relay := func(from *bufio.Reader, to *bufio.Writer, readMasked, writeMasked bool, dir string) {
		defer wg.Done()
		for {
			fin, op, payload, err := readWSFrame(from, readMasked)
			if err != nil {
				doShutdown()
				return
			}
			mu.Lock()
			frames = append(frames, capture.WSFrame{
				Timestamp: time.Now(),
				Direction: dir,
				Opcode:    int(op),
				Payload:   append(capture.BodyBytes(nil), payload...),
			})
			mu.Unlock()
			if err := writeWSFrame(to, writeMasked, fin, op, payload); err != nil {
				doShutdown()
				return
			}
			if err := to.Flush(); err != nil {
				doShutdown()
				return
			}
			if op == 8 {
				doShutdown()
				return
			}
		}
	}

	wg.Add(2)
	go relay(clientBR, ow, readClientMasked, true, "c2s")
	go relay(originBR, cw, false, false, "s2c")
	wg.Wait()

	cap.Duration = time.Since(t0)
	cap.WebSocket = &capture.WebSocketCapture{Frames: frames}
	h.addCapture(cap)
}

type bodyReaderWithCtx struct {
	r   io.Reader
	ctx context.Context
}

func (b bodyReaderWithCtx) Read(p []byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	default:
	}
	return b.r.Read(p)
}

type flushResponseWriter struct {
	http.ResponseWriter
}

func (f *flushResponseWriter) Write(p []byte) (int, error) {
	n, err := f.ResponseWriter.Write(p)
	if err != nil {
		return n, err
	}
	if fl, ok := f.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}
