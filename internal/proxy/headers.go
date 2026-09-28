package proxy

import (
	"net/http"
	"net/textproto"
	"strings"
)

// Hop-by-hop headers apply to a single connection and must not be forwarded
// (RFC 9110 §7.6.1).
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard, sent by older clients
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopHeaders(h http.Header) {
	for _, v := range h["Connection"] {
		for name := range strings.SplitSeq(v, ",") {
			if name = textproto.TrimString(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
}

// upgradeType returns the protocol a request asks to switch to, if any.
func upgradeType(h http.Header) string {
	for _, v := range h["Connection"] {
		for tok := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(textproto.TrimString(tok), "upgrade") {
				return h.Get("Upgrade")
			}
		}
	}
	return ""
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
