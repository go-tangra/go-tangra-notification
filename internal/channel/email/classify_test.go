package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"testing"
)

// TestRetryable: temporary conditions (network, 4xx, timeouts) may succeed
// later; configuration, certificate, recipient and 5xx refusals never will.
func TestRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"4xx greylisting", fmt.Errorf("email: recipient: %w", &textproto.Error{Code: 451, Msg: "try later"}), true},
		{"421 closing", fmt.Errorf("email: greeting: %w", &textproto.Error{Code: 421, Msg: "busy"}), true},
		{"5xx recipient", fmt.Errorf("email: recipient: %w", &textproto.Error{Code: 550, Msg: "no such user"}), false},
		{"5xx auth", fmt.Errorf("email: auth: %w", &textproto.Error{Code: 535, Msg: "bad credentials"}), false},
		{"dial refused", fmt.Errorf("email: dial: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}), true},
		{"timeout", fmt.Errorf("email: greeting: %w", os.ErrDeadlineExceeded), true},
		{"context deadline", fmt.Errorf("email: dial: %w", context.DeadlineExceeded), true},
		{"eof", fmt.Errorf("email: data: %w", io.EOF), true},
		{"no starttls", fmt.Errorf("%w: relay offers no STARTTLS", ErrPlaintext), false},
		{"settings", fmt.Errorf("%w: host is required", ErrSettings), false},
		{"header", ErrHeader, false},
		{"auth over plaintext", ErrAuthTLS, false},
		{"bad recipient address", fmt.Errorf("%w: %w", ErrRecipient, errors.New("mail: missing @")), false},
		{"unknown authority", fmt.Errorf("email: starttls: %w", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), false},
		{"hostname mismatch", fmt.Errorf("email: starttls: %w", x509.HostnameError{Host: "10.0.0.1", Certificate: &x509.Certificate{}}), false},
		{"expired", fmt.Errorf("email: starttls: %w", x509.CertificateInvalidError{Reason: x509.Expired}), false},
		{"unknown", errors.New("something odd"), true},
	}
	for _, tc := range cases {
		if got := Retryable(tc.err); got != tc.want {
			t.Errorf("%s: retryable=%v", tc.name, got)
		}
	}
}
