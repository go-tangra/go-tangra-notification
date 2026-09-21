package stats

import (
	"context"
	"errors"
	"testing"

	"github.com/go-freya/freya/services/notification/internal/memstore"
	"github.com/go-freya/freya/services/notification/internal/store"
)

type streams int

func (s streams) OpenStreams() int { return int(s) }

func TestCounts(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	const tA = "0190f7c2-6a3e-7c1a-9b2e-2f6f9d1b4c55"
	_ = ms.InsertChannel(ctx, store.Channel{ID: store.NewID(), TenantID: tA, Name: "c", Type: "email", SettingsSealed: []byte("x")})
	_ = ms.InsertLog(ctx, store.LogRow{ID: store.NewID(), TenantID: tA, ChannelType: "email", Status: "sent", Recipient: "a@b"})
	_ = ms.InsertMessage(ctx, store.Message{ID: store.NewID(), TenantID: tA, Title: "t", Status: "draft"})
	v, err := New(ms, streams(3)).Counts(ctx, tA)
	if err != nil || v.Channels != 1 || v.Templates != 0 || v.Notifications["sent"] != 1 || v.Messages["draft"] != 1 || v.OpenStreams != 3 {
		t.Fatalf("%v %+v", err, v)
	}
	v, err = New(ms, nil).Counts(ctx, "other")
	if err != nil || v.OpenStreams != 0 || v.Notifications == nil || v.Messages == nil || len(v.Notifications) != 0 {
		t.Fatalf("empty %v %+v", err, v)
	}
	ms.FailOn("TenantStats", errors.New("down"))
	if _, err := New(ms, nil).Counts(ctx, tA); err == nil {
		t.Fatal("outage")
	}
}
