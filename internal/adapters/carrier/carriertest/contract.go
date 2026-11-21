// Package carriertest là bộ contract test chung của mọi adapter hãng vận chuyển (fulfillment.CarrierAdapter): mỗi
// khả năng khai true trong Capabilities phải qua ca của nó, khai false phải trả fulfillment.ErrNotSupported; đổi mã
// trạng thái theo testdata/<hãng>/statuses.json; lớp lỗi 401, 429, chưa gửi, không rõ; tách và kiểm chữ ký webhook
// theo testdata/<hãng>/webhook.json; không kết quả hay lỗi nào chứa SĐT.
//
// Khi nào không dùng: không thay test riêng của adapter (đọc đúng định dạng hãng, mã lỗi riêng); chỉ gọi từ test.
package carriertest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Harness là phần điều khiển máy chủ giả của hãng (tools/fakecarrier/server cài): xóa trạng thái, đặt chế độ lỗi theo
// thao tác, đẩy sự kiện hành trình, hỏi kiện theo mã CRM, đếm số yêu cầu theo thao tác.
type Harness interface {
	Reset()
	SetMode(op, mode string)
	Push(tracking string, ev fulfillment.CarrierEvent)
	Has(shipmentCode string) bool
	Calls(op string) int
}

// Factory là adapter cần kiểm, máy chủ giả nó nói chuyện (nil khi mọi khả năng gọi hãng khai false) và tài khoản dùng.
type Factory struct {
	Adapter fulfillment.CarrierAdapter
	Server  Harness
	Account fulfillment.Account
}

// callTimeout là hạn một lời gọi adapter trong contract; timeoutCall ngắn hơn cho ca quá giờ.
const (
	callTimeout = 10 * time.Second
	timeoutCall = 300 * time.Millisecond
)

// unknownRaw là mã thô không hãng nào dùng.
const unknownRaw = "__carriertest_ma_la__"

type suite struct {
	f    Factory
	caps fulfillment.Capabilities
	raw  map[string]fulfillment.Canonical
	seq  int
}

// Run chạy bộ contract trên f; mỗi phần là một subtest đặt tên theo khả năng.
func Run(t *testing.T, f Factory) {
	t.Helper()
	if f.Adapter == nil {
		t.Fatal("carriertest: Factory.Adapter nil")
	}
	s := &suite{f: f, caps: f.Adapter.Capabilities()}
	t.Run("Key", func(t *testing.T) {
		if f.Adapter.Key() == "" {
			t.Fatal("Key rỗng")
		}
	})
	t.Run("Capabilities", s.consistent)
	t.Run("MapCanonical", s.mapCanonical)
	t.Run("Create", s.create)
	t.Run("LookupByRef", s.lookup)
	t.Run("Quote", s.quote)
	for _, o := range []struct {
		name string
		on   bool
		set  func(*fulfillment.QuoteInput)
	}{
		{"Inspection", s.caps.Inspection, func(in *fulfillment.QuoteInput) { in.Options.Inspection = "try" }},
		{"DeclaredValue", s.caps.DeclaredValue, func(in *fulfillment.QuoteInput) { in.Parcel.DeclaredValue = 2_000_000 }},
		{"FeeOnRefuse", s.caps.FeeOnRefuse, func(in *fulfillment.QuoteInput) { in.Options.FeeOnRefuse = 30000 }},
	} {
		t.Run(o.name, func(t *testing.T) { s.quoteOption(t, o.name, o.on, o.set) })
	}
	t.Run("Cancel", s.cancel)
	t.Run("Track", s.track)
	t.Run("Webhook", s.webhook)
	t.Run("Labels", s.labels)
	t.Run("UpdateAddress", s.updateAddress)
	t.Run("Redeliver", s.redeliver)
	t.Run("Locations", s.locations)
	t.Run("Statement", s.statement)
}

// consistent: khả năng phụ thuộc phải đi cùng khả năng gốc.
func (s *suite) consistent(t *testing.T) {
	c := s.caps
	if c.CancelIdempotent && !c.Cancel {
		t.Error("khả năng CancelIdempotent khai true mà Cancel false")
	}
	if c.LabelsBatch && !c.Labels {
		t.Error("khả năng LabelsBatch khai true mà Labels false")
	}
	if (c.Inspection || c.DeclaredValue || c.FeeOnRefuse) && !c.Quote && !c.Create {
		t.Error("khả năng tùy chọn giao khai true mà không báo phí, không tạo kiện")
	}
	if (c.Cancel || c.Track || c.Labels || c.UpdateAddress || c.Redeliver) && !c.Create {
		t.Error("khả năng thao tác trên vận đơn khai true mà Create false")
	}
}

func (s *suite) mapCanonical(t *testing.T) {
	a := s.f.Adapter
	m, err := statuses(a.Key())
	if err != nil {
		t.Fatal(err)
	}
	s.raw = m
	for raw, want := range m {
		got, ok := a.MapCanonical(raw)
		if !ok || got != want || got == fulfillment.CanonUnknown {
			t.Errorf("MapCanonical(%q) = %q %v, muốn %q true", raw, got, ok, want)
		}
		st, sok := a.MapStatus(raw)
		wst, wok := fulfillment.StatusFromCanonical(want)
		if st != wst || sok != wok {
			t.Errorf("MapStatus(%q) = %q %v, muốn %q %v (theo StatusFromCanonical)", raw, st, sok, wst, wok)
		}
	}
	if got, ok := a.MapCanonical(unknownRaw); ok || got != fulfillment.CanonUnknown {
		t.Errorf("MapCanonical(mã lạ) = %q %v, muốn unknown false", got, ok)
	}
	if _, ok := a.MapStatus(unknownRaw); ok {
		t.Error("MapStatus(mã lạ) phải false")
	}
}

// rawFor trả mã thô đầu tiên (theo thứ tự chữ) của trạng thái chuẩn c trong statuses.json.
func (s *suite) rawFor(c fulfillment.Canonical) (string, bool) {
	m := s.raw
	if m == nil {
		var err error
		if m, err = statuses(s.f.Adapter.Key()); err != nil {
			return "", false
		}
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == c {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", false
	}
	slices.Sort(keys)
	return keys[0], true
}

// reset xóa trạng thái máy chủ giả; dừng subtest khi khả năng khai true mà không có máy chủ giả.
func (s *suite) reset(t *testing.T, capName string) {
	t.Helper()
	if s.f.Server == nil {
		t.Fatalf("khả năng %s khai true nhưng Factory.Server nil (cần máy chủ giả để kiểm)", capName)
	}
	s.f.Server.Reset()
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), callTimeout)
	t.Cleanup(cancel)
	return c
}

// code trả mã vận đơn mới, duy nhất trong lần chạy.
func (s *suite) code() string {
	s.seq++
	return fmt.Sprintf("CT%s%04d", strings.ToUpper(s.f.Adapter.Key()), s.seq)
}

// mustCreate tạo một kiện ở chế độ ok, trả mã hãng.
func (s *suite) mustCreate(t *testing.T) (code, tracking string) {
	t.Helper()
	code = s.code()
	res, err := s.f.Adapter.CreateShipment(ctx(t), s.f.Account, sampleCreate(s.f.Account, code))
	noPhone(t, "CreateShipment", res, err)
	if err != nil || res.TrackingCode == "" {
		t.Fatalf("CreateShipment(%s) = %+v %v", code, res, err)
	}
	return code, res.TrackingCode
}

// unsupported kiểm err là ErrNotSupported khi khả năng khai false.
func unsupported(t *testing.T, capName, call string, err error) {
	t.Helper()
	if !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("khả năng %s khai false nhưng %s trả %v, muốn ErrNotSupported", capName, call, err)
	}
}

// supported kiểm err nil khi khả năng khai true.
func supported(t *testing.T, capName, call string, err error) bool {
	t.Helper()
	if err != nil {
		if errors.Is(err, fulfillment.ErrNotSupported) {
			t.Errorf("khả năng %s khai true nhưng %s trả ErrNotSupported", capName, call)
		} else {
			t.Errorf("khả năng %s khai true nhưng %s lỗi: %v", capName, call, err)
		}
		return false
	}
	return true
}

// class kiểm err là *CarrierError lớp want; trả RetryAfter của lỗi (ok = false khi sai lớp).
func class(t *testing.T, what string, err error, want fulfillment.ErrorClass) (time.Duration, bool) {
	t.Helper()
	ce, ok := errors.AsType[*fulfillment.CarrierError](err)
	if !ok || ce.Class != want {
		t.Errorf("%s: lỗi = %v, muốn CarrierError lớp %s", what, err, want)
		return 0, false
	}
	return ce.RetryAfter, true
}

// rateLimited kiểm err là CarrierError lớp rate_limited có RetryAfter > 0.
func rateLimited(t *testing.T, what string, err error) {
	t.Helper()
	if ra, ok := class(t, what, err, fulfillment.ClassRateLimited); ok && ra <= 0 {
		t.Errorf("%s: RetryAfter = %s, muốn > 0", what, ra)
	}
}

// noPhone: kết quả và lỗi không được chứa chuỗi SĐT.
func noPhone(t *testing.T, call string, v any, err error) {
	t.Helper()
	if s := fmt.Sprintf("%+v", v); phoneRe.MatchString(s) {
		t.Errorf("%s trả kết quả có SĐT: %s", call, phoneRe.FindString(s))
	}
	if err != nil && phoneRe.MatchString(err.Error()) {
		t.Errorf("%s trả lỗi có SĐT", call)
	}
}

func (s *suite) create(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Create {
		_, err := a.CreateShipment(ctx(t), acc, sampleCreate(acc, s.code()))
		unsupported(t, "Create", "CreateShipment", err)
		_, err = a.GetStatus(ctx(t), acc, "X1")
		unsupported(t, "Create", "GetStatus", err)
		return
	}
	s.reset(t, "Create")
	t.Run("ok", s.createOK)
	t.Run("timeout_after_create rồi ok cùng mã", s.createAgain)
	for _, c := range []struct {
		mode  string
		class fulfillment.ErrorClass
		is    error
	}{
		{ModeFailBeforeCreate, fulfillment.ClassNotSentRetry, fulfillment.ErrNotSent},
		{ModeReject, fulfillment.ClassRejected, fulfillment.ErrRejected},
		{ModeAuthRevoked, fulfillment.ClassAuth, nil},
		{ModeRateLimited, fulfillment.ClassRateLimited, fulfillment.ErrNotSent},
	} {
		t.Run(c.mode, func(t *testing.T) { s.createFails(t, c.mode, c.class, c.is) })
	}
}

func (s *suite) createOK(t *testing.T) {
	a, acc, h := s.f.Adapter, s.f.Account, s.f.Server
	code, tracking := s.mustCreate(t)
	if !h.Has(code) {
		t.Errorf("máy chủ không có kiện %s", code)
	}
	st, err := a.GetStatus(ctx(t), acc, tracking)
	noPhone(t, "GetStatus", st, err)
	if !s.caps.Track { // hãng không có API đọc trạng thái (trạng thái chỉ về qua webhook): GetStatus như Track
		unsupported(t, "Track", "GetStatus", err)
		return
	}
	if err != nil || st.RawStatus == "" || st.EventKey == "" {
		t.Fatalf("GetStatus = %+v %v", st, err)
	}
	if _, ok := a.MapStatus(st.RawStatus); !ok {
		t.Errorf("trạng thái sau tạo %q không đổi được", st.RawStatus)
	}
}

// createAgain: quá giờ sau khi hãng đã tạo → lỗi không rõ; gửi lại cùng mã → cùng kiện, tra theo mã thấy.
func (s *suite) createAgain(t *testing.T) {
	a, acc, h := s.f.Adapter, s.f.Account, s.f.Server
	h.Reset()
	code := s.code()
	h.SetMode(OpCreate, ModeTimeoutAfterCreate)
	c, cancel := context.WithTimeout(t.Context(), timeoutCall)
	_, err := a.CreateShipment(c, acc, sampleCreate(acc, code))
	cancel()
	h.SetMode(OpCreate, ModeOK)
	noPhone(t, "CreateShipment", nil, err)
	if err == nil || errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("quá giờ sau khi tạo: lỗi = %v, muốn lỗi không rõ", err)
	}
	class(t, "quá giờ sau khi tạo", err, fulfillment.ClassUnknownOutcome)
	if !s.caps.LookupByRef {
		s.createAgainNoLookup(t, code)
		return
	}
	res, err := a.CreateShipment(ctx(t), acc, sampleCreate(acc, code))
	if err != nil || res.TrackingCode == "" {
		t.Fatalf("tạo lại cùng mã = %+v %v", res, err)
	}
	lk, err := a.LookupByCode(ctx(t), acc, code)
	if err != nil || !lk.Found || lk.TrackingCode != res.TrackingCode {
		t.Fatalf("LookupByCode sau tạo lại = %+v %v, muốn mã %s", lk, err, res.TrackingCode)
	}
	if !h.Has(code) {
		t.Errorf("máy chủ không có kiện %s", code)
	}
	if n := h.Calls(OpCreate); n != 2 {
		t.Errorf("Calls(create) = %d, muốn 2 (quá giờ rồi gửi lại một lần, không tự gửi thêm)", n)
	}
	again, err := a.CreateShipment(ctx(t), acc, sampleCreate(acc, code))
	if err != nil || again.TrackingCode != res.TrackingCode {
		t.Errorf("tạo lần ba cùng mã = %+v %v, muốn cùng kiện %s (không tạo kiện thứ hai)", again, err, res.TrackingCode)
	}
}

// createAgainNoLookup: hãng không tra được theo mã (LookupByRef false) — gửi lại cùng mã sau khi quá giờ không được
// tạo kiện thứ hai và không được báo "chưa gửi" hay "từ chối" (vận đơn chờ người nhập mã hãng): lỗi lớp không rõ,
// máy chủ có đúng kiện đó, đúng 2 lần gọi tạo.
func (s *suite) createAgainNoLookup(t *testing.T, code string) {
	a, acc, h := s.f.Adapter, s.f.Account, s.f.Server
	res, err := a.CreateShipment(ctx(t), acc, sampleCreate(acc, code))
	noPhone(t, "CreateShipment", res, err)
	if err == nil || errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("gửi lại cùng mã khi hãng không tra được = %+v %v, muốn lỗi không rõ", res, err)
	}
	class(t, "gửi lại cùng mã khi hãng không tra được", err, fulfillment.ClassUnknownOutcome)
	if !h.Has(code) {
		t.Errorf("máy chủ không có kiện %s", code)
	}
	if n := h.Calls(OpCreate); n != 2 {
		t.Errorf("Calls(create) = %d, muốn 2 (quá giờ rồi gửi lại một lần, không tự gửi thêm)", n)
	}
}

// createFails: chế độ lỗi của tạo kiện ra đúng lớp lỗi, bọc đúng lỗi M1; chưa gửi thì máy chủ không có kiện.
func (s *suite) createFails(t *testing.T, mode string, want fulfillment.ErrorClass, is error) {
	a, acc, h := s.f.Adapter, s.f.Account, s.f.Server
	h.Reset()
	h.SetMode(OpCreate, mode)
	defer h.SetMode(OpCreate, ModeOK)
	code := s.code()
	_, err := a.CreateShipment(ctx(t), acc, sampleCreate(acc, code))
	noPhone(t, "CreateShipment", nil, err)
	if mode == ModeRateLimited {
		rateLimited(t, mode, err)
	} else {
		class(t, mode, err, want)
	}
	if is != nil && !errors.Is(err, is) {
		t.Errorf("%s: lỗi %v phải bọc %v", mode, err, is)
	}
	if h.Has(code) {
		t.Errorf("%s: máy chủ không được tạo kiện", mode)
	}
}

func (s *suite) lookup(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.LookupByRef {
		_, err := a.LookupByCode(ctx(t), acc, s.code())
		unsupported(t, "LookupByRef", "LookupByCode", err)
		return
	}
	s.reset(t, "LookupByRef")
	lk, err := a.LookupByCode(ctx(t), acc, s.code())
	if !supported(t, "LookupByRef", "LookupByCode", err) || lk.Found || lk.Inconclusive {
		t.Errorf("LookupByCode(mã chưa tạo) = %+v, muốn chắc chắn không có", lk)
	}
	if !s.caps.Create {
		return
	}
	code, tracking := s.mustCreate(t)
	lk, err = a.LookupByCode(ctx(t), acc, code)
	noPhone(t, "LookupByCode", lk, err)
	if err != nil || !lk.Found || lk.TrackingCode != tracking {
		t.Errorf("LookupByCode(%s) = %+v %v, muốn %s", code, lk, err, tracking)
	}
}

func (s *suite) quote(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Quote {
		_, err := a.Quote(ctx(t), acc, sampleQuote(acc))
		unsupported(t, "Quote", "Quote", err)
		return
	}
	s.reset(t, "Quote")
	h := s.f.Server
	qs, err := a.Quote(ctx(t), acc, sampleQuote(acc))
	noPhone(t, "Quote", qs, err)
	if supported(t, "Quote", "Quote", err) && len(qs) == 0 {
		t.Error("Quote trả rỗng")
	}
	for _, q := range qs {
		if q.Service == "" || q.Total <= 0 {
			t.Errorf("báo phí sai: %+v", q)
		}
	}
	h.SetMode(OpQuote, ModeRateLimited)
	_, err = a.Quote(ctx(t), acc, sampleQuote(acc))
	rateLimited(t, "Quote 429", err)
	h.SetMode(OpQuote, ModeAuthRevoked)
	_, err = a.Quote(ctx(t), acc, sampleQuote(acc))
	class(t, "Quote 401", err, fulfillment.ClassAuth)
	h.SetMode(OpQuote, ModeOK)
}

// quoteOption: tùy chọn giao khai false → Quote có tùy chọn đó trả ErrNotSupported; khai true → Quote nhận.
func (s *suite) quoteOption(t *testing.T, name string, on bool, set func(*fulfillment.QuoteInput)) {
	if !s.caps.Quote { // không báo phí: Quote đã kiểm ErrNotSupported ở ca Quote; tùy chọn chỉ còn đi qua CreateShipment
		return
	}
	s.reset(t, name)
	in := sampleQuote(s.f.Account)
	set(&in)
	qs, err := s.f.Adapter.Quote(ctx(t), s.f.Account, in)
	noPhone(t, "Quote", qs, err)
	if on {
		supported(t, name, "Quote", err)
		return
	}
	unsupported(t, name, "Quote", err)
}

func (s *suite) cancel(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Cancel {
		_, err := a.Cancel(ctx(t), acc, "X1")
		unsupported(t, "Cancel", "Cancel", err)
		return
	}
	s.reset(t, "Cancel")
	h := s.f.Server
	_, tracking := s.mustCreate(t)
	res, err := a.Cancel(ctx(t), acc, tracking)
	noPhone(t, "Cancel", res, err)
	if supported(t, "Cancel", "Cancel", err) && res != fulfillment.CancelDone && res != fulfillment.CancelPending {
		t.Errorf("Cancel kiện chưa lấy = %q, muốn done hoặc pending", res)
	}
	if s.caps.CancelIdempotent {
		res, err = a.Cancel(ctx(t), acc, tracking)
		if err != nil || (res != fulfillment.CancelDone && res != fulfillment.CancelPending) {
			t.Errorf("khả năng CancelIdempotent: hủy lần hai = %q %v", res, err)
		}
	}
	if raw, ok := s.rawFor(fulfillment.CanonPickedUp); ok {
		_, picked := s.mustCreate(t)
		h.Push(picked, sampleEvent("ct-picked", raw))
		res, err = a.Cancel(ctx(t), acc, picked)
		if err != nil || res != fulfillment.CancelRejectedPickedUp {
			t.Errorf("Cancel kiện đã lấy = %q %v, muốn rejected_picked_up", res, err)
		}
	}
	_, other := s.mustCreate(t)
	h.SetMode(OpCancel, ModeRateLimited)
	_, err = a.Cancel(ctx(t), acc, other)
	rateLimited(t, "Cancel 429", err)
	h.SetMode(OpCancel, ModeOK)
}

func (s *suite) track(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Track {
		_, err := a.Track(ctx(t), acc, "X1", "")
		unsupported(t, "Track", "Track", err)
		return
	}
	s.reset(t, "Track")
	h := s.f.Server
	code, tracking := s.mustCreate(t)
	raw, ok := s.rawFor(fulfillment.CanonInTransit)
	if !ok {
		t.Fatal("khả năng Track: statuses.json không có mã nào ra in_transit để đẩy")
	}
	h.Push(tracking, sampleEvent("ct-transit", raw))
	tr, err := a.Track(ctx(t), acc, tracking, code)
	noPhone(t, "Track", tr, err)
	if !supported(t, "Track", "Track", err) {
		return
	}
	found := false
	for _, e := range tr.Events {
		if e.RawStatus == raw {
			found = e.Canonical == fulfillment.CanonInTransit && !e.OccurredAt.IsZero() && e.Key != ""
		}
	}
	if tr.TrackingCode != tracking || !found {
		t.Errorf("Track = %+v, muốn có sự kiện %s → in_transit", tr, raw)
	}
	h.SetMode(OpTrack, ModeAuthRevoked)
	_, err = a.Track(ctx(t), acc, tracking, code)
	class(t, "Track 401", err, fulfillment.ClassAuth)
	h.SetMode(OpTrack, ModeOK)
}

func (s *suite) webhook(t *testing.T) {
	a := s.f.Adapter
	if !s.caps.Webhook {
		if a.VerifyWebhook([]byte(`{}`), fulfillment.Header{}, "bi-mat-bat-ky") {
			t.Error("khả năng Webhook khai false nhưng VerifyWebhook trả true")
		}
		_, err := a.SplitWebhook([]byte(`{}`))
		unsupported(t, "Webhook", "SplitWebhook", err)
		return
	}
	w, err := webhook(a.Key())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(w.Body)
	if !a.VerifyWebhook(body, w.Headers, w.Secret) {
		t.Error("VerifyWebhook(thân mẫu, chữ ký đúng) = false")
	}
	if a.VerifyWebhook(body, w.Headers, w.Secret+"x") {
		t.Error("VerifyWebhook sai bí mật = true")
	}
	if w.TokenOnly {
		other := fulfillment.Header{}
		for k, vs := range w.Headers {
			for _, v := range vs {
				other[k] = append(other[k], v+"x")
			}
		}
		if a.VerifyWebhook(body, other, w.Secret) {
			t.Error("VerifyWebhook token trong header khác bí mật = true")
		}
	} else if a.VerifyWebhook(append(slices.Clone(body), ' '), w.Headers, w.Secret) {
		t.Error("VerifyWebhook thân bị sửa = true")
	}
	if a.VerifyWebhook(body, fulfillment.Header{}, w.Secret) || a.VerifyWebhook(body, w.Headers, "") {
		t.Error("VerifyWebhook thiếu chữ ký hay bí mật rỗng = true")
	}
	sig, err := a.SplitWebhook(body)
	noPhone(t, "SplitWebhook", sig, err)
	if err != nil || !slices.Equal(sig, w.Signals) {
		t.Errorf("SplitWebhook = %+v %v, muốn %+v", sig, err, w.Signals)
	}
	if _, err := a.SplitWebhook([]byte(`không phải json`)); err == nil {
		t.Error("SplitWebhook thân hỏng không lỗi")
	}
}

func (s *suite) labels(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Labels {
		_, err := a.Labels(ctx(t), acc, []string{"X1"})
		unsupported(t, "Labels", "Labels", err)
		return
	}
	s.reset(t, "Labels")
	_, t1 := s.mustCreate(t)
	_, t2 := s.mustCreate(t)
	l, err := a.Labels(ctx(t), acc, []string{t1})
	if supported(t, "Labels", "Labels", err) && (!strings.HasPrefix(string(l.PDF), "%PDF") || l.Pages < 1) {
		t.Errorf("nhãn một kiện: %d trang, đầu %q", l.Pages, string(l.PDF[:min(len(l.PDF), 8)]))
	}
	l, err = a.Labels(ctx(t), acc, []string{t1, t2})
	if !s.caps.LabelsBatch {
		unsupported(t, "LabelsBatch", "Labels(hai mã)", err)
		return
	}
	if supported(t, "LabelsBatch", "Labels(hai mã)", err) && l.Pages < 2 {
		t.Errorf("nhãn hai kiện: %d trang", l.Pages)
	}
}

func (s *suite) updateAddress(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.UpdateAddress {
		_, err := a.UpdateAddress(ctx(t), acc, "X1", sampleTo())
		unsupported(t, "UpdateAddress", "UpdateAddress", err)
		return
	}
	s.reset(t, "UpdateAddress")
	_, tracking := s.mustCreate(t)
	to := sampleTo()
	to.Line = "99 Trần Hưng Đạo"
	res, err := a.UpdateAddress(ctx(t), acc, tracking, to)
	noPhone(t, "UpdateAddress", res, err)
	if supported(t, "UpdateAddress", "UpdateAddress", err) && !validUpdate(res) {
		t.Errorf("UpdateAddress = %q", res)
	}
}

func (s *suite) redeliver(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Redeliver {
		_, err := a.Redeliver(ctx(t), acc, "X1", "giao lại")
		unsupported(t, "Redeliver", "Redeliver", err)
		return
	}
	s.reset(t, "Redeliver")
	h := s.f.Server
	_, tracking := s.mustCreate(t)
	if raw, ok := s.rawFor(fulfillment.CanonDeliveryFailed); ok {
		h.Push(tracking, sampleEvent("ct-failed", raw))
	}
	res, err := a.Redeliver(ctx(t), acc, tracking, "giao lại buổi chiều")
	noPhone(t, "Redeliver", res, err)
	if supported(t, "Redeliver", "Redeliver", err) && !validUpdate(res) {
		t.Errorf("Redeliver = %q", res)
	}
}

func validUpdate(r fulfillment.UpdateResult) bool {
	return r == fulfillment.UpdateDone || r == fulfillment.UpdatePending || r == fulfillment.UpdateRejected
}

func (s *suite) locations(t *testing.T) {
	a, acc := s.f.Adapter, s.f.Account
	if !s.caps.Locations {
		_, err := a.Locations(ctx(t), acc, "province", "")
		unsupported(t, "Locations", "Locations", err)
		return
	}
	s.reset(t, "Locations")
	ls, err := a.Locations(ctx(t), acc, "province", "")
	if supported(t, "Locations", "Locations", err) && len(ls) == 0 {
		t.Error("Locations(province) rỗng")
	}
	for _, l := range ls {
		if l.CarrierCode == "" || l.Name == "" || l.Level != "province" {
			t.Errorf("địa giới sai: %+v", l)
		}
	}
}

func (s *suite) statement(t *testing.T) {
	a := s.f.Adapter
	p, ok := a.(fulfillment.StatementFormatProvider)
	var fs []fulfillment.StatementFormat
	if ok {
		fs = p.StatementFormats()
	}
	if s.caps.Statement != (len(fs) > 0) {
		t.Errorf("khả năng Statement khai %v nhưng adapter có %d định dạng bảng kê", s.caps.Statement, len(fs))
	}
	for _, f := range fs {
		if f.Key == "" || f.CarrierKey != a.Key() || len(f.Required) == 0 {
			t.Errorf("định dạng bảng kê sai: %+v", f)
		}
	}
}
