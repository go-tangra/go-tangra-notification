//go:build integration

package integration

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Relay is an in-process SMTP relay for the platform channel tests: it
// offers STARTTLS with a certificate for "localhost" issued by its own test
// CA (or no STARTTLS at all) and records every accepted message and every
// command it saw.
type Relay struct {
	Port     int
	CA       *x509.CertPool
	ln       net.Listener
	cert     *tls.Certificate
	mu       sync.Mutex
	messages []RelayMessage
	commands []string
}

// RelayMessage is one accepted message.
type RelayMessage struct {
	From, To string
	Data     string
	TLS      bool
}

// StartRelay listens on 127.0.0.1; starttls selects whether the relay offers STARTTLS.
func StartRelay(t *testing.T, starttls bool) *Relay {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "relay test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{CA: x509.NewCertPool()}
	r.CA.AddCert(caCert)
	if starttls {
		r.cert = &tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}
	}
	if r.ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	r.Port = r.ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := r.ln.Accept()
			if err != nil {
				return
			}
			go r.serve(c)
		}
	}()
	t.Cleanup(func() { _ = r.ln.Close() })
	return r
}

// Messages returns the accepted messages.
func (r *Relay) Messages() []RelayMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RelayMessage(nil), r.messages...)
}

// Commands returns the verbs seen, in order.
func (r *Relay) Commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

// WaitMessage waits up to 20 s for a message to an address.
func (r *Relay) WaitMessage(t *testing.T, to string) RelayMessage {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range r.Messages() {
			if m.To == to {
				return m
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("relay: no message for %s", to)
	return RelayMessage{}
}

func (r *Relay) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	rd := bufio.NewReader(c)
	w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	w("220 localhost ESMTP test relay")
	var msg RelayMessage
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		r.mu.Lock()
		r.commands = append(r.commands, verb)
		r.mu.Unlock()
		switch {
		case verb == "EHLO" || verb == "HELO":
			if r.cert != nil && !msg.TLS {
				w("250-localhost")
				w("250-STARTTLS")
				w("250 8BITMIME")
			} else {
				w("250-localhost")
				w("250 8BITMIME")
			}
		case verb == "STARTTLS" && r.cert != nil:
			w("220 go ahead")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*r.cert}, MinVersion: tls.VersionTLS12})
			if err := tc.Handshake(); err != nil {
				return
			}
			c, rd, msg = tc, bufio.NewReader(tc), RelayMessage{TLS: true}
			w = func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		case verb == "MAIL":
			msg.From = addr(line)
			w("250 ok")
		case verb == "RCPT":
			msg.To = addr(line)
			w("250 ok")
		case verb == "DATA":
			w("354 end with .")
			var b strings.Builder
			for {
				l, err := rd.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			msg.Data = b.String()
			r.mu.Lock()
			r.messages = append(r.messages, msg)
			r.mu.Unlock()
			msg = RelayMessage{TLS: msg.TLS}
			w("250 queued")
		case verb == "RSET" || verb == "NOOP":
			w("250 ok")
		case verb == "QUIT":
			w("221 bye")
			return
		default:
			w("502 not implemented")
		}
	}
}

func addr(line string) string {
	i, j := strings.IndexByte(line, '<'), strings.IndexByte(line, '>')
	if i < 0 || j < i {
		return ""
	}
	return line[i+1 : j]
}
