package bodyutil

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

const sample = `{"hello":"world","n":[1,2,3]}`

func compress(t *testing.T, mk func(io.Writer) io.WriteCloser, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := mk(&buf)
	if _, err := w.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeCodings(t *testing.T) {
	cases := map[string]func(io.Writer) io.WriteCloser{
		"gzip":    func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) },
		"deflate": func(w io.Writer) io.WriteCloser { return zlib.NewWriter(w) },
		"br":      func(w io.Writer) io.WriteCloser { return brotli.NewWriter(w) },
		"zstd": func(w io.Writer) io.WriteCloser {
			zw, _ := zstd.NewWriter(w)
			return zw
		},
	}
	for enc, mk := range cases {
		t.Run(enc, func(t *testing.T) {
			got, err := Decode(enc, compress(t, mk, []byte(sample)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != sample {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestDecodeRawDeflate(t *testing.T) {
	raw := compress(t, func(w io.Writer) io.WriteCloser {
		fw, _ := flate.NewWriter(w, flate.DefaultCompression)
		return fw
	}, []byte(sample))
	got, err := Decode("deflate", raw)
	if err != nil || string(got) != sample {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeStacked(t *testing.T) {
	inner := compress(t, func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }, []byte(sample))
	outer := compress(t, func(w io.Writer) io.WriteCloser { return brotli.NewWriter(w) }, inner)
	got, err := Decode("gzip, br", outer)
	if err != nil || string(got) != sample {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeIdentity(t *testing.T) {
	got, err := Decode("", []byte(sample))
	if err != nil || string(got) != sample {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecodeUnknown(t *testing.T) {
	if _, err := Decode("compress", []byte("x")); err == nil {
		t.Fatal("expected error for unsupported coding")
	}
}

func TestDecodeBombIsCapped(t *testing.T) {
	bomb := compress(t, func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) },
		make([]byte, MaxDecoded+1024))
	if _, err := Decode("gzip", bomb); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}
