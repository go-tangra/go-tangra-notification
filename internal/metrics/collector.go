package metrics

import (
	"context"
	"os"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	commonMetrics "github.com/go-tangra/go-tangra-common/metrics"
)

const namespace = "tangra"
const subsystem = "notification"

// Collector holds all Prometheus metrics for the notification module.
type Collector struct {
	log    *log.Helper
	server *commonMetrics.MetricsServer

	// Channel metrics
	ChannelsByType *prometheus.GaugeVec
	ChannelsTotal  prometheus.Gauge

	// Template metrics
	TemplatesByChannelType *prometheus.GaugeVec
	TemplatesTotal         prometheus.Gauge

	// Notification log metrics
	NotificationsByStatus      *prometheus.GaugeVec
	NotificationsByChannelType *prometheus.GaugeVec
	NotificationsTotal         prometheus.Gauge
}

// NewCollector creates and registers all notification Prometheus metrics.
func NewCollector(ctx *bootstrap.Context) *Collector {
	c := &Collector{
		log: ctx.NewLoggerHelper("notification/metrics"),

		ChannelsByType: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "channels_by_type",
			Help:      "Number of channels by type.",
		}, []string{"type"}),

		ChannelsTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "channels_total",
			Help:      "Total number of channels.",
		}),

		TemplatesByChannelType: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "templates_by_channel_type",
			Help:      "Number of templates by channel type.",
		}, []string{"channel_type"}),

		TemplatesTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "templates_total",
			Help:      "Total number of templates.",
		}),

		NotificationsByStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "notifications_by_status",
			Help:      "Number of notification logs by status.",
		}, []string{"status"}),

		NotificationsByChannelType: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "notifications_by_channel_type",
			Help:      "Number of notification logs by channel type.",
		}, []string{"channel_type"}),

		NotificationsTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "notifications_total",
			Help:      "Total number of notification logs.",
		}),
	}

	collectors := []prometheus.Collector{
		c.ChannelsByType,
		c.ChannelsTotal,
		c.TemplatesByChannelType,
		c.TemplatesTotal,
		c.NotificationsByStatus,
		c.NotificationsByChannelType,
		c.NotificationsTotal,
	}
	for _, col := range collectors {
		if err := prometheus.Register(col); err != nil {
			c.log.Warnf("Failed to register metric: %v", err)
		}
	}

	addr := os.Getenv("METRICS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:10310"
	}
	c.server = commonMetrics.NewMetricsServer(addr, nil, ctx.GetLogger())

	// M12: Start metrics server in background; log bind errors
	startErr := make(chan error, 1)
	go func() {
		startErr <- c.server.Start()
	}()

	// Give the server a moment to bind or fail
	select {
	case err := <-startErr:
		if err != nil {
			c.log.Errorf("Metrics server failed to start: %v", err)
		}
	default:
		// Server is starting normally
	}

	return c
}

// Stop shuts down the metrics HTTP server.
func (c *Collector) Stop(ctx context.Context) {
	if c.server != nil {
		c.server.Stop(ctx)
	}
}

// validChannelTypes is the set of known channel types for label validation.
var validChannelTypes = map[string]bool{
	"EMAIL": true, "SMS": true, "SLACK": true, "SSE": true,
}

// validStatuses is the set of known notification statuses for label validation.
var validStatuses = map[string]bool{
	"PENDING": true, "SENT": true, "FAILED": true,
}

// sanitizeChannelType returns the channel type if known, or "unknown" to prevent label cardinality explosion.
func sanitizeChannelType(t string) string {
	if validChannelTypes[t] {
		return t
	}
	return "unknown"
}

// sanitizeStatus returns the status if known, or "unknown".
func sanitizeStatus(s string) string {
	if validStatuses[s] {
		return s
	}
	return "unknown"
}

// --- Channel helpers ---

// ChannelCreated increments counters for a newly created channel.
func (c *Collector) ChannelCreated(channelType string) {
	channelType = sanitizeChannelType(channelType)
	c.ChannelsByType.WithLabelValues(channelType).Inc()
	c.ChannelsTotal.Inc()
}

// ChannelDeleted decrements counters for a deleted channel.
func (c *Collector) ChannelDeleted(channelType string) {
	channelType = sanitizeChannelType(channelType)
	c.ChannelsByType.WithLabelValues(channelType).Dec()
	c.ChannelsTotal.Dec()
}

// --- Template helpers ---

// TemplateCreated increments counters for a newly created template.
func (c *Collector) TemplateCreated(channelType string) {
	channelType = sanitizeChannelType(channelType)
	c.TemplatesByChannelType.WithLabelValues(channelType).Inc()
	c.TemplatesTotal.Inc()
}

// TemplateDeleted decrements counters for a deleted template.
func (c *Collector) TemplateDeleted(channelType string) {
	channelType = sanitizeChannelType(channelType)
	c.TemplatesByChannelType.WithLabelValues(channelType).Dec()
	c.TemplatesTotal.Dec()
}

// --- Notification log helpers ---

// NotificationCreated increments counters for a newly created notification log.
func (c *Collector) NotificationCreated(status, channelType string) {
	status = sanitizeStatus(status)
	channelType = sanitizeChannelType(channelType)
	c.NotificationsByStatus.WithLabelValues(status).Inc()
	c.NotificationsByChannelType.WithLabelValues(channelType).Inc()
	c.NotificationsTotal.Inc()
}

// NotificationStatusChanged adjusts the status gauge when a notification's status changes.
func (c *Collector) NotificationStatusChanged(oldStatus, newStatus string) {
	oldStatus = sanitizeStatus(oldStatus)
	newStatus = sanitizeStatus(newStatus)
	c.NotificationsByStatus.WithLabelValues(oldStatus).Dec()
	c.NotificationsByStatus.WithLabelValues(newStatus).Inc()
}
