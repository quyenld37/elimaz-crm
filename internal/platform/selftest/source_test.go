package selftest_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const secret = "0123456789abcdef0123456789abcdef"

func sign(body []byte, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestKeyIsSelftest(t *testing.T) {
	var s webhooks.Source = selftest.NewSource(secret)
	if s.Key() != "selftest" {
		t.Fatalf("Key = %q", s.Key())
	}
}

func TestVerifySignature(t *testing.T) {
	s := selftest.NewSource(secret)
	body := []byte(`{"events":[]}`)
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"đúng", sign(body, secret), true},
		{"sai khóa", sign(body, "khac"), false},
		{"thiếu tiền tố", sign(body, secret)[len("sha256="):], false},
		{"không phải hex", "sha256=zz", false},
		{"rỗng", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.header != "" {
				h.Set("X-Selftest-Signature", c.header)
			}
			if got := s.VerifySignature(body, h); got != c.want {
				t.Fatalf("VerifySignature = %v, muốn %v", got, c.want)
			}
		})
	}
	h := http.Header{}
	h.Set("X-Selftest-Signature", sign(body, secret))
	if s.VerifySignature([]byte(`{"events":[1]}`), h) {
		t.Fatal("thân đã đổi mà chữ ký vẫn đúng")
	}
}

func TestChallenge(t *testing.T) {
	s := selftest.NewSource(secret)
	got, ok := s.Challenge(url.Values{"verify_token": {secret}, "challenge": {"abc"}})
	if !ok || got != "abc" {
		t.Fatalf("Challenge đúng mã = %q, %v", got, ok)
	}
	for _, q := range []url.Values{
		{"verify_token": {"sai"}, "challenge": {"abc"}},
		{"challenge": {"abc"}},
		{"verify_token": {secret}},
	} {
		if got, ok := s.Challenge(q); ok {
			t.Fatalf("Challenge(%v) = %q, true; muốn false", q, got)
		}
	}
}

func TestSplit(t *testing.T) {
	s := selftest.NewSource(secret)
	raw := []byte(`{"events":[
		{"kind":"ping","eventId":"e1","tenantKey":"ka","sentAt":"2026-11-18T01:02:03.004Z"},
		{"kind":"mystery","eventId":"e2","tenantKey":"ka","sentAt":"2026-11-18T01:02:03Z"},
		{"kind":"ping","eventId":"","tenantKey":"ka","sentAt":"2026-11-18T01:02:03Z"},
		{"kind":"ping","eventId":"e3","tenantKey":"kb","sentAt":"2026-11-18T01:02:04Z"}
	]}`)
	res, err := s.Split(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("Events = %d, muốn 2: %+v", len(res.Events), res.Events)
	}
	e := res.Events[0]
	want := time.Date(2026, 11, 18, 1, 2, 3, 4_000_000, time.UTC)
	if e.Type != "selftest.ping" || e.DedupeKey != "e1" || e.Route != (webhooks.Route{Kind: "selftest", ExternalKey: "ka"}) ||
		e.OrderKey != "ka" || !e.OccurredAt.Equal(want) {
		t.Fatalf("sự kiện 1 = %+v", e)
	}
	if !json.Valid(e.Payload) {
		t.Fatalf("Payload không phải JSON: %s", e.Payload)
	}
	if res.Events[1].DedupeKey != "e3" || res.Events[1].Route.ExternalKey != "kb" {
		t.Fatalf("sự kiện 2 = %+v", res.Events[1])
	}
	if res.Skipped["mystery"] != 1 || res.Skipped["invalid"] != 1 {
		t.Fatalf("Skipped = %v", res.Skipped)
	}
	if _, err := s.Split([]byte("không phải json")); err == nil {
		t.Fatal("thân không phải JSON mà Split không lỗi")
	}
}
