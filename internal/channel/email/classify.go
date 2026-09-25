package email

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/textproto"
)

// ErrRecipient marks a recipient address that does not parse.
var ErrRecipient = errors.New("email: recipient")

// Retryable classifies a delivery failure (research D7): a temporary
// condition a later attempt may overcome (network errors, timeouts, SMTP
// 4xx replies) is retryable; a configuration problem (no STARTTLS offered,
// certificate not valid for the host, invalid settings, credentials over
// plaintext), an invalid recipient or an SMTP 5xx refusal is permanent.
// Unknown errors count as retryable so a message is never dropped for an
// unexpected transient failure; callers bound their retries.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return tp.Code >= 400 && tp.Code < 500
	}
	for _, permanent := range []error{ErrPlaintext, ErrSettings, ErrHeader, ErrAuthTLS, ErrRecipient} {
		if errors.Is(err, permanent) {
			return false
		}
	}
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &host) || errors.As(err, &invalid) {
		return false
	}
	return true
}
