package httpapi

import (
	"errors"
	"net/http"
	"os"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stream"
)

// StreamDeps are the services behind the live stream route.
type StreamDeps struct {
	Hub   *stream.Hub
	Audit *audit.Writer
}

// RegisterStream mounts GET /stream (contracts/stream.md).
func (s *Server) RegisterStream(d StreamDeps) {
	instance, _ := os.Hostname()
	s.MustHandle("GET", Prefix+"/stream", func(w http.ResponseWriter, r *http.Request) {
		id, err := Caller(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		sub, err := d.Hub.Subscribe(r.Context(), id.TenantID, id.UserID, r.Header.Get("Last-Event-ID"))
		if err != nil {
			if errors.Is(err, stream.ErrTooMany) {
				if d.Audit != nil {
					_ = d.Audit.Emit(audit.Event{Type: audit.StreamRefused, TenantID: id.TenantID, ActorKind: "user", ActorID: id.UserID, SubjectKind: "system", Outcome: "refused", Reason: "too_many_streams"})
				}
				Fail(w, r, nil, ErrRateLimited)
				return
			}
			Fail(w, r, s.rt.Logger(), err)
			return
		}
		if d.Audit != nil {
			_ = d.Audit.Emit(audit.Event{Type: audit.StreamOpened, TenantID: id.TenantID, ActorKind: "user", ActorID: id.UserID, SubjectKind: "system", Outcome: "ok", CorrelationID: RequestID(r)})
		}
		stream.ServeSSE(w, r, sub, instance, stream.Heartbeat, stream.MaxAge)
	})
}
