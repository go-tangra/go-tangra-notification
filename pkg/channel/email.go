package channel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// EmailConfig holds SMTP connection settings.
type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	// TLS mode: "none", "starttls", "implicit"
	TLSMode string `json:"tls_mode"`
}

// String returns a safe representation of EmailConfig that never includes the password.
func (c EmailConfig) String() string {
	return fmt.Sprintf("EmailConfig{Host:%s, Port:%d, From:%s, TLSMode:%s}", c.Host, c.Port, c.From, c.TLSMode)
}

// GoString implements fmt.GoStringer to prevent password leaks via %#v.
func (c EmailConfig) GoString() string {
	return c.String()
}

// ParseEmailConfig parses a JSON config string into EmailConfig.
func ParseEmailConfig(configJSON string) (*EmailConfig, error) {
	var cfg EmailConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse email config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("email config: host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("email config: from is required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("email config: invalid from address: %w", err)
	}
	if cfg.TLSMode == "" {
		cfg.TLSMode = "starttls"
	}
	switch cfg.TLSMode {
	case "none", "starttls", "implicit":
		// valid modes
	default:
		return nil, fmt.Errorf("email config: invalid tls_mode %q (must be none, starttls, or implicit)", cfg.TLSMode)
	}
	return &cfg, nil
}

// EmailSender sends notifications via SMTP.
type EmailSender struct {
	config *EmailConfig
}

// NewEmailSender creates a new EmailSender from a JSON config.
func NewEmailSender(configJSON string) (*EmailSender, error) {
	cfg, err := ParseEmailConfig(configJSON)
	if err != nil {
		return nil, err
	}
	return &EmailSender{config: cfg}, nil
}

func (s *EmailSender) Type() string {
	return "EMAIL"
}

func (s *EmailSender) Send(ctx context.Context, msg *Message) error {
	// Validate recipient is a well-formed email address
	if strings.ContainsAny(msg.Recipient, "\r\n") {
		return fmt.Errorf("invalid recipient: contains newline characters")
	}

	addr := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))

	headers := map[string]string{
		"From":         encodeHeaderIfNeeded(s.config.From),
		"To":           encodeHeaderIfNeeded(msg.Recipient),
		"Subject":      encodeHeaderIfNeeded(msg.Subject),
		"MIME-Version": "1.0",
		"Content-Type": "text/html; charset=\"UTF-8\"",
	}

	var sb strings.Builder
	for k, v := range headers {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	sb.WriteString(msg.Body)

	body := sb.String()

	var auth smtp.Auth
	if s.config.Username != "" {
		auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	}

	switch s.config.TLSMode {
	case "none":
		return s.sendPlaintext(addr, auth, body, msg.Recipient)
	case "implicit":
		return s.sendImplicitTLS(addr, auth, body, msg.Recipient)
	default: // "starttls" — validated in ParseEmailConfig
		return s.sendSTARTTLS(addr, auth, body, msg.Recipient)
	}
}

// smtpDialTimeout is the maximum time to wait for an SMTP connection.
const smtpDialTimeout = 30 * time.Second

func (s *EmailSender) sendPlaintext(addr string, auth smtp.Auth, body, recipient string) error {
	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	c, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	return s.sendMailViaSMTPClient(c, body, recipient)
}

func (s *EmailSender) sendSTARTTLS(addr string, auth smtp.Auth, body, recipient string) error {
	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	c, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	// Verify the server advertises STARTTLS before attempting upgrade
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("starttls: server does not support STARTTLS, refusing to send credentials in plaintext")
	}

	tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
	if err := c.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	return s.sendMailViaSMTPClient(c, body, recipient)
}

func (s *EmailSender) sendImplicitTLS(addr string, auth smtp.Auth, body, recipient string) error {
	tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	c, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	return s.sendMailViaSMTPClient(c, body, recipient)
}

// sanitizeHeaderValue strips CR and LF characters to prevent SMTP header injection.
func sanitizeHeaderValue(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// isASCII returns true if s contains only ASCII characters.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

// encodeHeaderIfNeeded applies RFC 2047 MIME encoding to header values that contain
// non-ASCII characters. This prevents Postfix from requiring the SMTPUTF8 extension
// which many mail servers don't support.
func encodeHeaderIfNeeded(value string) string {
	value = sanitizeHeaderValue(value)
	if isASCII(value) {
		return value
	}

	// For email addresses with display names like "Имя <user@example.com>",
	// only encode the display name part.
	if addr, err := mail.ParseAddress(value); err == nil {
		encoded := mime.QEncoding.Encode("UTF-8", addr.Name)
		return fmt.Sprintf("%s <%s>", encoded, addr.Address)
	}

	return mime.QEncoding.Encode("UTF-8", value)
}

func (s *EmailSender) sendMailViaSMTPClient(c *smtp.Client, body, recipient string) error {
	if err := c.Mail(s.config.From); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	return c.Quit()
}
