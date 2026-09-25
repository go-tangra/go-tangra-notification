package app

import (
	"context"
	"time"

	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/grpcapi"
	"github.com/go-tangra/go-tangra-notification/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-notification/v4/internal/inbox"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stats"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stream"
	"github.com/go-tangra/go-tangra-notification/v4/internal/transfer"
)

// Version is reported by the health route.
const Version = "1.0.0"

// Wire builds the domain services and mounts every story's handlers on the
// HTTP server and the gRPC services, then checks that every declared route
// has a handler so the contract and the implementation cannot drift.
func Wire(a *App) error {
	cfg := a.Cfg
	a.Authz = authz.New(a.Repo, a.Audit)
	a.Hub = stream.NewHub(a.KV, stream.Config{ReplayWindow: cfg.ReplayWindow(), StreamsPerUser: cfg.Limits.StreamsPerUser, StreamsPerTenant: cfg.Limits.StreamsPerTenant}, a.Log)
	a.closers = append(a.closers, a.Hub.Close)
	limiter := stream.NewLimiter(a.KV)
	a.Channels = notify.NewChannels(a.Repo, a.Envelope, a.Providers, a.Authz, a.Audit)
	if a.Platform != nil {
		a.Channels.SetPlatformProvider(a.Platform)
	}
	a.Templates = notify.NewTemplates(a.Repo, a.Authz, a.Audit)
	a.Sender = notify.NewSender(a.Repo, a.Channels, a.Templates, a.Authz, a.Audit, limiter, notify.Limits{PerTenant: cfg.Limits.SendPerTenantPerMinute, PerSender: cfg.Limits.SendPerSenderPerMinute,
		System: cfg.Limits.SystemSendPerMinute})
	a.Sender.SetPlatformTenant(cfg.PlatformTenantID)
	nd := httpapi.NotifyDeps{Channels: a.Channels, Templates: a.Templates, Sender: a.Sender, Authz: a.Authz, Perms: a.Perms}
	a.HTTP.RegisterChannels(nd)
	a.HTTP.RegisterTemplates(nd)
	a.HTTP.RegisterNotifications(nd)
	a.HTTP.RegisterGrants(httpapi.GrantDeps{Authz: a.Authz})
	a.Messages = messages.New(a.Repo, a.Audit, a.Members, a.Hub)
	a.Inbox = inbox.New(a.Repo, a.Audit)
	a.HTTP.RegisterMessages(httpapi.MessageDeps{Messages: a.Messages, Inbox: a.Inbox, Perms: a.Perms})
	a.HTTP.RegisterStream(httpapi.StreamDeps{Hub: a.Hub, Audit: a.Audit})
	a.Scheduler = messages.NewScheduler(a.Repo, a.Messages, messages.SchedulerConfig{Interval: cfg.SchedulerInterval(), Lease: cfg.LeaseDuration()}, a.Log, a.Tick)
	a.workers = append(a.workers, a.Scheduler.Run)
	a.workers = append(a.workers, func(ctx context.Context) { a.Sender.RunExpiry(ctx, time.Minute) })
	a.Transfer = transfer.New(a.Repo, a.Channels, a.Templates, a.Messages, a.Audit)
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.SeedSystemEmail(sctx)
	cancel()
	a.HTTP.RegisterOps(httpapi.OpsDeps{Transfer: a.Transfer, MaxBytes: cfg.Limits.BackupMaxBytes, Stats: stats.New(a.Repo, a.Hub), Audit: a.Repo, Version: Version,
		Health: func(ctx context.Context) any { return a.Health(ctx) }})
	if v, ok := a.Verifier.(*authclient.Verifier); ok {
		_ = v // service-to-service methods carry no user token; the Freya server polices callers
	} else if a.GRPCAuth != nil {
		a.Freya.GRPC().Use("/notification.v1.*", a.GRPCAuth)
	}
	notificationv1.RegisterNotifierServer(a.Freya.GRPC(), &grpcapi.NotifierServer{Sender: a.Sender, Audit: a.Audit})
	notificationv1.RegisterEventsServer(a.Freya.GRPC(), &grpcapi.EventsServer{Hub: a.Hub, Audit: a.Audit})
	return a.CheckRoutes()
}
