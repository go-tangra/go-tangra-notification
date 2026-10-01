package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/audit"
	"github.com/go-tangra/go-tangra-notification/v4/internal/stats"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
	"github.com/go-tangra/go-tangra-notification/v4/internal/transfer"
)

// OpsDeps are the services behind backup, stats, audit and health.
type OpsDeps struct {
	Transfer *transfer.Service
	MaxBytes int64
	Stats    *stats.Service
	Audit    audit.Querier
	Version  string
	Health   func(ctx context.Context) any
}

// RegisterOps mounts the operations routes (contracts §backup, §operations).
func (s *Server) RegisterOps(d OpsDeps) {
	s.MustHandle("POST", Prefix+"/backup/export", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			IncludeCredentials bool `json:"include_credentials"`
		}
		if r.ContentLength != 0 {
			if err := DecodeJSON(r, &in, 0); err != nil {
				Fail(w, r, nil, err)
				return
			}
		}
		doc, err := d.Transfer.Export(r.Context(), subj, in.IncludeCredentials)
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="notification-backup.json"`)
		WriteJSON(w, http.StatusOK, doc)
	})
	s.MustHandle("POST", Prefix+"/backup/import", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		limit := d.MaxBytes
		if limit <= 0 {
			limit = transfer.MaxBytes
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				Fail(w, r, nil, ErrBodyTooLarge)
				return
			}
			Fail(w, r, nil, ErrMalformed)
			return
		}
		doc, err := transfer.DecodeBounded(raw)
		if err != nil {
			if errors.Is(err, transfer.ErrTooLarge) {
				Fail(w, r, nil, ErrBodyTooLarge)
				return
			}
			WriteDetail(w, ErrValidation, map[string]any{"message": err.Error()})
			return
		}
		mode := r.URL.Query().Get("mode")
		if mode == "" {
			mode = "skip"
		}
		rep, err := d.Transfer.Import(r.Context(), subj, doc, mode)
		if err != nil {
			if errors.Is(err, transfer.ErrMode) {
				Fail(w, r, nil, ErrValidation)
				return
			}
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, rep)
	})
	s.MustHandle("GET", Prefix+"/stats", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Stats.Counts(r.Context(), subj.TenantID)
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("GET", Prefix+"/audit", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		f := audit.Filter{EventType: q.Get("event_type"), ActorID: q.Get("actor_id")}
		if f.From, f.To, err = window(q); err != nil {
			failParam(w, err)
			return
		}
		auditErr := func(err error) error {
			if errors.Is(err, audit.ErrFilter) {
				return ErrValidation
			}
			return domainError(err)
		}
		serveList(s, w, r, store.AuditList, auditErr,
			func() ([]audit.Item, string, error) {
				lf := f
				lf.Cursor = q.Get("cursor")
				lf.Limit, _ = strconv.Atoi(q.Get("limit"))
				page, err := audit.Query(r.Context(), d.Audit, subj.TenantID, lf)
				return page.Items, page.NextCursor, err
			},
			func(req listquery.Request) (listquery.Page[audit.Item], error) {
				return audit.QueryPage(r.Context(), d.Audit, subj.TenantID, f, req)
			})
	})
	s.MustHandle("GET", Prefix+"/health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := subjects(r); err != nil {
			Fail(w, r, nil, err)
			return
		}
		h := d.Health(r.Context())
		raw, _ := json.Marshal(h)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		status := "ok"
		for _, k := range []string{"database", "valkey"} {
			if v, ok := m[k].(string); ok && v != "ok" {
				status = "degraded"
			}
		}
		if m == nil {
			m = map[string]any{}
		}
		m["status"], m["version"] = status, d.Version
		WriteJSON(w, http.StatusOK, m)
	})
}
