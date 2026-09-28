package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/traffickit/traffickit/internal/traffic"
)

// classify turns an upstream error into something a person can act on.
func classify(err error, target string) *traffic.Failure {
	f := &traffic.Failure{Code: "upstream", Detail: err.Error()}

	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var recordErr tls.RecordHeaderError

	switch {
	case errors.As(err, &dnsErr):
		f.Code = "dns"
		f.Message = fmt.Sprintf("Could not resolve %s.", dnsErr.Name)
		f.Hint = "Check the hostname for typos and that this machine's DNS works (try it in a browser without the proxy)."
	case errors.Is(err, syscall.ECONNREFUSED) || isWinErrno(err, 10061):
		f.Code = "refused"
		f.Message = fmt.Sprintf("%s refused the connection.", target)
		f.Hint = "Nothing is listening on that port, or a firewall is rejecting it. Check the server is running."
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || isTimeout(err):
		f.Code = "timeout"
		f.Message = fmt.Sprintf("Timed out talking to %s.", target)
		f.Hint = "The server is slow or unreachable from this network."
	case errors.Is(err, syscall.ECONNRESET) || isWinErrno(err, 10054) || errors.Is(err, io.ErrUnexpectedEOF):
		f.Code = "reset"
		f.Message = fmt.Sprintf("%s closed the connection unexpectedly.", target)
		f.Hint = "The server dropped the connection mid-exchange. It may have crashed or rejected the request."
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostnameErr), errors.As(err, &recordErr):
		f.Code = "tls"
		f.Message = fmt.Sprintf("TLS handshake with %s failed.", target)
		f.Hint = "The server's certificate isn't trusted by this machine, doesn't match the hostname, or the server doesn't speak TLS on that port."
	case errors.Is(err, context.Canceled):
		f.Code = "client_closed"
		f.Message = "The client gave up before the exchange finished."
	default:
		f.Message = fmt.Sprintf("Request to %s failed.", target)
	}
	return f
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Windows socket errors don't always map onto the syscall.E* constants.
func isWinErrno(err error, code uintptr) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && uintptr(errno) == code
}

// writeFailure answers the client with a plain-text explanation. The header
// lets clients and tests tell our errors from the upstream's.
func writeFailure(w http.ResponseWriter, status int, f *traffic.Failure) {
	var b strings.Builder
	b.WriteString("TrafficKit: ")
	b.WriteString(f.Message)
	b.WriteString("\n\n")
	if f.Hint != "" {
		b.WriteString(f.Hint)
		b.WriteString("\n\n")
	}
	b.WriteString(f.Detail)
	b.WriteString("\n")

	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-TrafficKit-Error", f.Code)
	h.Set("Connection", "close")
	w.WriteHeader(status)
	io.WriteString(w, b.String())
}
