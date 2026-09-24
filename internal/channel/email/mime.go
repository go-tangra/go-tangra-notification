package email

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var tagRE = regexp.MustCompile(`(?s)<[^>]*>`)
var blockRE = regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6])>|<br\s*/?>`)

// TextFromHTML derives a plain-text alternative from an HTML body.
func TextFromHTML(html string) string {
	s := blockRE.ReplaceAllString(html, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&#34;", `"`).Replace(s)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Build assembles the RFC 5322 message: text/plain alone, or
// multipart/alternative with a text part derived from the HTML.
func Build(from, to, replyTo, subject, text, html string, now time.Time) ([]byte, error) {
	for _, h := range []string{from, to, replyTo, subject} {
		if !HeaderSafe(h) {
			return nil, ErrHeader
		}
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return nil, fmt.Errorf("email: recipient: %w", err)
	}
	if text == "" && html != "" {
		text = TextFromHTML(html)
	}
	var b bytes.Buffer
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	domain := "notification.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			domain = a.Address[i+1:]
		}
	}
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	if replyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", replyTo)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeSubject(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("X-Mailer: freya-notification\r\n")
	if html == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		writeQP(&b, text)
		return b.Bytes(), nil
	}
	boundary := "=_freya_" + hex.EncodeToString(id[:8])
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary)
	writeQP(&b, text)
	fmt.Fprintf(&b, "\r\n--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary)
	writeQP(&b, html)
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// encodeSubject encodes non-ASCII subjects and folds long ones so that no
// header line exceeds 78 characters (RFC 5322 §2.1.1; 998 is the hard
// limit). Long subjects become a chain of B-encoded words: the folding
// whitespace between encoded words is dropped on decoding, so the subject
// reaches the reader unchanged.
func encodeSubject(subject string) string {
	if enc := mime.QEncoding.Encode("utf-8", subject); len(enc) <= 66 { // "Subject: " + 66 fits 76 columns
		return enc
	}
	var words []string
	rest := subject
	for len(rest) > 0 {
		n := 0
		for n < len(rest) {
			_, size := utf8.DecodeRuneInString(rest[n:])
			if n+size > 45 { // 45 bytes → 60 base64 characters → a 72-byte encoded word
				break
			}
			n += size
		}
		words = append(words, "=?utf-8?b?"+base64.StdEncoding.EncodeToString([]byte(rest[:n]))+"?=")
		rest = rest[n:]
	}
	return strings.Join(words, "\r\n ")
}

func writeQP(b *bytes.Buffer, s string) {
	w := quotedprintable.NewWriter(b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	b.WriteString("\r\n")
}
