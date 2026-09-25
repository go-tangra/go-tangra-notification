// Package config loads and validates the notification service configuration:
// the Freya framework config plus the module's own sections. Every value is
// explicit; insecure opt-outs are named and logged at start.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	fconfig "github.com/go-tangra/go-tangra/v4/config"
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

	// PlatformEmail is the platform-wide mail relay (feature 017); nil =
	// no platform channel, platform email disabled.
	PlatformEmail *PlatformEmail `yaml:"platform_email"`
	// PlatformTenantID owns the platform channel and the system templates
	// (auth's platform tenant).
	PlatformTenantID string `yaml:"platform_tenant_id"`
}

// DefaultPlatformTenantID is auth's platform tenant.
const DefaultPlatformTenantID = "00000000-0000-0000-0000-000000000001"

// PlatformEmail is the relay behind the configuration-managed platform
// channel. The password comes only from a mounted secret file; a literal
// password is refused so it never sits in a configuration file.
type PlatformEmail struct {
	Host           string `yaml:"host"` // must match the relay certificate
	Port           int    `yaml:"port"`
	TLS            string `yaml:"tls"` // implicit | starttls (default) | none
	Username       string `yaml:"username"`
	Password       string `yaml:"password"` // refused: use password_file
	PasswordFile   string `yaml:"password_file"`
	From           string `yaml:"from"`
	ReplyTo        string `yaml:"reply_to"`
	AllowPlaintext bool   `yaml:"allow_plaintext"` // required for tls: none; warned at start
}

// Mode is the transport security with its default applied.
func (p *PlatformEmail) Mode() string {
	if p.TLS == "" {
		return "starttls"
	}
	return p.TLS
}

// validate checks the relay setting; the password file is read to prove it
// is usable, its content never appears in an error.
func (p *PlatformEmail) validate() error {
	if p.Password != "" {
		return errors.New("config: platform_email.password is not accepted: use password_file (a mounted secret file)")
	}
	if p.Host == "" || len(p.Host) > 253 || strings.ContainsAny(p.Host, " /\\\r\n\t") {
		return errors.New("config: platform_email.host is required (the relay host name its certificate carries)")
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("config: platform_email.port must be within [1, 65535]")
	}
	switch p.Mode() {
	case "implicit", "starttls":
	case "none":
		if !p.AllowPlaintext {
			return errors.New("config: platform_email.tls none requires platform_email.allow_plaintext: true (mail would travel unencrypted)")
		}
		if p.Username != "" {
			return errors.New("config: platform_email.username is refused with tls none (credentials are never sent unencrypted)")
		}
	default:
		return errors.New("config: platform_email.tls must be implicit, starttls or none")
	}
	if p.Username != "" && p.PasswordFile == "" {
		return errors.New("config: platform_email.password_file is required with platform_email.username")
	}
	if p.Username == "" && p.PasswordFile != "" {
		return errors.New("config: platform_email.username is required with platform_email.password_file")
	}
	if a, err := mail.ParseAddress(p.From); err != nil || a.Address == "" || len(p.From) > 320 {
		return errors.New("config: platform_email.from must be one address")
	}
	if p.ReplyTo != "" {
		if _, err := mail.ParseAddress(p.ReplyTo); err != nil || len(p.ReplyTo) > 320 {
			return errors.New("config: platform_email.reply_to must be one address")
		}
	}
	if _, err := p.ReadPassword(); err != nil {
		return err
	}
	return nil
}

// ReadPassword reads the relay password from password_file ("" without
// one): the file must be a regular file of mode 0640 or stricter and not
// empty; one trailing newline is dropped.
func (p *PlatformEmail) ReadPassword() (string, error) {
	if p.PasswordFile == "" {
		return "", nil
	}
	fi, err := os.Stat(p.PasswordFile)
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("config: platform_email.password_file is not a readable file")
	}
	if fi.Mode().Perm()&^0o640 != 0 {
		return "", fmt.Errorf("config: platform_email.password_file must have mode 0640 or stricter (has %04o)", fi.Mode().Perm())
	}
	raw, err := os.ReadFile(p.PasswordFile) // #nosec G304 -- operator-supplied secret path
	if err != nil {
		return "", errors.New("config: platform_email.password_file is not a readable file")
	}
	pw := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if pw == "" {
		return "", errors.New("config: platform_email.password_file is empty")
	}
	return pw, nil
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
	EnrollURL     string `yaml:"enroll_url"` // public first-enroll endpoint (via the gateway edge)
	LCMGRPCTarget string `yaml:"lcm_grpc"`   // lcm gRPC endpoint for mTLS renewal
	TenantID      string `yaml:"tenant_id"`  // the lcm mesh tenant (so the SVID chains to the mesh root)
	TokenFile     string `yaml:"token_file"`
	StateFile     string `yaml:"state_file"` // path to the single-use join token
	Insecure      bool   `yaml:"insecure"`   // dev: skip server verification (self-signed edge)
}

// Limits bound the module's own request shapes and rates.
type Limits struct {
	BackupMaxBytes         int64 `yaml:"backup_max_bytes"`
	SendPerTenantPerMinute int   `yaml:"send_per_tenant_per_minute"`
	SendPerSenderPerMinute int   `yaml:"send_per_sender_per_minute"`
	StreamsPerUser         int   `yaml:"streams_per_user"`
	StreamsPerTenant       int   `yaml:"streams_per_tenant"`
	ReplayWindowSeconds    int   `yaml:"replay_window_seconds"`
	SystemSendPerMinute    int   `yaml:"system_send_per_minute"` // system template sends per calling service
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
			StreamsPerUser: 5, StreamsPerTenant: 2000, ReplayWindowSeconds: 300, SystemSendPerMinute: 300},
		PlatformTenantID: DefaultPlatformTenantID,
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
	if c.Limits.SystemSendPerMinute < 1 || c.Limits.SystemSendPerMinute > 10000 {
		return errors.New("config: limits_notification.system_send_per_minute must be within [1, 10000]")
	}
	if !uuidRE.MatchString(c.PlatformTenantID) {
		return errors.New("config: platform_tenant_id must be a uuid")
	}
	if c.PlatformEmail != nil {
		if err := c.PlatformEmail.validate(); err != nil {
			return err
		}
	}
	return nil
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Warnings lists accepted insecure opt-outs (logged at start).
func (c Config) Warnings() []string {
	w := c.Config.Warnings()
	if c.Valkey.AllowPlaintext {
		w = append(w, "valkey.allow_plaintext: stream traffic without TLS (development only)")
	}
	if c.SMTP.AllowPlaintext {
		w = append(w, "smtp.allow_plaintext: channels may deliver mail without TLS (development only)")
	}
	if c.PlatformEmail != nil && c.PlatformEmail.AllowPlaintext {
		w = append(w, "platform_email.allow_plaintext: the platform relay may be reached without TLS (tls: "+c.PlatformEmail.Mode()+")")
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
