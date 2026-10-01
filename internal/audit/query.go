package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Querier reads the audit hypertable (database or in-memory).
type Querier interface {
	QueryAudit(ctx context.Context, tenantID, eventType, actorID string, from, to, cursor time.Time, limit int) ([]store.AuditRow, error)
	PageAudit(ctx context.Context, tenantID, eventType, actorID string, from, to time.Time, req listquery.Request) ([]store.AuditRow, int, listquery.Request, error)
}

// Filter selects events; zero values mean "any".
type Filter struct {
	ActorID   string
	EventType string
	From, To  time.Time
	Cursor    string
	Limit     int // ≤ 200, default 50
}

// Item is one event as returned to administrators.
type Item struct {
	TS            time.Time       `json:"ts"`
	EventType     string          `json:"event_type"`
	ActorKind     string          `json:"actor_kind"`
	ActorID       string          `json:"actor_id,omitempty"`
	SubjectKind   string          `json:"subject_kind,omitempty"`
	SubjectID     string          `json:"subject_id,omitempty"`
	SubjectName   string          `json:"subject_name,omitempty"` // channel/template name or message title while it exists
	Outcome       string          `json:"outcome"`
	Reason        string          `json:"reason,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Details       json.RawMessage `json:"details"`
}

// Page is a cursor-paged result.
type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ErrFilter is returned for malformed filters.
var ErrFilter = errors.New("audit: invalid filter")

// check validates the filter's event type and window order.
func (f Filter) check() error {
	if f.EventType != "" && !Known(f.EventType) {
		return ErrFilter
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Before(f.From) {
		return ErrFilter
	}
	return nil
}

func itemOf(r store.AuditRow) Item {
	details := r.Details
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	return Item{TS: r.TS, EventType: r.EventType, ActorKind: r.ActorKind, ActorID: r.ActorID, SubjectKind: r.SubjectKind,
		SubjectID: r.SubjectID, SubjectName: r.SubjectName, Outcome: r.Outcome, Reason: r.Reason, CorrelationID: r.CorrelationID, Details: details}
}

// Query lists events of one tenant newest first with the legacy cursor
// (one more release; QueryPage is the list contract). Without from/to it
// covers the default window (store.Window).
func Query(ctx context.Context, q Querier, tenantID string, f Filter) (Page, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if err := f.check(); err != nil {
		return Page{}, err
	}
	var cursor time.Time
	if f.Cursor != "" {
		n, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil {
			return Page{}, ErrFilter
		}
		cursor = time.Unix(0, n)
	}
	from, to, err := store.Window(f.From, f.To, time.Now())
	if err != nil {
		return Page{}, err
	}
	rows, err := q.QueryAudit(ctx, tenantID, f.EventType, f.ActorID, from, to, cursor, f.Limit+1)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: []Item{}}
	for i, r := range rows {
		if i == f.Limit {
			page.NextCursor = strconv.FormatInt(rows[i-1].TS.UnixNano(), 10)
			break
		}
		page.Items = append(page.Items, itemOf(r))
	}
	return page, nil
}

// QueryPage lists events of one tenant on the list contract (store.AuditList)
// within the filter's window (store.Window: the last 7 days without from/to).
// Cursor and Limit are ignored.
func QueryPage(ctx context.Context, q Querier, tenantID string, f Filter, req listquery.Request) (listquery.Page[Item], error) {
	if err := f.check(); err != nil {
		return listquery.Page[Item]{}, err
	}
	from, to, err := store.Window(f.From, f.To, time.Now())
	if err != nil {
		return listquery.Page[Item]{}, err
	}
	rows, total, applied, err := q.PageAudit(ctx, tenantID, f.EventType, f.ActorID, from, to, store.ListRequest(req, store.AuditList))
	if err != nil {
		return listquery.Page[Item]{}, err
	}
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		items = append(items, itemOf(r))
	}
	return listquery.NewPage(items, total, applied), nil
}
