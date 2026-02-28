package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
)

// decompressBody returns a copy of body decompressed according to contentEncoding.
// Supported: gzip, deflate, br (brotli). If encoding is empty or unsupported, body is returned unchanged.
func decompressBody(body []byte, contentEncoding string) []byte {
	if len(body) == 0 {
		return body
	}
	enc := strings.TrimSpace(strings.Split(contentEncoding, ",")[0])
	switch strings.ToLower(enc) {
	case "gzip":
		return decompressGzip(body)
	case "deflate":
		return decompressDeflate(body)
	case "br":
		return decompressBrotli(body)
	default:
		return body
	}
}

func decompressGzip(body []byte) []byte {
	r, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return body
	}
	return out
}

func decompressDeflate(body []byte) []byte {
	r := flate.NewReader(bytes.NewReader(body))
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return body
	}
	return out
}

func decompressBrotli(body []byte) []byte {
	r := brotli.NewReader(bytes.NewReader(body))
	out, err := io.ReadAll(r)
	if err != nil {
		return body
	}
	return out
}
