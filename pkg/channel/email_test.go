package channel

import (
	"strings"
	"testing"
)

func TestParseEmailConfig_CustomHeaders(t *testing.T) {
	t.Run("accepts well-formed headers and canonicalizes keys", func(t *testing.T) {
		cfg, err := ParseEmailConfig(`{
			"host":"smtp.example.com","port":587,"from":"noreply@example.com",
			"custom_headers":{
				"x-mailer":"GoTangra",
				"reply-TO":"support@example.com"
			}
		}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := cfg.CustomHeaders["X-Mailer"]; got != "GoTangra" {
			t.Errorf("X-Mailer = %q, want %q", got, "GoTangra")
		}
		if got := cfg.CustomHeaders["Reply-To"]; got != "support@example.com" {
			t.Errorf("Reply-To = %q, want %q", got, "support@example.com")
		}
	})

	t.Run("rejects reserved header overrides", func(t *testing.T) {
		for _, name := range []string{"From", "to", "Subject", "MIME-Version", "Content-Type"} {
			_, err := ParseEmailConfig(`{
				"host":"smtp.example.com","port":587,"from":"x@y.z",
				"custom_headers":{"` + name + `":"oops"}
			}`)
			if err == nil {
				t.Errorf("expected error rejecting reserved header %q, got nil", name)
			}
		}
	})

	t.Run("rejects invalid header name characters", func(t *testing.T) {
		invalid := []string{"", "has space", "with:colon", "non\nascii", "\x7f"}
		for _, name := range invalid {
			_, err := ParseEmailConfig(`{
				"host":"smtp.example.com","port":587,"from":"x@y.z",
				"custom_headers":{"` + name + `":"v"}
			}`)
			if err == nil {
				t.Errorf("expected error rejecting invalid header name %q, got nil", name)
			}
		}
	})

	t.Run("strips CR/LF from values to block injection", func(t *testing.T) {
		cfg, err := ParseEmailConfig("{\"host\":\"smtp.example.com\",\"port\":587,\"from\":\"x@y.z\"," +
			"\"custom_headers\":{\"X-Injected\":\"clean\\r\\nBcc: attacker@evil.com\"}}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := cfg.CustomHeaders["X-Injected"]
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("header value still contains CR/LF: %q", got)
		}
		if !strings.HasPrefix(got, "clean") || !strings.Contains(got, "attacker") {
			t.Errorf("CR/LF were stripped, expected content preserved: %q", got)
		}
	})

	t.Run("absent custom_headers leaves field nil", func(t *testing.T) {
		cfg, err := ParseEmailConfig(`{"host":"smtp.example.com","port":587,"from":"x@y.z"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.CustomHeaders != nil {
			t.Errorf("CustomHeaders should be nil when absent, got %v", cfg.CustomHeaders)
		}
	})
}

func TestValidHeaderName(t *testing.T) {
	valid := []string{"X-Mailer", "Reply-To", "List-Unsubscribe", "X-Custom-1"}
	for _, n := range valid {
		if !validHeaderName(n) {
			t.Errorf("validHeaderName(%q) = false, want true", n)
		}
	}
	invalid := []string{"", " ", "has space", "with:colon", "Bcc\n", string([]byte{0x7f})}
	for _, n := range invalid {
		if validHeaderName(n) {
			t.Errorf("validHeaderName(%q) = true, want false", n)
		}
	}
}
