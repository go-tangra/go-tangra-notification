//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-notification/internal/client"
	"github.com/go-tangra/go-tangra-notification/internal/metrics"
	"github.com/go-tangra/go-tangra-notification/internal/service"
)

var ProviderSet = wire.NewSet(
	service.NewChannelService,
	service.NewTemplateService,
	service.NewNotificationService,
	service.NewPermissionService,
	service.NewUserService,
	service.NewSSEService,
	service.NewInternalMessageService,
	service.NewInternalMessageRecipientService,
	service.NewInternalMessageCategoryService,
	service.NewBackupService,
	client.NewAdminClient,
	metrics.NewCollector,
	ProvideResourceLookup,
	ProvidePermissionStore,
	ProvideAuthzEngine,
	ProvideAuthzChecker,
)
