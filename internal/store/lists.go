package store

import (
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"
)

// List definitions of the notification tables (go-tangra
// specs/032-server-side-tables, contracts/sortable-fields.md "notification").
// Sort fields map to constant SQL expressions only (the page queries alias
// channels as c, templates as t, messages as m, categories as k and the audit
// hypertable as a); the memstore sorts the same public names in Go. The
// backup walks and the default-channel bookkeeping keep their name keysets.
var (
	// ChannelList pages GET /channels: name order by default.
	ChannelList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "c.name", Text: true},
			"type":       {Expr: "c.type"},
			"created_at": {Expr: "c.created_at", DefaultDir: listquery.Desc},
		},
		Default: "name", TieBreak: "c.id",
	}
	// TemplateList pages GET /templates; channel orders by the bound
	// channel's name (system templates without a channel come last).
	TemplateList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "t.name", Text: true},
			"channel":    {Expr: "c.name", Text: true},
			"updated_at": {Expr: "t.updated_at", DefaultDir: listquery.Desc},
		},
		Default: "name", TieBreak: "t.id",
	}
	// MessageList pages GET /messages: newest first by default; subject is
	// the message title.
	MessageList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"created_at": {Expr: "m.created_at", DefaultDir: listquery.Desc},
			"subject":    {Expr: "m.title", Text: true},
			"status":     {Expr: "m.status"},
		},
		Default: "created_at", TieBreak: "m.id",
	}
	// LogList pages GET /notifications (the notification_log hypertable)
	// within a time window (LogWindow when from/to are absent); channel is
	// the channel type recorded on the entry.
	LogList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"created_at": {Expr: "created_at", DefaultDir: listquery.Desc},
			"status":     {Expr: "status"},
			"channel":    {Expr: "channel_type"},
		},
		Default: "created_at", TieBreak: "id",
	}
	// CategoryList pages GET /categories: the administrator-chosen sort
	// order by default.
	CategoryList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"name":       {Expr: "k.name", Text: true},
			"sort_order": {Expr: "k.sort"},
		},
		Default: "sort_order", TieBreak: "k.id",
	}
	// AuditList pages GET /audit (the audit hypertable) within a time window.
	// The events carry no id: rows with an equal timestamp live in the same
	// chunk, where the physical row id is unique and stable (the table is
	// append-only for the application role), so (ts, ctid) orders totally.
	AuditList = listquery.Spec{
		Fields: map[string]listquery.Field{
			"ts": {Expr: "a.ts", DefaultDir: listquery.Desc},
		},
		Default: "ts", TieBreak: "a.ctid", DefaultSize: 50,
	}
)

// LogWindow is the default time window of the log and audit lists when the
// request names neither from nor to (research D6): exact counts stay bounded
// on the hypertables.
const LogWindow = 7 * 24 * time.Hour

// Window completes a [from, to] filter: to defaults to now (plus a minute of
// clock slack), from to LogWindow before to.
func Window(from, to, now time.Time) (time.Time, time.Time) {
	if to.IsZero() {
		to = now.Add(time.Minute)
	}
	if from.IsZero() {
		from = to.Add(-LogWindow)
	}
	return from, to
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
