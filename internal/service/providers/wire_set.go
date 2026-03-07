//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-notification/internal/service"
)

var ProviderSet = wire.NewSet(
	service.NewChannelService,
	service.NewTemplateService,
	service.NewNotificationService,
)
