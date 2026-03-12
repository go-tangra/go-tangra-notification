package main

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-common/registration"
	"github.com/go-tangra/go-tangra-common/service"
	"github.com/go-tangra/go-tangra-notification/cmd/server/assets"
)

var (
	moduleID    = "notification"
	moduleName  = "Notification"
	version     = "1.0.0"
	description = "Multi-channel notification service with template-based messaging"
)

var globalRegHelper *registration.RegistrationHelper

func newApp(
	ctx *bootstrap.Context,
	gs *grpc.Server,
	hs *kratosHttp.Server,
) *kratos.App {
	globalRegHelper = registration.StartRegistration(ctx, ctx.GetLogger(), &registration.Config{
		ModuleID:          moduleID,
		ModuleName:        moduleName,
		Version:           version,
		Description:       description,
		GRPCEndpoint:      registration.GetGRPCAdvertiseAddr(ctx, "0.0.0.0:10300"),
		FrontendEntryUrl:  registration.GetEnvOrDefault("FRONTEND_ENTRY_URL", ""),
		HttpEndpoint:      registration.GetEnvOrDefault("HTTP_ADVERTISE_ADDR", ""),
		AdminEndpoint:     registration.GetEnvOrDefault("ADMIN_GRPC_ENDPOINT", ""),
		OpenapiSpec:       assets.OpenApiData,
		ProtoDescriptor:   assets.DescriptorData,
		MenusYaml:         assets.MenusData,
		HeartbeatInterval: 30 * time.Second,
		RetryInterval:     5 * time.Second,
		MaxRetries:        60,
	})

	return bootstrap.NewApp(ctx, gs, hs)
}

func runApp() error {
	ctx := bootstrap.NewContext(
		context.Background(),
		&conf.AppInfo{
			Project: service.Project,
			AppId:   "notification.service",
			Version: version,
		},
	)

	// H4: Guard against nil dereference if newApp was never called (early failure)
	defer func() {
		if globalRegHelper != nil {
			globalRegHelper.Stop()
		}
	}()

	return bootstrap.RunApp(ctx, initApp)
}

func main() {
	if err := runApp(); err != nil {
		panic(err)
	}
}
