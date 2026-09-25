package notifyclient

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
)

type fakeNotifier struct {
	notificationv1.NotifierClient
	req  *notificationv1.SendRequest
	resp *notificationv1.SendResponse
	err  error
}

func (f *fakeNotifier) Send(_ context.Context, in *notificationv1.SendRequest, _ ...grpc.CallOption) (*notificationv1.SendResponse, error) {
	f.req = in
	return f.resp, f.err
}

func TestSendKeyRequest(t *testing.T) {
	f := &fakeNotifier{resp: &notificationv1.SendResponse{LogId: "l1", Status: notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT}}
	c := &Client{notifier: f, timeout: DefaultTimeout}
	vars := map[string]string{"link": "https://x/a?token=t"}
	res, err := c.SendKey(context.Background(), "t1", "auth.invite", "a@example.org", vars, "c1")
	if err != nil || !res.Sent || res.LogID != "l1" || res.Retryable {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	r := f.req
	if r.GetTenantId() != "t1" || r.GetTemplateKey() != "auth.invite" || r.GetTemplateId() != "" ||
		r.GetRecipient() != "a@example.org" || r.GetCorrelationId() != "c1" || r.GetVariables()["link"] != vars["link"] {
		t.Fatalf("request %+v", r)
	}
}

func TestSendKeyFailedOutcome(t *testing.T) {
	for _, retryable := range []bool{true, false} {
		f := &fakeNotifier{resp: &notificationv1.SendResponse{LogId: "l2", Status: notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED, Error: "relay said no", Retryable: retryable}}
		c := &Client{notifier: f, timeout: DefaultTimeout}
		res, err := c.SendKey(context.Background(), "t", "auth.invite", "a@example.org", nil, "")
		if err != nil || res.Sent || res.Retryable != retryable || res.Reason != "relay said no" || res.LogID != "l2" {
			t.Fatalf("retryable=%v: res=%+v err=%v", retryable, res, err)
		}
	}
}

func TestSendKeyPendingIsRetryable(t *testing.T) {
	f := &fakeNotifier{resp: &notificationv1.SendResponse{LogId: "l3", Status: notificationv1.DeliveryStatus_DELIVERY_STATUS_PENDING}}
	c := &Client{notifier: f, timeout: DefaultTimeout}
	res, err := c.SendKey(context.Background(), "t", "auth.invite", "a@example.org", nil, "")
	if err != nil || res.Sent || !res.Retryable {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSendKeyStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		code      codes.Code
		retryable bool
	}{
		{codes.Unavailable, true},
		{codes.DeadlineExceeded, true},
		{codes.ResourceExhausted, true},
		{codes.Aborted, true},
		{codes.PermissionDenied, false},
		{codes.NotFound, false},
		{codes.InvalidArgument, false},
		{codes.FailedPrecondition, false},
	} {
		f := &fakeNotifier{err: status.Error(tc.code, "x")}
		c := &Client{notifier: f, timeout: DefaultTimeout}
		res, err := c.SendKey(context.Background(), "t", "auth.invite", "a@example.org", nil, "")
		if tc.retryable {
			if err != nil || res.Sent || !res.Retryable {
				t.Errorf("%v: res=%+v err=%v", tc.code, res, err)
			}
			continue
		}
		if err == nil || status.Code(err) != tc.code {
			t.Errorf("%v: want permanent error, got res=%+v err=%v", tc.code, res, err)
		}
	}
	// A non-status error (e.g. a context error from the caller) is permanent
	// for this call; the caller decides about its own context.
	f := &fakeNotifier{err: errors.New("boom")}
	c := &Client{notifier: f, timeout: DefaultTimeout}
	if _, err := c.SendKey(context.Background(), "t", "k.x", "a@example.org", nil, ""); err == nil {
		t.Fatal("plain error swallowed")
	}
}
