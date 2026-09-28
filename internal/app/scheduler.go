package app

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	"github.com/go-tangra/go-tangra-scheduler/sdk/v4/pkg/schedulerclient"

	"github.com/go-tangra/go-tangra-notification/v4/internal/config"
	"github.com/go-tangra/go-tangra-notification/v4/internal/taskexec"
)

// schedulerExecutor builds the executor of the module's scheduled task
// types for the scheduler identity of the configuration (feature 026).
func schedulerExecutor(cfg config.Config, sender taskexec.Sender) (*taskexec.Executor, error) {
	return taskexec.New(sender, cfg.TrustDomain, cfg.TaskScheduler.Service)
}

// schedulerRegistrar keeps the task types registered with the scheduler
// module; nil unless task_scheduler.enabled.
func schedulerRegistrar(cfg config.Config, dial func(context.Context, string) (*grpc.ClientConn, error), log *slog.Logger) *schedulerclient.Registrar {
	if !cfg.TaskScheduler.Enabled {
		return nil
	}
	service := cfg.TaskScheduler.Service
	return &schedulerclient.Registrar{
		Dial: func(ctx context.Context) (grpc.ClientConnInterface, error) {
			conn, err := dial(ctx, service)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
		Types: taskexec.Descriptors(),
		Log:   log,
	}
}
