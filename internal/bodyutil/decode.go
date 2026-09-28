// Package bodyutil undoes HTTP content codings for display.
package bodyutil

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// MaxDecoded caps decoder output. A few KB of gzip can expand to gigabytes.
const MaxDecoded = 64 << 20

var ErrTooLarge = fmt.Errorf("decoded body exceeds %d MiB", MaxDecoded>>20)

// Decode removes the codings listed in a Content-Encoding value, last applied
// first. It returns the input unchanged for identity or an empty encoding.
// Unknown codings are an error.
func Decode(encoding string, data []byte) ([]byte, error) {
	codings := strings.Split(encoding, ",")
	out := data
	for i := len(codings) - 1; i >= 0; i-- {
		c := strings.ToLower(strings.TrimSpace(codings[i]))
		if c == "" || c == "identity" {
			continue
		}
		var err error
		out, err = decodeOne(c, out)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c, err)
		}
	}
	return out, nil
}

func decodeOne(coding string, data []byte) ([]byte, error) {
	src := bytes.NewReader(data)
	var r io.Reader
	switch coding {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(src)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	case "deflate":
		// "deflate" is supposed to be zlib-wrapped, but plenty of servers send
		// raw deflate. Check for a zlib header and fall back.
		if looksZlib(data) {
			zr, err := zlib.NewReader(src)
			if err != nil {
				return nil, err
			}
			defer zr.Close()
			r = zr
		} else {
			fr := flate.NewReader(src)
			defer fr.Close()
			r = fr
		}
	case "br":
		r = brotli.NewReader(src)
	case "zstd":
		zr, err := zstd.NewReader(src, zstd.WithDecoderMaxMemory(MaxDecoded))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	default:
		return nil, errors.New("unsupported content coding")
	}

	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, MaxDecoded+1))
	if n > MaxDecoded {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func looksZlib(b []byte) bool {
	return len(b) >= 2 && b[0]&0x0f == 8 && (uint16(b[0])<<8|uint16(b[1]))%31 == 0
}
