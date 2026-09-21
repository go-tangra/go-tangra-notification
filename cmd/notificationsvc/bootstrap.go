package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-freya/freya/services/auth/pkg/authclient"
	"github.com/go-freya/freya/services/notification/internal/app"
	"github.com/go-freya/freya/services/notification/internal/config"
	"github.com/go-freya/freya/services/notification/internal/sealed"
)

// bootstrap prepares a deployment: migrations, a KEK check (seal/open round
// trip) and the dependency health. It is idempotent and never serves.
// `bootstrap -rotate-kek <new-kek-file>` re-seals every channel with a new
// key (run with the old key in the configuration).
func bootstrap(args []string) int {
	fs := flag.NewFlagSet("notificationsvc bootstrap", flag.ContinueOnError)
	cfgPath := fs.String("config", "deploy/dev.yaml", "configuration file")
	rotate := fs.String("rotate-kek", "", "re-seal channel settings with the KEK in this file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg.Server.GRPCAddr, cfg.Admin.Addr = "127.0.0.1:0", "127.0.0.1:0"
	if cfg.Server.HTTPAddr != "" {
		cfg.Server.HTTPAddr = "127.0.0.1:0"
	}
	a, err := app.Build(ctx, cfg, app.Options{Migrate: true, Verifier: noVerifier{}, Members: noDirectory{}})
	if err != nil {
		return fail(err)
	}
	defer a.Close()
	res := struct {
		Migrated bool       `json:"migrated"`
		Health   app.Health `json:"health"`
		KEK      string     `json:"kek"`
		Resealed int        `json:"resealed,omitempty"`
	}{Migrated: true, Health: a.Health(ctx), KEK: "ok"}
	if err := probeKEK(a.Envelope); err != nil {
		res.KEK = err.Error()
	}
	if *rotate != "" {
		n, err := app.RotateKEK(ctx, a, *rotate)
		if err != nil {
			return fail(err)
		}
		res.Resealed = n
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
	if res.Health.DB != "ok" || res.Health.Valkey != "ok" || res.KEK != "ok" {
		return 1
	}
	return 0
}

// probeKEK seals and opens a probe value so a corrupted key surfaces before
// the first channel is saved.
func probeKEK(e *sealed.Envelope) error {
	blob, err := e.Seal([]byte(`{"probe":true}`), sealed.AD("probe"))
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if _, err := e.Open(blob, sealed.AD("probe")); err != nil {
		return fmt.Errorf("open: %w", err)
	}
	return nil
}

// noVerifier refuses every token: bootstrap never serves requests.
type noVerifier struct{}

func (noVerifier) Verify(context.Context, string) (authclient.Identity, error) {
	return authclient.Identity{}, authclient.ErrUnauthenticated
}

// noDirectory resolves nobody: bootstrap never publishes messages.
type noDirectory struct{}

func (noDirectory) Lookup(context.Context, string, []string) ([]string, error)  { return nil, nil }
func (noDirectory) Members(context.Context, string, func([]string) error) error { return nil }
