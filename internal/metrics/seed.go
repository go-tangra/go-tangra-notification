package metrics

import (
	"context"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/template"
)

// Seed loads initial gauge values from the database.
// Called once at startup so Prometheus has accurate values from the start.
func (c *Collector) Seed(ctx context.Context, entClient *entCrud.EntClient[*ent.Client]) {
	c.log.Info("Seeding Prometheus metrics from database...")

	client := entClient.Client()

	// Seed channel metrics
	c.seedChannels(ctx, client)

	// Seed template metrics
	c.seedTemplates(ctx, client)

	// Seed notification log metrics
	c.seedNotifications(ctx, client)

	c.log.Info("Prometheus metrics seeded successfully")
}

func (c *Collector) seedChannels(ctx context.Context, client *ent.Client) {
	channelTypes := []channel.Type{
		channel.TypeEMAIL,
		channel.TypeSMS,
		channel.TypeSLACK,
		channel.TypeSSE,
	}

	var totalCount int
	for _, t := range channelTypes {
		count, err := client.Channel.Query().
			Where(channel.TypeEQ(t)).
			Where(channel.DeleteTimeIsNil()).
			Count(ctx)
		if err != nil {
			c.log.Errorf("Failed to seed channel count for type %s: %v", t, err)
			continue
		}
		c.ChannelsByType.WithLabelValues(string(t)).Set(float64(count))
		totalCount += count
	}
	c.ChannelsTotal.Set(float64(totalCount))
}

func (c *Collector) seedTemplates(ctx context.Context, client *ent.Client) {
	channelTypes := []channel.Type{
		channel.TypeEMAIL,
		channel.TypeSMS,
		channel.TypeSLACK,
		channel.TypeSSE,
	}

	var totalCount int
	for _, t := range channelTypes {
		// Find all channel IDs of this type
		channelIDs, err := client.Channel.Query().
			Where(channel.TypeEQ(t)).
			Where(channel.DeleteTimeIsNil()).
			IDs(ctx)
		if err != nil {
			c.log.Errorf("Failed to query channels for type %s: %v", t, err)
			continue
		}
		if len(channelIDs) == 0 {
			c.TemplatesByChannelType.WithLabelValues(string(t)).Set(0)
			continue
		}

		count, err := client.Template.Query().
			Where(template.ChannelIDIn(channelIDs...)).
			Where(template.DeleteTimeIsNil()).
			Count(ctx)
		if err != nil {
			c.log.Errorf("Failed to seed template count for channel type %s: %v", t, err)
			continue
		}
		c.TemplatesByChannelType.WithLabelValues(string(t)).Set(float64(count))
		totalCount += count
	}
	c.TemplatesTotal.Set(float64(totalCount))
}

func (c *Collector) seedNotifications(ctx context.Context, client *ent.Client) {
	statuses := []notificationlog.Status{
		notificationlog.StatusPENDING,
		notificationlog.StatusSENT,
		notificationlog.StatusFAILED,
	}

	var totalCount int
	for _, s := range statuses {
		count, err := client.NotificationLog.Query().
			Where(notificationlog.StatusEQ(s)).
			Where(notificationlog.DeleteTimeIsNil()).
			Count(ctx)
		if err != nil {
			c.log.Errorf("Failed to seed notification count for status %s: %v", s, err)
			continue
		}
		c.NotificationsByStatus.WithLabelValues(string(s)).Set(float64(count))
		totalCount += count
	}
	c.NotificationsTotal.Set(float64(totalCount))

	channelTypes := []notificationlog.ChannelType{
		notificationlog.ChannelTypeEMAIL,
		notificationlog.ChannelTypeSMS,
		notificationlog.ChannelTypeSLACK,
		notificationlog.ChannelTypeSSE,
	}

	for _, t := range channelTypes {
		count, err := client.NotificationLog.Query().
			Where(notificationlog.ChannelTypeEQ(t)).
			Where(notificationlog.DeleteTimeIsNil()).
			Count(ctx)
		if err != nil {
			c.log.Errorf("Failed to seed notification count for channel type %s: %v", t, err)
			continue
		}
		c.NotificationsByChannelType.WithLabelValues(string(t)).Set(float64(count))
	}
}
