package email

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"mime"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

// fakeSMTP is a minimal relay: EHLO, STARTTLS (when cert set), AUTH PLAIN,
// MAIL, RCPT, DATA, QUIT. It records the last message and can refuse steps.
type fakeSMTP struct {
	ln       net.Listener
	cert     *tls.Certificate
	implicit bool
	refuseAt string // "greeting" | "auth" | "rcpt" | "data"
	wantUser string
	mu       sync.Mutex
	last     string
	authed   bool
	tlsUsed  bool
}

func newFake(t *testing.T, cert *tls.Certificate, implicit bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, cert: cert, implicit: implicit}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) addr() (string, int) {
	a := f.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	f.mu.Lock()
	refuseAt := f.refuseAt
	f.mu.Unlock()
	if f.implicit {
		c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*f.cert}, MinVersion: tls.VersionTLS12})
		f.mu.Lock()
		f.tlsUsed = true
		f.mu.Unlock()
	}
	r := bufio.NewReader(c)
	w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	if refuseAt == "greeting" {
		w("554 go away")
		return
	}
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if f.cert != nil && !f.implicit && !f.isTLS(c) {
				w("250-fake")
				w("250-STARTTLS")
				w("250 AUTH PLAIN")
			} else {
				w("250-fake")
				w("250 AUTH PLAIN")
			}
		case cmd == "STARTTLS":
			w("220 ready")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*f.cert}, MinVersion: tls.VersionTLS12})
			if err := tc.Handshake(); err != nil {
				return
			}
			c = tc
			r = bufio.NewReader(c)
			w = func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
			f.mu.Lock()
			f.tlsUsed = true
			f.mu.Unlock()
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			if refuseAt == "auth" {
				w("535 authentication failed for " + strings.TrimSpace(line[10:]))
				continue
			}
			f.mu.Lock()
			f.authed = true
			f.mu.Unlock()
			w("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			if refuseAt == "mail" {
				w("553 sender refused")
				continue
			}
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			if refuseAt == "rcpt" {
				w("550 no such user")
				continue
			}
			w("250 ok")
		case cmd == "DATA":
			if refuseAt == "data" {
				w("451 try later")
				continue
			}
			w("354 go")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				sb.WriteString(l)
			}
			f.mu.Lock()
			f.last = sb.String()
			f.mu.Unlock()
			if refuseAt == "end" {
				w("554 rejected after data")
				continue
			}
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		default:
			w("500 unknown")
		}
	}
}

func (f *fakeSMTP) isTLS(c net.Conn) bool { _, ok := c.(*tls.Conn); return ok }

func (f *fakeSMTP) message() string { f.mu.Lock(); defer f.mu.Unlock(); return f.last }

func testCert(t *testing.T) (*tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

func settings(host string, port int, extra map[string]any) sealed.Settings {
	s := sealed.Settings{"host": host, "port": float64(port), "from": "Freya <noreply@example.org>"}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func TestParseSettings(t *testing.T) {
	p := &Provider{}
	good, err := p.Parse(settings("smtp.example.org", 587, map[string]any{"username": "u", "password": "NOTIF-MARKER-PW-x", "reply_to": "ops@example.org"}))
	if err != nil || good.TLS != "starttls" || good.Port != 587 || good.Username != "u" {
		t.Fatalf("%+v %v", good, err)
	}
	if _, err := p.Parse(settings("smtp.example.org", 25, map[string]any{"port": 25})); err != nil { // int port
		t.Fatal(err)
	}
	bad := []sealed.Settings{
		{"port": float64(25), "from": "a@b.c"},
		settings("", 25, nil),
		settings("h", 0, nil),
		settings("h", 70000, nil),
		{"host": "h", "from": "a@b.c"},
		settings("h", 25, map[string]any{"tls": "none"}),
		settings("h", 25, map[string]any{"tls": "sslv3"}),
		settings("h", 25, map[string]any{"username": "u"}),
		settings("h", 25, map[string]any{"password": "p"}),
		settings("h", 25, map[string]any{"from": "not an address"}),
		settings("h", 25, map[string]any{"from": "a@b.c, d@e.f"}),
		settings("h", 25, map[string]any{"reply_to": "nope"}),
		settings("h", 25, map[string]any{"host": float64(1)}),
		settings("h", 25, map[string]any{"from": float64(1)}),
		settings("h", 25, map[string]any{"tls": float64(1)}),
		settings("h", 25, map[string]any{"username": float64(1), "password": "p"}),
		settings("h", 25, map[string]any{"username": "u", "password": float64(1)}),
		settings("h", 25, map[string]any{"reply_to": float64(1)}),
		settings("h\r\nX: y", 25, nil),
		settings("h", 25, map[string]any{"username": "u\nx", "password": "p"}),
	}
	for i, s := range bad {
		if _, err := p.Parse(s); err == nil {
			t.Errorf("case %d accepted: %v", i, s)
		}
	}
	// Plaintext relays need the service opt-in; credentials still need TLS.
	pp := &Provider{AllowPlaintext: true}
	if _, err := pp.Parse(settings("h", 25, map[string]any{"tls": "none"})); err != nil {
		t.Fatal(err)
	}
	if _, err := pp.Parse(settings("h", 25, map[string]any{"tls": "none", "username": "u", "password": "p"})); !errors.Is(err, ErrAuthTLS) {
		t.Fatalf("auth over plaintext: %v", err)
	}
	if err := p.Validate(settings("h", 25, nil)); err != nil || p.Type() != "email" || len(p.Secret()) != 1 {
		t.Fatal("validate/type/secret")
	}
}

func TestHeaderSafeAndBuild(t *testing.T) {
	for _, bad := range []string{"a\r\nb", "a\nb", "a\x00b", "a\x7fb", "\x01"} {
		if HeaderSafe(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !HeaderSafe("plain subject\twith tab") {
		t.Fatal("tab refused")
	}
	if _, err := Build("a@b.c", "x@y.z\r\nBcc: e@v.il", "", "s", "t", "", time.Now()); !errors.Is(err, ErrHeader) {
		t.Fatalf("injection: %v", err)
	}
	if _, err := Build("a@b.c", "not an address", "", "s", "t", "", time.Now()); err == nil {
		t.Fatal("bad recipient accepted")
	}
	msg, err := Build("Freya <a@b.c>", "x@y.z", "r@b.c", "Hällo =?", "", "<p>Hi <b>there</b> &amp; you</p><br>line", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{"From: Freya <a@b.c>\r\n", "To: x@y.z\r\n", "Reply-To: r@b.c\r\n", "Subject: =?utf-8?q?", "Message-ID: <", "@b.c>", "multipart/alternative", "text/plain", "text/html", "Hi there & you", "<b>there</b>", "MIME-Version: 1.0"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	plain, _ := Build("a@b.c", "x@y.z", "", "s", "just text", "", time.Unix(0, 0))
	if strings.Contains(string(plain), "multipart") || !strings.Contains(string(plain), "just text") {
		t.Fatalf("plain message %s", plain)
	}
	if got := TextFromHTML("<div>a</div><p>b &lt; c</p>"); got != "a\nb < c" {
		t.Fatalf("text from html %q", got)
	}
	// Long subjects fold into a chain of encoded words that decodes back unchanged (RFC 5322 line limits).
	long := strings.Repeat("Ünïcode subject ", 40) + "end"
	folded := encodeSubject(long)
	for _, line := range strings.Split(folded, "\r\n") {
		if len(line) > 78 {
			t.Fatalf("folded line too long: %q", line)
		}
	}
	dec := new(mime.WordDecoder)
	got, err := dec.DecodeHeader(strings.ReplaceAll(folded, "\r\n ", " "))
	if err != nil || got != long {
		t.Fatalf("decode %v %q", err, got)
	}
	if encodeSubject("short") != "short" {
		t.Fatal("short subject must stay plain")
	}
}

func TestSendVariants(t *testing.T) {
	cert, pool := testCert(t)
	ctx := context.Background()
	msg := Message{To: "Alice <alice@example.org>", Subject: "Hello", HTMLBody: "<p>Hi Alice</p>"}
	t.Run("starttls with auth", func(t *testing.T) {
		f := newFake(t, cert, false)
		f.mu.Lock()
		f.wantUser = "u"
		f.mu.Unlock()
		h, port := f.addr()
		p := &Provider{TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DialTimeout: 5 * time.Second}
		if err := p.Send(ctx, settings(h, port, map[string]any{"username": "u", "password": "NOTIF-MARKER-PW-1"}), msg); err != nil {
			t.Fatal(err)
		}
		if !f.tlsUsed || !f.authed || !strings.Contains(f.message(), "Hi Alice") {
			t.Fatalf("tls=%v authed=%v msg=%q", f.tlsUsed, f.authed, f.message())
		}
	})
	t.Run("implicit tls on 465-style port", func(t *testing.T) {
		f := newFake(t, cert, true)
		h, port := f.addr()
		p := &Provider{TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
		if err := p.Send(ctx, settings(h, port, map[string]any{"tls": "implicit"}), msg); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.message(), "Subject: Hello") {
			t.Fatal("message not delivered")
		}
	})
	t.Run("plaintext only when allowed", func(t *testing.T) {
		f := newFake(t, nil, false)
		h, port := f.addr()
		p := &Provider{}
		if err := p.Send(ctx, settings(h, port, map[string]any{"tls": "starttls"}), msg); !errors.Is(err, ErrPlaintext) {
			t.Fatalf("no STARTTLS offered: %v", err)
		}
		if err := p.Send(ctx, settings(h, port, map[string]any{"tls": "none"}), msg); !errors.Is(err, ErrSettings) {
			t.Fatalf("tls none refused by settings: %v", err)
		}
		pp := &Provider{AllowPlaintext: true}
		if err := pp.Send(ctx, settings(h, port, map[string]any{"tls": "none"}), msg); err != nil {
			t.Fatal(err)
		}
		if err := pp.Send(ctx, settings(h, port, map[string]any{"tls": "none", "username": "u", "password": "p"}), msg); !errors.Is(err, ErrAuthTLS) {
			t.Fatalf("auth without tls: %v", err)
		}
	})
	t.Run("provider refusals surface without credentials", func(t *testing.T) {
		for _, at := range []string{"greeting", "auth", "mail", "rcpt", "data", "end"} {
			f := newFake(t, cert, false)
			f.mu.Lock()
			f.refuseAt = at
			f.mu.Unlock()
			h, port := f.addr()
			p := &Provider{TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
			err := p.Send(ctx, settings(h, port, map[string]any{"username": "u", "password": "NOTIF-MARKER-PW-2"}), msg)
			if err == nil {
				t.Fatalf("%s: no error", at)
			}
			if strings.Contains(err.Error(), "NOTIF-MARKER-PW-2") && at != "auth" {
				t.Fatalf("%s: credential in error %v", at, err)
			}
		}
	})
	t.Run("bad inputs and dial failures", func(t *testing.T) {
		p := &Provider{}
		if err := p.Send(ctx, sealed.Settings{}, msg); err == nil {
			t.Fatal("bad settings accepted")
		}
		s := settings("127.0.0.1", 1, map[string]any{"tls": "none"})
		pp := &Provider{AllowPlaintext: true, DialTimeout: time.Second}
		if err := pp.Send(ctx, s, Message{To: "a@b.c\r\nBcc: x", Subject: "s"}); !errors.Is(err, ErrHeader) {
			t.Fatalf("header injection: %v", err)
		}
		if err := pp.Send(ctx, s, Message{To: "not-an-address", Subject: "s"}); err == nil {
			t.Fatal("bad recipient accepted")
		}
		pp.Dialer = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("connection refused") }
		if err := pp.Send(ctx, s, msg); err == nil || !strings.Contains(err.Error(), "dial") {
			t.Fatalf("dial: %v", err)
		}
		pp.Dialer = nil
		if err := pp.Send(ctx, s, msg); err == nil {
			t.Fatal("closed port accepted")
		}
		// STARTTLS handshake failure (server cert not trusted).
		f := newFake(t, cert, false)
		h, port := f.addr()
		if err := (&Provider{}).Send(ctx, settings(h, port, nil), msg); err == nil || !strings.Contains(err.Error(), "starttls") {
			t.Fatalf("untrusted cert: %v", err)
		}
	})
}
