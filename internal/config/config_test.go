package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func valid() Config {
	c := Default()
	c.ServiceName, c.TrustDomain, c.Env = "notification", "example.org", "dev"
	c.Identity.Provider = "file"
	c.Identity.File.Cert, c.Identity.File.Key, c.Identity.File.Bundle = "c", "k", "b"
	c.Authz.Source, c.Authz.Path = "file", "p.yaml"
	c.Config.Limits.MaxRequestBytes = 17 << 20
	c.DB.DSN = "postgres://notification_app:x@db/notification?sslmode=disable"
	c.Valkey.Addresses = []string{"127.0.0.1:6379"}
	c.Valkey.AllowPlaintext = true
	c.KEK.Path = "deploy/kek.dev"
	c.SMTP.AllowPlaintext = true
	c.Gateway.Issuer = "https://localhost:8443"
	return c
}

func production(c *Config) {
	c.Env = "production"
	c.DB.DSN = "postgres://u:p@db/notification?sslmode=verify-full"
	c.Valkey.AllowPlaintext = false
	c.SMTP.AllowPlaintext = false
}

func TestDefaultsAreSecure(t *testing.T) {
	c := Default()
	if c.Valkey.AllowPlaintext || c.SMTP.AllowPlaintext {
		t.Fatal("plaintext must be opt-in")
	}
	if c.KEK.Source != "file" || c.SMTP.DialTimeoutSeconds != 30 || c.Scheduler.IntervalSeconds != 15 || c.Scheduler.LeaseSeconds != 60 || c.Gateway.Service != "gateway" ||
		c.Limits.BackupMaxBytes != 16<<20 || c.Limits.SendPerTenantPerMinute != 600 || c.Limits.SendPerSenderPerMinute != 60 || c.Limits.StreamsPerUser != 5 || c.Limits.StreamsPerTenant != 2000 || c.Limits.ReplayWindowSeconds != 300 {
		t.Fatalf("defaults %+v", c)
	}
}

func TestValidateAcceptsDevAndProduction(t *testing.T) {
	c := valid()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.ReplayWindow() != 5*time.Minute || c.DialTimeout() != 30*time.Second || c.SchedulerInterval() != 15*time.Second || c.LeaseDuration() != time.Minute {
		t.Fatal("durations")
	}
	if w := c.Warnings(); len(w) < 2 || !strings.Contains(strings.Join(w, "\n"), "smtp.allow_plaintext") || !strings.Contains(strings.Join(w, "\n"), "valkey.allow_plaintext") {
		t.Fatalf("warnings %v", w)
	}
	production(&c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.DB.DSN = "postgres://u:p@db/notification?sslmode=verify-ca"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.KEK = KEK{Source: "env", Env: "NOTIFICATION_KEK"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"freya", func(c *Config) { c.ServiceName = "" }, "service_name"},
		{"db dsn", func(c *Config) { c.DB.DSN = "" }, "db.dsn"},
		{"db sslmode prod", func(c *Config) { production(c); c.DB.DSN = "postgres://u:p@db/n?sslmode=require" }, "sslmode"},
		{"valkey addresses", func(c *Config) { c.Valkey.Addresses = nil }, "valkey.addresses"},
		{"valkey plaintext prod", func(c *Config) { production(c); c.Valkey.AllowPlaintext = true }, "valkey.allow_plaintext"},
		{"kek path", func(c *Config) { c.KEK.Path = "" }, "kek.path"},
		{"kek env", func(c *Config) { c.KEK = KEK{Source: "env"} }, "kek.env"},
		{"kek source", func(c *Config) { c.KEK.Source = "vault" }, "kek.source"},
		{"smtp plaintext prod", func(c *Config) { production(c); c.SMTP.AllowPlaintext = true }, "smtp.allow_plaintext"},
		{"smtp timeout", func(c *Config) { c.SMTP.DialTimeoutSeconds = 0 }, "dial_timeout"},
		{"scheduler interval", func(c *Config) { c.Scheduler.IntervalSeconds = 61 }, "interval_seconds"},
		{"scheduler lease", func(c *Config) { c.Scheduler.LeaseSeconds = 5 }, "lease_seconds"},
		{"gateway service", func(c *Config) { c.Gateway.Service = "" }, "gateway.service"},
		{"gateway issuer", func(c *Config) { c.Gateway.Issuer = "http://x" }, "gateway.issuer"},
		{"backup limit", func(c *Config) { c.Limits.BackupMaxBytes = 1 << 20 }, "backup_max_bytes"},
		{"request limit below backup", func(c *Config) { c.Config.Limits.MaxRequestBytes = 8 << 20 }, "max_request_bytes"},
		{"send rate", func(c *Config) { c.Limits.SendPerSenderPerMinute = 0 }, "send_per_"},
		{"streams", func(c *Config) { c.Limits.StreamsPerTenant = 1 }, "streams_per_user"},
		{"replay window", func(c *Config) { c.Limits.ReplayWindowSeconds = 10 }, "replay_window_seconds"},
	}
	for _, tc := range cases {
		c := valid()
		tc.mut(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	_ = os.WriteFile(good, []byte("service_name: notification\nsmtp:\n  allow_plaintext: true\nlimits_notification:\n  streams_per_user: 3\n"), 0o600)
	c, err := Load(good)
	if err != nil || c.ServiceName != "notification" || !c.SMTP.AllowPlaintext || c.Limits.StreamsPerUser != 3 || c.Limits.StreamsPerTenant != 2000 {
		t.Fatalf("%v %+v", err, c)
	}
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("service_name: x\nunknown_key: 1\n"), 0o600)
	if _, err := Load(bad); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := Load("../../deploy/dev.yaml"); err != nil {
		t.Fatalf("dev.yaml: %v", err)
	}
}

// secretFile writes a relay password file with mode perm.
func secretFile(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "smtp.password")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
	return p
}

func platformEmail(t *testing.T) *PlatformEmail {
	return &PlatformEmail{Host: "mx01.example.net", Port: 587, Username: "tangra@example.net", PasswordFile: secretFile(t, "NOTIF-MARKER-RELAY-PW\n", 0o640), From: "tangra@example.net"}
}

func TestPlatformEmailDefaults(t *testing.T) {
	c := Default()
	if c.PlatformEmail != nil || c.PlatformTenantID != "00000000-0000-0000-0000-000000000001" || c.Limits.SystemSendPerMinute != 300 {
		t.Fatalf("defaults %+v %q %d", c.PlatformEmail, c.PlatformTenantID, c.Limits.SystemSendPerMinute)
	}
	c = valid()
	c.PlatformEmail = platformEmail(t)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.PlatformEmail.Mode() != "starttls" {
		t.Fatalf("tls default %q", c.PlatformEmail.Mode())
	}
	pw, err := c.PlatformEmail.ReadPassword()
	if err != nil || pw != "NOTIF-MARKER-RELAY-PW" {
		t.Fatalf("password %q %v", pw, err)
	}
	// Warnings never carry the password; TLS without opt-out adds none.
	if w := strings.Join(c.Warnings(), "\n"); strings.Contains(w, "NOTIF-MARKER") || strings.Contains(w, "platform_email") {
		t.Fatalf("warnings %q", w)
	}
	// No credentials: no password file needed; implicit TLS accepted.
	c.PlatformEmail = &PlatformEmail{Host: "mx01.example.net", Port: 465, TLS: "implicit", From: "Tangra <tangra@example.net>", ReplyTo: "ops@example.net"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if pw, err := c.PlatformEmail.ReadPassword(); err != nil || pw != "" {
		t.Fatalf("no password %q %v", pw, err)
	}
	// Plaintext with the named opt-out is accepted (also in production) and warned.
	c.PlatformEmail = &PlatformEmail{Host: "mailpit", Port: 1025, TLS: "none", AllowPlaintext: true, From: "tangra@example.org"}
	production(&c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(c.Warnings(), "\n"); !strings.Contains(w, "platform_email.allow_plaintext") {
		t.Fatalf("warnings %q", w)
	}
	// The opt-out without tls none is still reported: it is set.
	c.PlatformEmail.TLS = "starttls"
	if w := strings.Join(c.Warnings(), "\n"); !strings.Contains(w, "platform_email.allow_plaintext") {
		t.Fatalf("warnings %q", w)
	}
}

func TestPlatformEmailRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*PlatformEmail)
		want string
	}{
		{"literal password", func(p *PlatformEmail) { p.Password = "hunter2" }, "use password_file"},
		{"plaintext without opt-out", func(p *PlatformEmail) { p.TLS, p.Username, p.PasswordFile = "none", "", "" }, "platform_email.allow_plaintext"},
		{"username with none", func(p *PlatformEmail) { p.TLS, p.AllowPlaintext = "none", true }, "platform_email.username"},
		{"missing host", func(p *PlatformEmail) { p.Host = "" }, "platform_email.host"},
		{"bad host", func(p *PlatformEmail) { p.Host = "mx01 example" }, "platform_email.host"},
		{"missing from", func(p *PlatformEmail) { p.From = "" }, "platform_email.from"},
		{"bad from", func(p *PlatformEmail) { p.From = "not an address" }, "platform_email.from"},
		{"bad reply_to", func(p *PlatformEmail) { p.ReplyTo = "a@b.c, d@e.f" }, "platform_email.reply_to"},
		{"port zero", func(p *PlatformEmail) { p.Port = 0 }, "platform_email.port"},
		{"port high", func(p *PlatformEmail) { p.Port = 70000 }, "platform_email.port"},
		{"tls mode", func(p *PlatformEmail) { p.TLS = "ssl" }, "platform_email.tls"},
		{"username without file", func(p *PlatformEmail) { p.PasswordFile = "" }, "platform_email.password_file"},
		{"file without username", func(p *PlatformEmail) { p.Username = "" }, "platform_email.username"},
		{"missing file", func(p *PlatformEmail) { p.PasswordFile = "/nonexistent/smtp.password" }, "platform_email.password_file"},
		{"empty file", func(p *PlatformEmail) { p.PasswordFile = secretFile(t, "\n", 0o600) }, "empty"},
		{"world readable", func(p *PlatformEmail) { p.PasswordFile = secretFile(t, "pw", 0o644) }, "0640"},
		{"group writable", func(p *PlatformEmail) { p.PasswordFile = secretFile(t, "pw", 0o660) }, "0640"},
		{"directory", func(p *PlatformEmail) { p.PasswordFile = t.TempDir() }, "platform_email.password_file"},
	}
	for _, tc := range cases {
		c := valid()
		c.PlatformEmail = platformEmail(t)
		tc.mut(c.PlatformEmail)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "NOTIF-MARKER") {
			t.Errorf("%s: secret in error %v", tc.name, err)
		}
	}
	for name, mut := range map[string]func(*Config){
		"platform tenant": func(c *Config) { c.PlatformTenantID = "platform" },
		"system rate low": func(c *Config) { c.Limits.SystemSendPerMinute = 0 },
		"system rate hi":  func(c *Config) { c.Limits.SystemSendPerMinute = 10001 },
	} {
		c := valid()
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	c := valid()
	c.Limits.SystemSendPerMinute = 10000
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPlatformEmail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(p, []byte("service_name: notification\nplatform_email:\n  host: mx01.example.net\n  port: 587\n  from: tangra@example.net\nplatform_tenant_id: 0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55\nlimits_notification:\n  system_send_per_minute: 120\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.PlatformEmail == nil || c.PlatformEmail.Host != "mx01.example.net" || c.PlatformEmail.Port != 587 ||
		c.PlatformTenantID != "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55" || c.Limits.SystemSendPerMinute != 120 {
		t.Fatalf("%v %+v", err, c)
	}
}
