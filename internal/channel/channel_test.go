package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

func TestRegistryAndRecipients(t *testing.T) {
	r := Builtin(Options{AllowPlaintext: true, DialTimeout: time.Second})
	for _, typ := range Types {
		if !ValidType(typ) {
			t.Errorf("%s invalid", typ)
		}
		p, err := r.Get(typ)
		if err != nil || p.Type() != typ {
			t.Fatalf("%s: %v", typ, err)
		}
		if len(r.SecretFields(typ)) == 0 {
			t.Errorf("%s: no secret fields", typ)
		}
	}
	if ValidType("pigeon") || r.SecretFields("pigeon") != nil {
		t.Fatal("unknown type accepted")
	}
	if _, err := r.Get("pigeon"); !errors.Is(err, ErrType) {
		t.Fatalf("unknown type: %v", err)
	}
	sms, _ := r.Get(TypeSMS)
	if err := sms.Validate(sealed.Settings{"api_key": "k"}); err != nil {
		t.Fatal(err)
	}
	if err := sms.Send(context.Background(), sealed.Settings{}, Message{To: "+1555"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nop send: %v", err)
	}
	for _, ok := range []string{"alice@example.org", "Alice <alice@example.org>"} {
		if err := ValidateRecipient(TypeEmail, ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "not an address", "a@b.c\r\nBcc: x@y.z", "a@b.c, d@e.f", "x\x00y"} {
		if err := ValidateRecipient(TypeEmail, bad); !errors.Is(err, ErrRecipient) {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateRecipient(TypeSMS, "+15551234567"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecipient(TypeSMS, string(make([]byte, 513))); !errors.Is(err, ErrRecipient) {
		t.Fatal("long recipient accepted")
	}
}
