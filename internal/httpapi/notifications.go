package httpapi

import (
	"net/http"
	"time"

	"github.com/go-freya/freya/services/notification/internal/notify"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// MaxSendBody bounds send requests (64 KiB variables + slack).
const MaxSendBody = 80 << 10

// RegisterNotifications mounts the send and log routes (contracts §notifications).
func (s *Server) RegisterNotifications(d NotifyDeps) {
	s.MustHandle("POST", Prefix+"/notifications/send", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			TemplateID string            `json:"template_id"`
			ChannelID  string            `json:"channel_id"`
			Recipient  string            `json:"recipient"`
			Variables  map[string]string `json:"variables"`
		}
		if err := DecodeJSON(r, &in, MaxSendBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Sender.Send(r.Context(), subj, notify.SendInput{TemplateID: in.TemplateID, ChannelID: in.ChannelID, Recipient: in.Recipient, Variables: in.Variables, CorrelationID: RequestID(r)})
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		if out.Status == "failed" && out.Error == "no_provider" {
			s.fail(w, r, ErrNoProvider)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("GET", Prefix+"/notifications", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		f := store.LogFilter{ChannelID: q.Get("channel_id"), TemplateID: q.Get("template_id"), Recipient: q.Get("recipient"), Status: q.Get("status"), Limit: limitParam(r)}
		if v := q.Get("from"); v != "" {
			f.From, _ = time.Parse(time.RFC3339, v)
		}
		if v := q.Get("to"); v != "" {
			f.To, _ = time.Parse(time.RFC3339, v)
		}
		if f.CursorTS, f.CursorID, err = notify.DecodeCursor(q.Get("cursor")); err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		items, next, err := d.Sender.ListLog(r.Context(), subj, f, hasPermission(r, d.Perms, "stats:read"))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	})
	s.MustHandle("GET", Prefix+"/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Sender.GetLog(r.Context(), subj, r.PathValue("id"), hasPermission(r, d.Perms, "stats:read"))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
