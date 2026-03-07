package service

import (
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

func protoToChannelType(t notificationpb.ChannelType) channel.Type {
	switch t {
	case notificationpb.ChannelType_CHANNEL_TYPE_EMAIL:
		return channel.TypeEMAIL
	case notificationpb.ChannelType_CHANNEL_TYPE_SMS:
		return channel.TypeSMS
	case notificationpb.ChannelType_CHANNEL_TYPE_SLACK:
		return channel.TypeSLACK
	case notificationpb.ChannelType_CHANNEL_TYPE_SSE:
		return channel.TypeSSE
	default:
		return channel.TypeEMAIL
	}
}
