package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc"

	"github.com/go-tangra/go-tangra-notification/v4/internal/config"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/taskexec"
)

// TestSchedulerRegistrar (feature 026): registration is opt-in, dials the
// configured scheduler service and declares the module's task types.
func TestSchedulerRegistrar(t *testing.T) {
	cfg := config.Default()
	var dialed []string
	dial := func(_ context.Context, service string) (*grpc.ClientConn, error) {
		dialed = append(dialed, service)
		return nil, errors.New("offline")
	}
	if r := schedulerRegistrar(cfg, dial, slog.New(slog.DiscardHandler)); r != nil {
		t.Fatal("registrar without task_scheduler.enabled")
	}
	cfg.TaskScheduler = config.TaskScheduler{Enabled: true, Service: "scheduler-2"}
	r := schedulerRegistrar(cfg, dial, slog.New(slog.DiscardHandler))
	if r == nil || len(r.Types) != 1 || r.Types[0].Type != taskexec.TypeSendTestEmail {
		t.Fatalf("registrar %+v", r)
	}
	if err := r.Once(context.Background()); err == nil || len(dialed) != 1 || dialed[0] != "scheduler-2" {
		t.Fatalf("dial %v %v", err, dialed)
	}
}

// TestSchedulerExecutor: the executor is built for the configured trust
// domain and scheduler name; an invalid identity is refused.
func TestSchedulerExecutor(t *testing.T) {
	cfg := config.Default()
	cfg.TrustDomain = "example.org"
	if _, err := schedulerExecutor(cfg, nil); err == nil {
		t.Fatal("nil sender accepted")
	}
	if e, err := schedulerExecutor(cfg, &notify.Sender{}); err != nil || len(e.Handlers()) != 1 {
		t.Fatalf("executor %v %v", e, err)
	}
	cfg.TrustDomain = ""
	if _, err := schedulerExecutor(cfg, &notify.Sender{}); err == nil {
		t.Fatal("empty trust domain accepted")
	}
}
