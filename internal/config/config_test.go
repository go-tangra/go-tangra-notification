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
