package proxy

import (
	"bufio"
	"bytes"
	"net/http"
	"testing"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	good := &http.Request{
		Header: http.Header{
			"Upgrade":               []string{"websocket"},
			"Connection":            []string{"Upgrade"},
			"Sec-Websocket-Key":     []string{"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Version": []string{"13"},
		},
	}
	if !isWebSocketUpgrade(good) {
		t.Fatal("expected upgrade")
	}
	noKey := &http.Request{Header: good.Header.Clone()}
	noKey.Header.Del("Sec-Websocket-Key")
	if isWebSocketUpgrade(noKey) {
		t.Fatal("expected false without key")
	}
}

func TestIsWebSocketAcceptResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header: http.Header{
			"Upgrade":                  []string{"websocket"},
			"Sec-Websocket-Accept":     []string{"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
			"Sec-Websocket-Extensions": []string{"permessage-deflate"},
		},
	}
	if !isWebSocketAcceptResponse(resp) {
		t.Fatal("expected accept")
	}
	resp.StatusCode = 200
	if isWebSocketAcceptResponse(resp) {
		t.Fatal("expected false for 200")
	}
}

func TestIsH2ExtendedWebSocket(t *testing.T) {
	req := &http.Request{
		Method:     http.MethodConnect,
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     http.Header{":protocol": []string{"websocket"}},
	}
	if !isH2ExtendedWebSocket(req) {
		t.Fatal("expected h2 extended ws")
	}
	req.Header = http.Header{"Protocol": []string{"websocket"}}
	if !isH2ExtendedWebSocket(req) {
		t.Fatal("expected Protocol fallback")
	}
	req.ProtoMajor = 1
	if isH2ExtendedWebSocket(req) {
		t.Fatal("expected false for h1")
	}
}

func TestWriteReadWSFrameMaskedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello")
	if err := writeWSFrame(&buf, true, true, 1, payload); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(&buf)
	fin, op, got, err := readWSFrame(br, true)
	if err != nil {
		t.Fatal(err)
	}
	if !fin || op != 1 || string(got) != "hello" {
		t.Fatalf("fin=%v op=%d got=%q", fin, op, got)
	}
}

func TestWriteReadWSFrameUnmaskedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{0, 1, 2, 255}
	if err := writeWSFrame(&buf, false, true, 2, payload); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(&buf)
	fin, op, got, err := readWSFrame(br, false)
	if err != nil {
		t.Fatal(err)
	}
	if !fin || op != 2 || !bytes.Equal(got, payload) {
		t.Fatalf("fin=%v op=%d got=%v", fin, op, got)
	}
}
