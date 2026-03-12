package server

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/metadata"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
	"github.com/go-tangra/go-tangra-notification/internal/cert"
	"github.com/go-tangra/go-tangra-notification/internal/service"

	"github.com/go-tangra/go-tangra-common/middleware/audit"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
	appViewer "github.com/go-tangra/go-tangra-common/viewer"
)

// systemViewerMiddleware injects system viewer context for all requests.
// This allows the notification service to bypass tenant privacy checks at the ent level,
// since tenant scoping is handled explicitly at the repository/service layer.
func systemViewerMiddleware() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			ctx = appViewer.NewSystemViewerContext(ctx)
			return handler(ctx, req)
		}
	}
}

func NewGRPCServer(
	ctx *bootstrap.Context,
	certManager *cert.CertManager,
	channelSvc *service.ChannelService,
	templateSvc *service.TemplateService,
	notifSvc *service.NotificationService,
	permissionSvc *service.PermissionService,
	userSvc *service.UserService,
) (*grpc.Server, error) {
	cfg := ctx.GetConfig()
	l := ctx.NewLoggerHelper("notification/grpc")

	var opts []grpc.ServerOption

	if cfg.Server != nil && cfg.Server.Grpc != nil {
		if cfg.Server.Grpc.Network != "" {
			opts = append(opts, grpc.Network(cfg.Server.Grpc.Network))
		}
		if cfg.Server.Grpc.Addr != "" {
			opts = append(opts, grpc.Address(cfg.Server.Grpc.Addr))
		}
		if cfg.Server.Grpc.Timeout != nil {
			opts = append(opts, grpc.Timeout(cfg.Server.Grpc.Timeout.AsDuration()))
		}
	}

	// H3: When TLS is expected, treat config failure as fatal to prevent silent downgrade
	if certManager != nil && certManager.IsTLSEnabled() {
		tlsConfig, err := certManager.GetServerTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("mTLS required but failed to load TLS config: %w", err)
		}
		opts = append(opts, grpc.TLSConfig(tlsConfig))
		l.Info("gRPC server configured with mTLS")
	} else {
		l.Warn("TLS not enabled, running without mTLS")
	}

	var ms []middleware.Middleware
	ms = append(ms, recovery.Recovery())
	ms = append(ms, systemViewerMiddleware())
	ms = append(ms, tracing.Server())
	ms = append(ms, metadata.Server())
	ms = append(ms, logging.Server(ctx.GetLogger()))

	ms = append(ms, mtls.MTLSMiddleware(
		ctx.GetLogger(),
		mtls.WithPublicEndpoints(
			"/grpc.health.v1.Health/Check",
			"/grpc.health.v1.Health/Watch",
		),
	))

	ms = append(ms, audit.Server(
		ctx.GetLogger(),
		audit.WithServiceName("notification-service"),
		audit.WithSkipOperations(
			"/grpc.health.v1.Health/Check",
			"/grpc.health.v1.Health/Watch",
		),
	))

	ms = append(ms, protoValidator())

	opts = append(opts, grpc.Middleware(ms...))

	srv := grpc.NewServer(opts...)

	notificationpb.RegisterRedactedNotificationChannelServiceServer(srv, channelSvc, nil)
	notificationpb.RegisterRedactedNotificationTemplateServiceServer(srv, templateSvc, nil)
	notificationpb.RegisterRedactedNotificationServiceServer(srv, notifSvc, nil)
	notificationpb.RegisterRedactedNotificationPermissionServiceServer(srv, permissionSvc, nil)
	notificationpb.RegisterRedactedNotificationUserServiceServer(srv, userSvc, nil)

	return srv, nil
}
