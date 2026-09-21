// Package stats reads per-tenant counts for operators.
package stats

import (
	"context"
	"time"

	"github.com/go-freya/freya/services/notification/internal/repo"
)

// Streams reports open live streams (the hub).
type Streams interface{ OpenStreams() int }

// Service computes statistics.
type Service struct {
	st      repo.Stats
	streams Streams
	now     func() time.Time
}

// New wires the service.
func New(st repo.Stats, streams Streams) *Service {
	return &Service{st: st, streams: streams, now: time.Now}
}

// View is the statistics payload.
type View struct {
	Channels      int64            `json:"channels"`
	Templates     int64            `json:"templates"`
	Notifications map[string]int64 `json:"notifications"`
	Messages      map[string]int64 `json:"messages"`
	OpenStreams   int              `json:"open_streams"`
	Operations24h int64            `json:"operations_24h"`
}

// Counts returns the tenant's statistics.
func (s *Service) Counts(ctx context.Context, tenantID string) (View, error) {
	st, err := s.st.TenantStats(ctx, tenantID, s.now())
	if err != nil {
		return View{}, err
	}
	v := View{Channels: st.Channels, Templates: st.Templates, Notifications: st.Notifications, Messages: st.Messages, Operations24h: st.Operations24h}
	if s.streams != nil {
		v.OpenStreams = s.streams.OpenStreams()
	}
	if v.Notifications == nil {
		v.Notifications = map[string]int64{}
	}
	if v.Messages == nil {
		v.Messages = map[string]int64{}
	}
	return v, nil
}
