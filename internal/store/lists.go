package store

import (
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"
)

// List definitions of the notification tables (go-tangra
// specs/032-server-side-tables, contracts/sortable-fields.md "notification").
// Sort fields map to constant SQL expressions only (the page queries alias
// channels as c, templates as t, messages as m, categories as k and the audit
// hypertable as a); the memstore sorts the same public names in Go. Fields
// over NOT NULL columns are NotNull (no NULLS LAST), so the (tenant_id, col)
// indexes serve both directions; the template channel name comes from a LEFT
// JOIN and stays nullable. The backup walks and the default-channel
// bookkeeping keep their name keysets.
var (
	// ChannelList pages GET /channels: name order by default.
	ChannelList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "c.name", Text: true, NotNull: true},
			"type":       {Expr: "c.type", NotNull: true},
			"created_at": {Expr: "c.created_at", DefaultDir: listquery.Desc, NotNull: true},
		},
		Default: "name", TieBreak: "c.id",
	}
	// TemplateList pages GET /templates; channel orders by the bound
	// channel's name (system templates without a channel come last).
	TemplateList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "t.name", Text: true, NotNull: true},
			"channel":    {Expr: "c.name", Text: true},
			"updated_at": {Expr: "t.updated_at", DefaultDir: listquery.Desc, NotNull: true},
		},
		Default: "name", TieBreak: "t.id",
	}
	// MessageList pages GET /messages: newest first by default; subject is
	// the message title.
	MessageList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"created_at": {Expr: "m.created_at", DefaultDir: listquery.Desc, NotNull: true},
			"subject":    {Expr: "m.title", Text: true, NotNull: true},
			"status":     {Expr: "m.status", NotNull: true},
		},
		Default: "created_at", TieBreak: "m.id",
	}
	// LogList pages GET /notifications (the notification_log hypertable)
	// within a time window (LogWindow when from/to are absent); channel is
	// the channel type recorded on the entry.
	LogList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"created_at": {Expr: "created_at", DefaultDir: listquery.Desc, NotNull: true},
			"status":     {Expr: "status", NotNull: true},
			"channel":    {Expr: "channel_type", NotNull: true},
		},
		Default: "created_at", TieBreak: "id",
	}
	// CategoryList pages GET /categories: the administrator-chosen sort
	// order by default.
	CategoryList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "k.name", Text: true, NotNull: true},
			"sort_order": {Expr: "k.sort", NotNull: true},
		},
		Default: "sort_order", TieBreak: "k.id",
	}
	// AuditList pages GET /audit (the audit hypertable) within a time window.
	// The events carry no id: rows with an equal timestamp live in the same
	// chunk, where the physical row id is unique and stable (the table is
	// append-only for the application role), so (ts, ctid) orders totally.
	AuditList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"ts": {Expr: "a.ts", DefaultDir: listquery.Desc, NotNull: true},
		},
		Default: "ts", TieBreak: "a.ctid", DefaultSize: 50,
	}
)

// LogWindow is the default time window of the log and audit lists when the
// request names neither from nor to (research D6): exact counts stay bounded
// on the hypertables.
const LogWindow = 7 * 24 * time.Hour

// MaxSpan caps an explicit [from, to] window of the log and audit lists
// (032 security review F-2): a wide from would otherwise force an exact
// count and OFFSET over the whole hypertable on every page.
const MaxSpan = 90 * 24 * time.Hour

// ErrSpan refuses a window wider than MaxSpan; it names the from parameter
// (validation_failed {param: from}) and never carries the value.
var ErrSpan = &listquery.Error{Param: "from"}

// Window completes a [from, to] filter: to defaults to now (plus a minute of
// clock slack), from to LogWindow before to. A window from an explicit from
// to to (now when absent) wider than MaxSpan is ErrSpan.
func Window(from, to, now time.Time) (time.Time, time.Time, error) {
	end := to
	if end.IsZero() {
		end = now
	}
	if !from.IsZero() && end.Sub(from) > MaxSpan {
		return time.Time{}, time.Time{}, ErrSpan
	}
	if to.IsZero() {
		to = now.Add(time.Minute)
	}
	if from.IsZero() {
		from = to.Add(-LogWindow)
	}
	return from, to, nil
}

// Visible restricts a list to the records the caller may read: every record
// of the tenant (All, tenant administrators) or the listed ids. The zero
// value shows nothing, so a forgotten restriction fails closed.
type Visible struct {
	All bool
	IDs []string
}

// Allows reports whether id is visible.
func (v Visible) Allows(id string) bool {
	if v.All {
		return true
	}
	for _, x := range v.IDs {
		if x == id {
			return true
		}
	}
	return false
}

// ListRequest completes r with the Spec's defaults (a zero Request from an
// internal caller pages with the defaults); an invalid hand-built Request
// falls back to the defaults entirely.
func ListRequest(r listquery.Request, s listquery.Spec) listquery.Request {
	out, err := listquery.New(r.Page, r.PageSize, r.Sort, r.Order, s)
	if err != nil {
		out, _ = listquery.New(0, 0, "", "", s)
	}
	return out
}
