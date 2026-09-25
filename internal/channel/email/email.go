// Package email delivers through a mail relay with net/smtp: implicit TLS on
// port 465, STARTTLS otherwise, plaintext only when the service allows it
// (development), PLAIN authentication only over TLS. Every header is checked
// against injection and provider errors never echo credentials (SR-001,
// SR-003; research R2).
package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

// Message is one rendered message.
type Message struct {
	To       string
	Subject  string
	HTMLBody string // empty for text-only channels
	TextBody string
}

// Settings are the email provider settings (contracts EmailSettings).
type Settings struct {
	Host     string
	Port     int
	TLS      string // implicit | starttls | none
	Username string
	Password string
	From     string
	FromAddr string // bare address parsed from From
	ReplyTo  string
}

// SecretFields of the email settings.
var SecretFields = []string{"password"}

// Errors.
var (
	ErrSettings  = errors.New("email: invalid settings")
	ErrHeader    = errors.New("email: header contains a line break or control character")
	ErrPlaintext = errors.New("email: plaintext delivery is not allowed")
	ErrAuthTLS   = errors.New("email: credentials require TLS")
)

// Provider is the email provider.
type Provider struct {
	AllowPlaintext bool
	DialTimeout    time.Duration
	// Dialer overrides the network dial (tests).
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig overrides the client TLS configuration (tests: test CA).
	TLSConfig *tls.Config
}

// Type implements channel.Provider.
func (p *Provider) Type() string { return "email" }

// Secret implements channel.Provider.
func (p *Provider) Secret() []string { return SecretFields }

// Parse decodes and validates settings.
func (p *Provider) Parse(s sealed.Settings) (Settings, error) {
	out := Settings{TLS: "starttls"}
	str := func(k string) (string, bool, error) {
		v, ok := s[k]
		if !ok || v == nil {
			return "", false, nil
		}
		sv, isStr := v.(string)
		if !isStr {
			return "", true, fmt.Errorf("%w: %s must be a string", ErrSettings, k)
		}
		return sv, true, nil
	}
	var err error
	if out.Host, _, err = str("host"); err != nil {
		return out, err
	}
	if out.Host == "" || len(out.Host) > 253 || strings.ContainsAny(out.Host, " /\\\r\n") {
		return out, fmt.Errorf("%w: host is required", ErrSettings)
	}
	switch v := s["port"].(type) {
	case float64:
		out.Port = int(v)
	case int:
		out.Port = v
	default:
		return out, fmt.Errorf("%w: port is required", ErrSettings)
	}
	if out.Port < 1 || out.Port > 65535 {
		return out, fmt.Errorf("%w: port out of range", ErrSettings)
	}
	if v, ok, err := str("tls"); err != nil {
		return out, err
	} else if ok && v != "" {
		out.TLS = v
	}
	switch out.TLS {
	case "implicit", "starttls":
	case "none":
		if !p.AllowPlaintext {
			return out, fmt.Errorf("%w: tls none is not allowed", ErrSettings)
		}
	default:
		return out, fmt.Errorf("%w: tls must be implicit, starttls or none", ErrSettings)
	}
	if out.Username, _, err = str("username"); err != nil {
		return out, err
	}
	if out.Password, _, err = str("password"); err != nil {
		return out, err
	}
	if (out.Username == "") != (out.Password == "") {
		return out, fmt.Errorf("%w: username and password go together", ErrSettings)
	}
	if out.Username != "" && out.TLS == "none" {
		return out, ErrAuthTLS
	}
	if out.From, _, err = str("from"); err != nil {
		return out, err
	}
	fromAddr, err := mail.ParseAddress(out.From)
	if err != nil || len(out.From) > 320 {
		return out, fmt.Errorf("%w: from must be one address", ErrSettings)
	}
	out.FromAddr = fromAddr.Address
	if out.ReplyTo, _, err = str("reply_to"); err != nil {
		return out, err
	}
	if out.ReplyTo != "" {
		if _, err := mail.ParseAddress(out.ReplyTo); err != nil || len(out.ReplyTo) > 320 {
			return out, fmt.Errorf("%w: reply_to must be one address", ErrSettings)
		}
	}
	for _, h := range []string{out.Host, out.Username, out.From, out.ReplyTo} {
		if !HeaderSafe(h) {
			return out, ErrHeader
		}
	}
	return out, nil
}

// Validate implements channel.Provider.
func (p *Provider) Validate(s sealed.Settings) error {
	_, err := p.Parse(s)
	return err
}

// HeaderSafe reports whether v may appear in a header (no CR/LF, no control characters).
func HeaderSafe(v string) bool {
	for _, r := range v {
		if r == '\r' || r == '\n' || r == 0 || (r < 0x20 && r != '\t') || r == 0x7f {
			return false
		}
	}
	return true
}

// Send implements channel.Provider.
func (p *Provider) Send(ctx context.Context, settings sealed.Settings, msg Message) error {
	cfg, err := p.Parse(settings)
	if err != nil {
		return err
	}
	if !HeaderSafe(msg.To) || !HeaderSafe(msg.Subject) {
		return ErrHeader
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecipient, err)
	}
	body, _ := Build(cfg.From, msg.To, cfg.ReplyTo, msg.Subject, msg.TextBody, msg.HTMLBody, time.Now()) // inputs validated above
	timeout := p.DialTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	dial := p.Dialer
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	conn, err := dial(dctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial: %w", err)
	}
	tlsCfg := p.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	} else {
		tlsCfg = tlsCfg.Clone()
		tlsCfg.ServerName = cfg.Host
	}
	if cfg.TLS == "implicit" {
		conn = tls.Client(conn, tlsCfg)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("email: greeting: %w", err)
	}
	defer c.Close()
	if cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%w: relay offers no STARTTLS", ErrPlaintext)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}
	if cfg.Username != "" { // Parse refused credentials over tls none
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := c.Mail(cfg.FromAddr); err != nil {
		return fmt.Errorf("email: from: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("email: recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	_, werr := w.Write(body)
	if cerr := w.Close(); cerr != nil || werr != nil {
		return fmt.Errorf("email: send: %w", errors.Join(werr, cerr))
	}
	return c.Quit()
}
