//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-notification/internal/data"
)

var ProviderSet = wire.NewSet(
	data.NewRedisClient,
	data.NewEntClient,
	data.NewChannelRepo,
	data.NewTemplateRepo,
	data.NewNotificationLogRepo,
	data.NewPermissionRepo,
	data.NewInternalMessageRepo,
	data.NewInternalMessageRecipientRepo,
	data.NewInternalMessageCategoryRepo,
	data.NewUserTokenCacheRepo,
)
