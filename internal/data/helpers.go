package data

import (
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

func derefUint32(v *uint32) uint32 {
	if v == nil {
		return 0
	}
	return *v
}

func channelTypeToProto(t channel.Type) notificationpb.ChannelType {
	switch t {
	case channel.TypeEMAIL:
		return notificationpb.ChannelType_CHANNEL_TYPE_EMAIL
	case channel.TypeSMS:
		return notificationpb.ChannelType_CHANNEL_TYPE_SMS
	case channel.TypeSLACK:
		return notificationpb.ChannelType_CHANNEL_TYPE_SLACK
	case channel.TypeSSE:
		return notificationpb.ChannelType_CHANNEL_TYPE_SSE
	default:
		return notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED
	}
}

func templateChannelTypeToProto(t template.ChannelType) notificationpb.ChannelType {
	switch t {
	case template.ChannelTypeEMAIL:
		return notificationpb.ChannelType_CHANNEL_TYPE_EMAIL
	case template.ChannelTypeSMS:
		return notificationpb.ChannelType_CHANNEL_TYPE_SMS
	case template.ChannelTypeSLACK:
		return notificationpb.ChannelType_CHANNEL_TYPE_SLACK
	case template.ChannelTypeSSE:
		return notificationpb.ChannelType_CHANNEL_TYPE_SSE
	default:
		return notificationpb.ChannelType_CHANNEL_TYPE_UNSPECIFIED
	}
}
