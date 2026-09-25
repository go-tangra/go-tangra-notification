package contract

import (
	"testing"

	notificationv1 "github.com/go-tangra/go-tangra-notification/sdk/v4/api/proto/notification/v1"
)

// TestGRPCSurface proves the generated services match contracts/notification.v1.proto.
func TestGRPCSurface(t *testing.T) {
	file := notificationv1.File_notification_v1_notification_proto
	want := map[string]map[string]bool{
		"Notifier": {"Send": true, "SendTest": true},
		"Events":   {"Publish": true},
	}
	for name, methods := range want {
		svc := file.Services().ByName(protoName(name))
		if svc == nil {
			t.Fatalf("service %s missing", name)
		}
		for i := 0; i < svc.Methods().Len(); i++ {
			m := svc.Methods().Get(i)
			if !methods[string(m.Name())] {
				t.Errorf("%s: unexpected method %s", name, m.Name())
			}
			delete(methods, string(m.Name()))
			if m.IsStreamingClient() || m.IsStreamingServer() {
				t.Errorf("%s.%s streams", name, m.Name())
			}
		}
		if len(methods) != 0 {
			t.Errorf("%s: missing methods %v", name, methods)
		}
	}
	if file.Services().Len() != 2 {
		t.Fatalf("services %d", file.Services().Len())
	}
	msgs := file.Messages()
	for _, m := range []string{"SendRequest", "SendTestRequest"} {
		if msgs.ByName(protoName(m)).Fields().ByName("tenant_id") == nil {
			t.Errorf("%s lacks tenant_id", m)
		}
	}
	// Responses never carry rendered bodies or credentials.
	for _, f := range []string{"rendered_body", "password", "settings"} {
		if msgs.ByName("SendResponse").Fields().ByName(protoName(f)) != nil {
			t.Errorf("SendResponse carries %s", f)
		}
	}
	if msgs.ByName("PublishRequest").Fields().ByName("data") == nil || msgs.ByName("PublishResponse").Fields().ByName("event_id") == nil {
		t.Error("Publish shapes")
	}
}
