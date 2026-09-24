// Package repodb binds repo.Store to the database: every call runs in a
// tenant-scoped transaction (RLS), system-scope calls in a system
// transaction. Covered by the tagged integration suite.
package repodb

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// DB implements repo.Store over *store.Store.
type DB struct {
	St *store.Store
	tx pgx.Tx // set inside Atomic
}

// New wraps the store.
func New(st *store.Store) *DB { return &DB{St: st} }

func (d *DB) run(ctx context.Context, scope store.Scope, fn func(tx pgx.Tx) error) error {
	if d.tx != nil {
		return fn(d.tx)
	}
	return d.St.Tx(ctx, scope, fn)
}

func (d *DB) tenant(ctx context.Context, tid string, fn func(tx pgx.Tx) error) error {
	return d.run(ctx, store.Scope{TenantID: tid}, fn)
}

func (d *DB) system(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return d.run(ctx, store.Scope{System: true}, fn)
}

// Atomic runs fn in one tenant transaction.
func (d *DB) Atomic(ctx context.Context, tenantID string, fn func(repo.Store) error) error {
	if d.tx != nil {
		return fn(d)
	}
	return d.St.Tx(ctx, store.Scope{TenantID: tenantID}, func(tx pgx.Tx) error { return fn(&DB{St: d.St, tx: tx}) })
}

// ---- channels

func (d *DB) InsertChannel(ctx context.Context, c store.Channel) error {
	return d.tenant(ctx, c.TenantID, func(tx pgx.Tx) error { return store.InsertChannel(ctx, tx, c) })
}
func (d *DB) GetChannel(ctx context.Context, tid, id string) (out store.Channel, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetChannel(ctx, tx, tid, id); return err })
	return
}
func (d *DB) ListChannels(ctx context.Context, tid, typ, after string, limit int) (out []store.Channel, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.ListChannels(ctx, tx, tid, typ, after, limit); return err })
	return
}
func (d *DB) UpdateChannel(ctx context.Context, c store.Channel) error {
	return d.tenant(ctx, c.TenantID, func(tx pgx.Tx) error { return store.UpdateChannel(ctx, tx, c) })
}
func (d *DB) ClearDefaultChannel(ctx context.Context, tid, typ, except string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.ClearDefaultChannel(ctx, tx, tid, typ, except) })
}
func (d *DB) DeleteChannel(ctx context.Context, tid, id string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteChannel(ctx, tx, tid, id) })
}

// ---- templates

func (d *DB) InsertTemplate(ctx context.Context, t store.Template) error {
	return d.tenant(ctx, t.TenantID, func(tx pgx.Tx) error { return store.InsertTemplate(ctx, tx, t) })
}
func (d *DB) GetTemplate(ctx context.Context, tid, id string) (out store.Template, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetTemplate(ctx, tx, tid, id); return err })
	return
}
func (d *DB) ListTemplates(ctx context.Context, tid string, ch *string, q, after string, limit int) (out []store.Template, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.ListTemplates(ctx, tx, tid, ch, q, after, limit); return err })
	return
}
func (d *DB) TemplatesByIDs(ctx context.Context, tid string, ids []string) (out []store.Template, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.TemplatesByIDs(ctx, tx, tid, ids); return err })
	return
}
func (d *DB) AllTemplates(ctx context.Context, tid string, limit int) (out []store.Template, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.AllTemplates(ctx, tx, tid, limit); return err })
	return
}
func (d *DB) UpdateTemplate(ctx context.Context, t store.Template) error {
	return d.tenant(ctx, t.TenantID, func(tx pgx.Tx) error { return store.UpdateTemplate(ctx, tx, t) })
}
func (d *DB) ClearDefaultTemplate(ctx context.Context, tid, ch, except string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.ClearDefaultTemplate(ctx, tx, tid, ch, except) })
}
func (d *DB) DeleteTemplate(ctx context.Context, tid, id string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteTemplate(ctx, tx, tid, id) })
}

// ---- log

func (d *DB) InsertLog(ctx context.Context, l store.LogRow) error {
	return d.tenant(ctx, l.TenantID, func(tx pgx.Tx) error { return store.InsertLog(ctx, tx, l) })
}
func (d *DB) SetLogOutcome(ctx context.Context, tid, id, status, errText, subject, body string, sentAt *time.Time) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error {
		return store.SetLogOutcome(ctx, tx, tid, id, status, errText, subject, body, sentAt)
	})
}
func (d *DB) GetLog(ctx context.Context, tid, id string) (out store.LogRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetLog(ctx, tx, tid, id); return err })
	return
}
func (d *DB) LogPage(ctx context.Context, tid string, f store.LogFilter) (out []store.LogRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.LogPage(ctx, tx, tid, f); return err })
	return
}
func (d *DB) ExpirePendingLogs(ctx context.Context, olderThan time.Time) (n int64, err error) {
	err = d.system(ctx, func(tx pgx.Tx) error { n, err = store.ExpirePendingLogs(ctx, tx, olderThan); return err })
	return
}

// ---- grants

func (d *DB) UpsertGrant(ctx context.Context, g store.Grant) (out store.Grant, err error) {
	err = d.tenant(ctx, g.TenantID, func(tx pgx.Tx) error { out, err = store.UpsertGrant(ctx, tx, g); return err })
	return
}
func (d *DB) GetGrant(ctx context.Context, tid, id string) (out store.Grant, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetGrant(ctx, tx, tid, id); return err })
	return
}
func (d *DB) DeleteGrant(ctx context.Context, tid, id string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteGrant(ctx, tx, tid, id) })
}
func (d *DB) GrantsOnResource(ctx context.Context, tid, rt, rid string) (out []store.Grant, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GrantsOnResource(ctx, tx, tid, rt, rid); return err })
	return
}
func (d *DB) GrantsForSubjects(ctx context.Context, tid, uid string, roles []string, now time.Time) (out []store.Grant, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GrantsForSubjects(ctx, tx, tid, uid, roles, now); return err })
	return
}
func (d *DB) DeleteGrantsOfResource(ctx context.Context, tid, rt, rid string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteGrantsOfResource(ctx, tx, tid, rt, rid) })
}
func (d *DB) DeleteGrantsOfSubject(ctx context.Context, tid, rt, rid, st, sid string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteGrantsOfSubject(ctx, tx, tid, rt, rid, st, sid) })
}

// ---- categories

func (d *DB) InsertCategory(ctx context.Context, c store.Category) error {
	return d.tenant(ctx, c.TenantID, func(tx pgx.Tx) error { return store.InsertCategory(ctx, tx, c) })
}
func (d *DB) GetCategory(ctx context.Context, tid, id string) (out store.Category, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetCategory(ctx, tx, tid, id); return err })
	return
}
func (d *DB) ListCategories(ctx context.Context, tid string) (out []store.Category, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.ListCategories(ctx, tx, tid); return err })
	return
}
func (d *DB) UpdateCategory(ctx context.Context, c store.Category) error {
	return d.tenant(ctx, c.TenantID, func(tx pgx.Tx) error { return store.UpdateCategory(ctx, tx, c) })
}
func (d *DB) DeleteCategory(ctx context.Context, tid, id string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteCategory(ctx, tx, tid, id) })
}

// ---- messages

func (d *DB) InsertMessage(ctx context.Context, m store.Message) error {
	return d.tenant(ctx, m.TenantID, func(tx pgx.Tx) error { return store.InsertMessage(ctx, tx, m) })
}
func (d *DB) GetMessage(ctx context.Context, tid, id string) (out store.Message, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetMessage(ctx, tx, tid, id); return err })
	return
}
func (d *DB) ListMessages(ctx context.Context, tid string, f store.MessageFilter) (out []store.Message, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.ListMessages(ctx, tx, tid, f); return err })
	return
}
func (d *DB) UpdateMessage(ctx context.Context, m store.Message) error {
	return d.tenant(ctx, m.TenantID, func(tx pgx.Tx) error { return store.UpdateMessage(ctx, tx, m) })
}
func (d *DB) SetMessageStatus(ctx context.Context, tid, id, status string, at *time.Time) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.SetMessageStatus(ctx, tx, tid, id, status, at) })
}
func (d *DB) DeleteMessage(ctx context.Context, tid, id string) error {
	return d.tenant(ctx, tid, func(tx pgx.Tx) error { return store.DeleteMessage(ctx, tx, tid, id) })
}
func (d *DB) ClaimDueMessages(ctx context.Context, now time.Time, lease time.Duration, limit int) (out []store.Message, err error) {
	err = d.system(ctx, func(tx pgx.Tx) error { out, err = store.ClaimDueMessages(ctx, tx, now, lease, limit); return err })
	return
}

// ---- inbox

func (d *DB) InsertInboxBatch(ctx context.Context, rows []store.InboxRow) (n int64, err error) {
	if len(rows) == 0 {
		return 0, nil
	}
	err = d.tenant(ctx, rows[0].TenantID, func(tx pgx.Tx) error { n, err = store.InsertInboxBatch(ctx, tx, rows); return err })
	return
}
func (d *DB) InboxPage(ctx context.Context, tid, rid, status string, cts time.Time, cid string, limit int) (out []store.InboxRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error {
		out, err = store.InboxPage(ctx, tx, tid, rid, status, cts, cid, limit)
		return err
	})
	return
}
func (d *DB) GetInboxEntry(ctx context.Context, tid, rid, id string) (out store.InboxRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.GetInboxEntry(ctx, tx, tid, rid, id); return err })
	return
}
func (d *DB) InboxUnread(ctx context.Context, tid, rid string) (n int64, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { n, err = store.InboxUnread(ctx, tx, tid, rid); return err })
	return
}
func (d *DB) SetInboxStatus(ctx context.Context, tid, rid string, ids []string, status string) (n int64, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { n, err = store.SetInboxStatus(ctx, tx, tid, rid, ids, status); return err })
	return
}
func (d *DB) RevokeUnread(ctx context.Context, tid, mid string) (out []string, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.RevokeUnread(ctx, tx, tid, mid); return err })
	return
}
func (d *DB) MessageRecipients(ctx context.Context, tid, mid, status, after string, limit int) (out []store.InboxRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error {
		out, err = store.MessageRecipients(ctx, tx, tid, mid, status, after, limit)
		return err
	})
	return
}

// ---- audit & stats

func (d *DB) InsertAuditRows(ctx context.Context, rows []store.AuditRow) error {
	return d.system(ctx, func(tx pgx.Tx) error { return store.InsertAuditRows(ctx, tx, rows) })
}
func (d *DB) QueryAudit(ctx context.Context, tid, et, actor string, from, to, cursor time.Time, limit int) (out []store.AuditRow, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error {
		out, err = store.QueryAudit(ctx, tx, tid, et, actor, from, to, cursor, limit)
		return err
	})
	return
}
func (d *DB) TenantStats(ctx context.Context, tid string, now time.Time) (out store.Stats, err error) {
	err = d.tenant(ctx, tid, func(tx pgx.Tx) error { out, err = store.TenantStats(ctx, tx, tid, now); return err })
	return
}
