package cfaccess_test

import (
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

const aud = "aud-crm"

// clock là đồng hồ giả, an toàn khi đọc từ nhiều goroutine.
type clock struct{ ns atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.ns.Store(time.Now().UnixNano())
	return c
}

func (c *clock) Now() time.Time      { return time.Unix(0, c.ns.Load()) }
func (c *clock) Add(d time.Duration) { c.ns.Add(int64(d)) }
func (c *clock) opts(k *testkit.AccessKeys) cfaccess.Options {
	return cfaccess.Options{
		TeamDomain: k.TeamDomain, AUD: aud, MFAClaim: testkit.AccessMFAClaim, MFAValues: []string{"mfa"},
		HTTPClient: k.Client, Now: c.Now,
	}
}

func newVerifier(t *testing.T, k *testkit.AccessKeys, c *clock) *cfaccess.Verifier {
	t.Helper()
	v, err := cfaccess.New(c.opts(k))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// primed: máy chủ khóa, đồng hồ giả và bộ kiểm thẻ đã kiểm một thẻ hợp lệ (đã tải khóa một lần).
func primed(t *testing.T) (*testkit.AccessKeys, *clock, *cfaccess.Verifier) {
	t.Helper()
	k := testkit.AccessKeyServer(t)
	c := newClock()
	v := newVerifier(t, k, c)
	if _, err := v.Verify(t.Context(), k.Sign(good("a@e.test"))); err != nil {
		t.Fatal(err)
	}
	return k, c, v
}

// noneToken dựng thẻ không chữ ký (alg none) lúc chạy, để tệp không chứa chuỗi giống JWT thật (gitleaks).
func noneToken() string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + enc([]byte(`{"email":"a@e.test","aud":"`+aud+`"}`)) + "."
}

func good(email string) testkit.AccessClaims {
	return testkit.AccessClaims{Email: email, AUD: aud, MFA: []string{"pwd", "mfa"}}
}

func TestVerify(t *testing.T) {
	k := testkit.AccessKeyServer(t)
	v := newVerifier(t, k, newClock())
	email, err := v.Verify(t.Context(), k.Sign(good("lan@elimaz.test")))
	if err != nil || email != "lan@elimaz.test" {
		t.Fatalf("thẻ hợp lệ: %q, %v", email, err)
	}

	bad := map[string]string{
		"rỗng":           "",
		"không phải JWT": "abc.def",
		"sai aud":        k.Sign(testkit.AccessClaims{Email: "a@e.test", AUD: "khac", MFA: []string{"mfa"}}),
		"sai iss":        k.Sign(testkit.AccessClaims{Email: "a@e.test", AUD: aud, Iss: "https://khac.cloudflareaccess.com", MFA: []string{"mfa"}}),
		"hết hạn":        k.Sign(testkit.AccessClaims{Email: "a@e.test", AUD: aud, Exp: -time.Minute, MFA: []string{"mfa"}}),
		"khóa khác":      k.SignForeign(good("a@e.test")),
		"thiếu email":    k.Sign(testkit.AccessClaims{AUD: aud, MFA: []string{"mfa"}}),
		"thiếu exp":      k.Sign(testkit.AccessClaims{Email: "a@e.test", AUD: aud, MFA: []string{"mfa"}, Extra: map[string]any{"exp": nil}}),
		"alg none":       noneToken(),
	}
	for name, tok := range bad {
		if _, err := v.Verify(t.Context(), tok); !errors.Is(err, identity.ErrInvalidToken) {
			t.Errorf("%s: lỗi %v, mong ErrInvalidToken", name, err)
		}
	}

	mfa := map[string]testkit.AccessClaims{
		"thiếu trường MFA":   {Email: "a@e.test", AUD: aud},
		"MFA không cho phép": {Email: "a@e.test", AUD: aud, MFA: []string{"pwd"}},
		"MFA kiểu lạ":        {Email: "a@e.test", AUD: aud, Extra: map[string]any{testkit.AccessMFAClaim: true}},
	}
	for name, c := range mfa {
		if _, err := v.Verify(t.Context(), k.Sign(c)); !errors.Is(err, identity.ErrMFARequired) {
			t.Errorf("%s: lỗi %v, mong ErrMFARequired", name, err)
		}
	}
	// MFA là chuỗi đơn cũng nhận.
	if _, err := v.Verify(t.Context(), k.Sign(testkit.AccessClaims{Email: "a@e.test", AUD: aud, Extra: map[string]any{testkit.AccessMFAClaim: "mfa"}})); err != nil {
		t.Fatalf("MFA chuỗi đơn: %v", err)
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	base := cfaccess.Options{TeamDomain: "x.cloudflareaccess.com", AUD: aud, MFAClaim: "amr", MFAValues: []string{"mfa"}}
	for name, mut := range map[string]func(*cfaccess.Options){
		"thiếu TeamDomain": func(o *cfaccess.Options) { o.TeamDomain = "" },
		"thiếu AUD":        func(o *cfaccess.Options) { o.AUD = "" },
		"thiếu MFAClaim":   func(o *cfaccess.Options) { o.MFAClaim = "" },
		"MFAValues rỗng":   func(o *cfaccess.Options) { o.MFAValues = nil },
		"TeamDomain có /":  func(o *cfaccess.Options) { o.TeamDomain = "x.com/evil" },
	} {
		o := base
		mut(&o)
		if _, err := cfaccess.New(o); err == nil {
			t.Errorf("%s: New phải lỗi", name)
		}
	}
	if _, err := cfaccess.New(base); err != nil {
		t.Fatalf("tùy chọn đủ: %v", err)
	}
}

// Máy chủ khóa đổi kid: thẻ ký bằng kid mới được nhận sau đúng một lần tải lại.
func TestKeyRotation(t *testing.T) {
	k, c, v := primed(t)
	if k.Fetches() != 1 {
		t.Fatalf("lần kiểm đầu tải khóa 1 lần, có %d", k.Fetches())
	}
	c.Add(31 * time.Second)
	k.Rotate()
	if _, err := v.Verify(t.Context(), k.Sign(good("a@e.test"))); err != nil {
		t.Fatalf("kid mới: %v", err)
	}
	if k.Fetches() != 2 {
		t.Fatalf("kid mới phải gây đúng một lần tải lại, có %d lần tổng", k.Fetches())
	}
}

// 100 thẻ kid lạ trong 1 s (song song) chỉ gây một lần tải; trong 30 s sau lần tải, kid lạ không tải nữa.
func TestUnknownKidThrottled(t *testing.T) {
	k, c, v := primed(t)
	c.Add(31 * time.Second)
	before := k.Fetches()
	var wg sync.WaitGroup
	var rejected atomic.Int64
	for i := range 100 {
		wg.Go(func() {
			tok := k.SignWithKid(good("a@e.test"), "kid-la-"+string(rune('a'+i%26)))
			if _, err := v.Verify(t.Context(), tok); errors.Is(err, identity.ErrInvalidToken) {
				rejected.Add(1)
			}
		})
	}
	wg.Wait()
	if got := k.Fetches() - before; got != 1 {
		t.Fatalf("100 kid lạ phải gây đúng 1 lần tải, có %d", got)
	}
	if rejected.Load() != 100 {
		t.Fatalf("thẻ kid lạ phải bị từ chối hết, bị từ chối %d", rejected.Load())
	}
	// Khóa cũ vẫn dùng được, không tải thêm.
	if _, err := v.Verify(t.Context(), k.Sign(good("a@e.test"))); err != nil || k.Fetches()-before != 1 {
		t.Fatalf("khóa đã có: %v, số lần tải %d", err, k.Fetches()-before)
	}
}

// Khóa cũ hơn 10 phút được làm mới ở lần kiểm sau; máy chủ khóa lỗi thì giữ khóa cũ.
func TestPeriodicRefreshKeepsOldKeysOnFailure(t *testing.T) {
	k := testkit.AccessKeyServer(t)
	c := newClock()
	v := newVerifier(t, k, c)
	claims := good("a@e.test")
	claims.Exp = time.Hour // đồng hồ giả chạy trước giờ thật tới 30 phút
	tok := k.Sign(claims)
	if _, err := v.Verify(t.Context(), tok); err != nil {
		t.Fatal(err)
	}
	c.Add(5 * time.Minute)
	if _, err := v.Verify(t.Context(), tok); err != nil || k.Fetches() != 1 {
		t.Fatalf("trong 10 phút không tải lại: %v, %d", err, k.Fetches())
	}
	c.Add(6 * time.Minute)
	if _, err := v.Verify(t.Context(), tok); err != nil || k.Fetches() != 2 {
		t.Fatalf("quá 10 phút phải tải lại đúng một lần: %v, %d", err, k.Fetches())
	}
	// Máy chủ khóa trả 500: lần làm mới lỗi, khóa cũ vẫn kiểm được thẻ.
	k.SetFailing(true)
	c.Add(11 * time.Minute)
	if _, err := v.Verify(t.Context(), tok); err != nil || k.Fetches() != 3 {
		t.Fatalf("làm mới lỗi phải giữ khóa cũ: %v, %d lần tải", err, k.Fetches())
	}
	// Trong 30 s sau lần lỗi không thử lại.
	if _, err := v.Verify(t.Context(), tok); err != nil || k.Fetches() != 3 {
		t.Fatalf("không dội máy chủ khóa đang lỗi: %v, %d lần tải", err, k.Fetches())
	}
}

// Chưa từng tải được khóa (máy chủ khóa lỗi) → thẻ không hợp lệ, không lỗi lạ.
func TestNoKeysYet(t *testing.T) {
	k := testkit.AccessKeyServer(t)
	k.SetFailing(true)
	v := newVerifier(t, k, newClock())
	if _, err := v.Verify(t.Context(), k.Sign(good("a@e.test"))); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("mong ErrInvalidToken, có %v", err)
	}
}
