// Package config loads and validates the notification service configuration:
// the Freya framework config plus the module's own sections. Every value is
// explicit; insecure opt-outs are named and logged at start.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	fconfig "github.com/go-freya/freya/config"
	"gopkg.in/yaml.v3"
)

// Config is the notification service configuration.
type Config struct {
	fconfig.Config `yaml:",inline"`

	DB        DB        `yaml:"db"`
	Valkey    Valkey    `yaml:"valkey"`
	KEK       KEK       `yaml:"kek"`
	SMTP      SMTP      `yaml:"smtp"`
	Scheduler Scheduler `yaml:"scheduler"`
	Gateway   Gateway   `yaml:"gateway"`
	Enroll    Enroll    `yaml:"enroll"`
	Limits    Limits    `yaml:"limits_notification"`
}

// DB configures TimescaleDB.
type DB struct {
	DSN        string `yaml:"dsn"`         // application role (no BYPASSRLS)
	MigrateDSN string `yaml:"migrate_dsn"` // migration role; empty = DSN
	MaxConns   int32  `yaml:"max_conns"`
}

// Valkey configures the live-event streams and rate-limit counters.
type Valkey struct {
	Addresses      []string `yaml:"addresses"`
	Username       string   `yaml:"username"`
	Password       string   `yaml:"password"`
	AllowPlaintext bool     `yaml:"allow_plaintext"`
	CAFile         string   `yaml:"ca_file"`
}

// KEK names where the 32-byte key-encryption key comes from (research R1).
type KEK struct {
	Source string `yaml:"source"` // file | env
	Path   string `yaml:"path"`
	Env    string `yaml:"env"`
}

// SMTP bounds every email channel of every tenant.
type SMTP struct {
	AllowPlaintext     bool `yaml:"allow_plaintext"` // channels may use tls: none (development only)
	DialTimeoutSeconds int  `yaml:"dial_timeout_seconds"`
}

// Scheduler configures the delayed-publishing worker (research R5).
type Scheduler struct {
	IntervalSeconds int `yaml:"interval_seconds"`
	LeaseSeconds    int `yaml:"lease_seconds"`
}

// Gateway names the application gateway and the platform token issuer.
type Gateway struct {
	Service string `yaml:"service"`
	Issuer  string `yaml:"issuer"`
}

// Enroll makes the service obtain its SVID by enrolling with lcm over the
// network (instead of reading it from a file), for multi-host deployments.
type Enroll struct {
	Enabled       bool   `yaml:"enabled"`
	EnrollURL     string `yaml:"enroll_url"`  // public first-enroll endpoint (via the gateway edge)
	LCMGRPCTarget string `yaml:"lcm_grpc"`    // lcm gRPC endpoint for mTLS renewal
	TenantID      string `yaml:"tenant_id"`   // the lcm mesh tenant (so the SVID chains to the mesh root)
	TokenFile     string `yaml:"token_file"`
	StateFile     string `yaml:"state_file"`  // path to the single-use join token
	Insecure      bool   `yaml:"insecure"`    // dev: skip server verification (self-signed edge)
}

// Limits bound the module's own request shapes and rates.
type Limits struct {
	BackupMaxBytes         int64 `yaml:"backup_max_bytes"`
	SendPerTenantPerMinute int   `yaml:"send_per_tenant_per_minute"`
	SendPerSenderPerMinute int   `yaml:"send_per_sender_per_minute"`
	StreamsPerUser         int   `yaml:"streams_per_user"`
	StreamsPerTenant       int   `yaml:"streams_per_tenant"`
	ReplayWindowSeconds    int   `yaml:"replay_window_seconds"`
}

// Default returns secure defaults on top of the Freya defaults.
func Default() Config {
	return Config{
		Config:    fconfig.Default(),
		DB:        DB{MaxConns: 16},
		KEK:       KEK{Source: "file"},
		SMTP:      SMTP{DialTimeoutSeconds: 30},
		Scheduler: Scheduler{IntervalSeconds: 15, LeaseSeconds: 60},
		Gateway:   Gateway{Service: "gateway"},
		Limits: Limits{BackupMaxBytes: 16 << 20, SendPerTenantPerMinute: 600, SendPerSenderPerMinute: 60,
			StreamsPerUser: 5, StreamsPerTenant: 2000, ReplayWindowSeconds: 300},
	}
}

// Load reads YAML over Default(); unknown fields are rejected. Not yet validated.
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied config path
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks the Freya config and every module section.
func (c Config) Validate() error {
	if err := c.Config.Validate(); err != nil {
		return err
	}
	prod := c.IsProduction()
	if c.DB.DSN == "" {
		return errors.New("config: db.dsn is required")
	}
	if prod && !strings.Contains(c.DB.DSN, "sslmode=verify-full") && !strings.Contains(c.DB.DSN, "sslmode=verify-ca") {
		return errors.New("config: db.dsn must use sslmode=verify-full (or verify-ca) in production")
	}
	if len(c.Valkey.Addresses) == 0 {
		return errors.New("config: valkey.addresses is required")
	}
	if prod && c.Valkey.AllowPlaintext {
		return errors.New("config: valkey.allow_plaintext is not permitted in production")
	}
	switch c.KEK.Source {
	case "file":
		if c.KEK.Path == "" {
			return errors.New("config: kek.path is required for kek.source file")
		}
	case "env":
		if c.KEK.Env == "" {
			return errors.New("config: kek.env is required for kek.source env")
		}
	default:
		return errors.New("config: kek.source must be file or env")
	}
	if prod && c.SMTP.AllowPlaintext {
		return errors.New("config: smtp.allow_plaintext is not permitted in production")
	}
	if c.SMTP.DialTimeoutSeconds < 1 || c.SMTP.DialTimeoutSeconds > 120 {
		return errors.New("config: smtp.dial_timeout_seconds must be within [1, 120]")
	}
	if c.Scheduler.IntervalSeconds < 1 || c.Scheduler.IntervalSeconds > 60 {
		return errors.New("config: scheduler.interval_seconds must be within [1, 60]")
	}
	if c.Scheduler.LeaseSeconds < c.Scheduler.IntervalSeconds || c.Scheduler.LeaseSeconds > 600 {
		return errors.New("config: scheduler.lease_seconds must be within [interval, 600]")
	}
	if c.Gateway.Service == "" {
		return errors.New("config: gateway.service is required")
	}
	if iu, err := url.Parse(c.Gateway.Issuer); err != nil || iu.Scheme != "https" || iu.Host == "" {
		return errors.New("config: gateway.issuer must be an https origin")
	}
	if c.Limits.BackupMaxBytes < 4<<20 || c.Limits.BackupMaxBytes > 64<<20 {
		return errors.New("config: limits_notification.backup_max_bytes must be within [4 MiB, 64 MiB]")
	}
	if c.Config.Limits.MaxRequestBytes < c.Limits.BackupMaxBytes {
		return errors.New("config: limits.max_request_bytes must be at least limits_notification.backup_max_bytes (backup uploads)")
	}
	if c.Limits.SendPerTenantPerMinute <= 0 || c.Limits.SendPerSenderPerMinute <= 0 {
		return errors.New("config: limits_notification.send_per_*_per_minute must be positive")
	}
	if c.Limits.StreamsPerUser <= 0 || c.Limits.StreamsPerTenant < c.Limits.StreamsPerUser {
		return errors.New("config: limits_notification.streams_per_user must be positive and streams_per_tenant at least as large")
	}
	if c.Limits.ReplayWindowSeconds < 60 || c.Limits.ReplayWindowSeconds > 3600 {
		return errors.New("config: limits_notification.replay_window_seconds must be within [60, 3600]")
	}
	return nil
}

// Warnings lists accepted insecure opt-outs (logged at start).
func (c Config) Warnings() []string {
	w := c.Config.Warnings()
	if c.Valkey.AllowPlaintext {
		w = append(w, "valkey.allow_plaintext: stream traffic without TLS (development only)")
	}
	if c.SMTP.AllowPlaintext {
		w = append(w, "smtp.allow_plaintext: channels may deliver mail without TLS (development only)")
	}
	return w
}

// ReplayWindow is the live-event replay window.
func (c Config) ReplayWindow() time.Duration {
	return time.Duration(c.Limits.ReplayWindowSeconds) * time.Second
}

// DialTimeout bounds SMTP connections.
func (c Config) DialTimeout() time.Duration {
	return time.Duration(c.SMTP.DialTimeoutSeconds) * time.Second
}

// SchedulerInterval is the worker tick.
func (c Config) SchedulerInterval() time.Duration {
	return time.Duration(c.Scheduler.IntervalSeconds) * time.Second
}

// LeaseDuration is how long a claimed message stays claimed.
func (c Config) LeaseDuration() time.Duration {
	return time.Duration(c.Scheduler.LeaseSeconds) * time.Second
}
