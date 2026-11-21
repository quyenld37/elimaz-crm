package vtp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp/vtptest"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// carrierErr kiểm err là *CarrierError lớp want, trả lỗi đó.
func carrierErr(t *testing.T, what string, err error, want fulfillment.ErrorClass) *fulfillment.CarrierError {
	t.Helper()
	ce, ok := errors.AsType[*fulfillment.CarrierError](err)
	if !ok || ce.Class != want {
		t.Fatalf("%s: lỗi = %v, muốn CarrierError lớp %s", what, err, want)
	}
	return ce
}

// wantClass như carrierErr khi không cần đọc lỗi.
func wantClass(t *testing.T, what string, err error, want fulfillment.ErrorClass) {
	t.Helper()
	_ = carrierErr(t, what, err, want)
}

// hnToHCM là đơn mẫu của kế hoạch: Hà Nội → TP. Hồ Chí Minh, 900 g, COD 630.000.
func hnToHCM(acc fulfillment.Account) fulfillment.QuoteInput {
	return fulfillment.QuoteInput{
		From: acc.Pickup,
		To: fulfillment.Address{
			Name: "Trần Thị Mẫu", Phone: carriertest.SamplePhone, Line: sampleToLine, ProvinceCode: "79", WardCode: "26734",
		},
		Parcel:  fulfillment.Parcel{WeightGrams: 900, LengthCm: 20, WidthCm: 15, HeightCm: 8, DeclaredValue: 630000, ItemCount: 2},
		COD:     630000,
		Options: fulfillment.Options{Payer: "shop"},
	}
}

func createReq(acc fulfillment.Account, code, service string) fulfillment.CreateRequest {
	q := hnToHCM(acc)
	return fulfillment.CreateRequest{
		Code: code, CODAmount: q.COD, ReceiverName: q.To.Name, ReceiverPhone: q.To.Phone, Address: q.To.Line,
		From: acc.Pickup, To: q.To, Parcel: q.Parcel, Service: service,
		Options: fulfillment.Options{Payer: "shop", Pickup: true, Note: "Gọi trước khi giao"},
	}
}

// bodyField đọc trường f của thân yêu cầu thứ i của op.
func bodyField(t *testing.T, s *vtptest.Server, op string, i int) map[string]any {
	t.Helper()
	bs := s.Bodies(op)
	if len(bs) <= i {
		t.Fatalf("máy chủ giả chỉ nhận %d yêu cầu %s, muốn thân thứ %d", len(bs), op, i)
	}
	var m map[string]any
	if err := json.Unmarshal(bs[i], &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Đăng nhập sai → CheckConnection trả Health{OK: false} và lỗi lớp auth; đúng → OK.
func TestCheckConnection(t *testing.T) {
	e := newEnv(t)
	h, err := e.ad.CheckConnection(t.Context(), e.acc)
	if err != nil || !h.OK {
		t.Fatalf("CheckConnection đúng khóa = %+v %v", h, err)
	}
	bad := e.acc
	bad.Creds = fulfillment.Credentials{"username": testUser, "password": "sai"}
	h, err = e.ad.CheckConnection(t.Context(), bad)
	if h.OK {
		t.Fatalf("CheckConnection sai mật khẩu: OK = true")
	}
	ce := carrierErr(t, "sai mật khẩu", err, fulfillment.ClassAuth)
	if ce.Code != "vtp_login" || !errors.Is(err, fulfillment.ErrRejected) {
		t.Errorf("sai mật khẩu: mã %q, bọc ErrRejected %v", ce.Code, errors.Is(err, fulfillment.ErrRejected))
	}
	none := e.acc
	none.Creds = fulfillment.Credentials{}
	_, err = e.ad.CheckConnection(t.Context(), none)
	wantClass(t, "thiếu khóa", err, fulfillment.ClassAuth)
	if n := e.srv.Calls(vtptest.OpLogin); n != 2 {
		t.Errorf("Calls(login) = %d, muốn 2 (thiếu khóa không gọi mạng)", n)
	}
}

// Mã truy cập hết hạn giữa hai lần gọi → lấy lại đúng một lần, lần gọi thành công; hãng vẫn từ chối sau khi lấy lại
// → lỗi auth, không lấy lại lần hai.
func TestTokenRefreshOnce(t *testing.T) {
	e := newEnv(t)
	in := hnToHCM(e.acc)
	if _, err := e.ad.Quote(t.Context(), e.acc, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ad.Quote(t.Context(), e.acc, in); err != nil {
		t.Fatal(err)
	}
	if n := e.srv.Calls(vtptest.OpLogin); n != 1 {
		t.Fatalf("hai lần báo phí: Calls(login) = %d, muốn 1 (dùng lại mã)", n)
	}
	e.srv.ExpireTokens()
	qs, err := e.ad.Quote(t.Context(), e.acc, in)
	if err != nil || len(qs) == 0 {
		t.Fatalf("báo phí sau khi mã hết hạn = %+v %v", qs, err)
	}
	if l, q := e.srv.Calls(vtptest.OpLogin), e.srv.Calls(carriertest.OpQuote); l != 2 || q != 4 {
		t.Errorf("mã hết hạn: Calls(login) = %d, Calls(quote) = %d, muốn 2 và 4", l, q)
	}
	e.srv.SetMode(carriertest.OpQuote, vtptest.ModeTokenExpired)
	if _, err := e.ad.Quote(t.Context(), e.acc, in); err != nil {
		t.Fatalf("mã 202 một lần: %v", err)
	}
	if n := e.srv.Calls(vtptest.OpLogin); n != 3 {
		t.Errorf("mã 202 một lần: Calls(login) = %d, muốn 3", n)
	}
	e.srv.SetMode(carriertest.OpQuote, carriertest.ModeAuthRevoked)
	_, err = e.ad.Quote(t.Context(), e.acc, in)
	ce := carrierErr(t, "khóa bị thu hồi", err, fulfillment.ClassAuth)
	if ce.Code != "vtp_token" {
		t.Errorf("khóa bị thu hồi: mã %q, muốn vtp_token", ce.Code)
	}
	if n := e.srv.Calls(vtptest.OpLogin); n != 4 {
		t.Errorf("khóa bị thu hồi: Calls(login) = %d, muốn 4 (lấy lại đúng một lần)", n)
	}
}

// Đổi bộ khóa của tài khoản → đăng nhập lại bằng khóa mới (không dùng mã của khóa cũ).
func TestTokenKeyedByCredentials(t *testing.T) {
	e := newEnv(t)
	if _, err := e.ad.Quote(t.Context(), e.acc, hnToHCM(e.acc)); err != nil {
		t.Fatal(err)
	}
	changed := e.acc
	changed.Creds = fulfillment.Credentials{"username": testUser, "password": "khoa-moi"}
	_, err := e.ad.Quote(t.Context(), changed, hnToHCM(changed))
	wantClass(t, "khóa mới sai", err, fulfillment.ClassAuth)
	if n := e.srv.Calls(vtptest.OpLogin); n != 2 {
		t.Errorf("Calls(login) = %d, muốn 2", n)
	}
}

// Nhiều worker báo phí cùng lúc cùng tài khoản: không đua dữ liệu (-race), đều thành công.
func TestQuoteConcurrent(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			if _, err := e.ad.Quote(t.Context(), e.acc, hnToHCM(e.acc)); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Báo phí đơn mẫu (Hà Nội → TP. Hồ Chí Minh, 900 g, COD 630.000): dịch vụ và Total đúng mẫu getPriceAll; thân gửi
// đúng ID VTP, cân nặng, COD; có dịch vụ → getPrice, Total = MONEY_TOTAL, phần cước vào Breakdown.
func TestQuoteSample(t *testing.T) {
	e := newEnv(t)
	qs, err := e.ad.Quote(t.Context(), e.acc, hnToHCM(e.acc))
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 || qs[0].Service != "PHS" || qs[0].ServiceName != "Nội tỉnh tiết kiệm" || qs[0].Total != 26400 ||
		qs[0].ExpectedDays != 2 {
		t.Fatalf("Quote = %+v, muốn PHS 26400 2 ngày (mẫu tài liệu)", qs)
	}
	b := bodyField(t, e.srv, carriertest.OpQuote, 0)
	want := map[string]any{
		"SENDER_PROVINCE": 1.0, "SENDER_WARD": 49902.0, "RECEIVER_PROVINCE": 2.0, "RECEIVER_WARD": 51284.0,
		"SENDER_DISTRICT": nil, "RECEIVER_DISTRICT": nil, "PRODUCT_TYPE": "HH", "PRODUCT_WEIGHT": 900.0,
		"PRODUCT_PRICE": 630000.0, "MONEY_COLLECTION": 630000.0, "TYPE": 1.0,
		"PRODUCT_LENGTH": 20.0, "PRODUCT_WIDTH": 15.0, "PRODUCT_HEIGHT": 8.0,
	}
	for k, v := range want {
		got, ok := b[k]
		if !ok || got != v {
			t.Errorf("getPriceAll %s = %v (có %v), muốn %v", k, got, ok, v)
		}
	}
	in := hnToHCM(e.acc)
	in.Service = "VHT"
	qs, err = e.ad.Quote(t.Context(), e.acc, in)
	if err != nil {
		t.Fatal(err)
	}
	wantBD := map[string]int64{"main": 13363, "fuel": 0, "cod": 0, "other": 0, "vas": 0, "vat": 1337}
	if len(qs) != 1 || qs[0].Service != "VHT" || qs[0].Total != 14700 || qs[0].ExpectedDays != 2 ||
		fmt.Sprint(qs[0].Breakdown) != fmt.Sprint(wantBD) {
		t.Fatalf("Quote(VHT) = %+v, muốn VHT 14700 (mẫu getPrice)", qs)
	}
	b = bodyField(t, e.srv, carriertest.OpQuote, 1)
	if b["ORDER_SERVICE"] != "VHT" || b["NATIONAL_TYPE"] != 1.0 || b["MONEY_COLLECTION"] != 630000.0 {
		t.Errorf("getPrice thân = %v", b)
	}
}

// Địa chỉ chưa có mã VTP (nơi nhận, nơi gửi, mã không phải số) → address_unmapped trước khi gọi mạng.
func TestQuoteUnmapped(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name string
		set  func(*fulfillment.QuoteInput, *fulfillment.Account)
	}{
		{"phường nhận chưa khớp", func(in *fulfillment.QuoteInput, _ *fulfillment.Account) { in.To.WardCode = "99999" }},
		{"tỉnh nhận rỗng", func(in *fulfillment.QuoteInput, _ *fulfillment.Account) { in.To.ProvinceCode = "" }},
		{"nơi gửi chưa khớp", func(in *fulfillment.QuoteInput, _ *fulfillment.Account) { in.From.WardCode = "00001" }},
		{"không có bảng đổi", func(_ *fulfillment.QuoteInput, a *fulfillment.Account) { a.Locations = nil }},
		{"mã hãng không phải số", func(_ *fulfillment.QuoteInput, a *fulfillment.Account) {
			a.Locations = locMap{"province:01": "1", "ward:00070": "49902", "province:79": "HCM", "ward:26734": "51284"}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			acc := e.acc
			in := hnToHCM(acc)
			c.set(&in, &acc)
			_, err := e.ad.Quote(t.Context(), acc, in)
			ce := carrierErr(t, c.name, err, fulfillment.ClassRejected)
			if ce.Code != "address_unmapped" || !errors.Is(err, fulfillment.ErrAddressUnmapped) {
				t.Errorf("%s: mã %q, bọc ErrAddressUnmapped %v", c.name, ce.Code, errors.Is(err, fulfillment.ErrAddressUnmapped))
			}
			_, err = e.ad.CreateShipment(t.Context(), acc, fulfillment.CreateRequest{
				Code: "E1", From: in.From, To: in.To, Parcel: in.Parcel, Service: "VHT",
			})
			if !errors.Is(err, fulfillment.ErrAddressUnmapped) {
				t.Errorf("%s: CreateShipment lỗi %v, muốn ErrAddressUnmapped", c.name, err)
			}
		})
	}
	if l, q, cr := e.srv.Calls(vtptest.OpLogin), e.srv.Calls(carriertest.OpQuote), e.srv.Calls(carriertest.OpCreate); l+q+cr != 0 {
		t.Errorf("address_unmapped vẫn gọi mạng: login %d, quote %d, create %d", l, q, cr)
	}
}

// Tùy chọn khai false (cho xem/thử hàng, phí khi khách không nhận) → ErrNotSupported, không gọi mạng.
func TestUnsupportedOptions(t *testing.T) {
	e := newEnv(t)
	for _, o := range []fulfillment.Options{{Inspection: "view"}, {Inspection: "try"}, {FeeOnRefuse: 30000}} {
		in := hnToHCM(e.acc)
		in.Options = o
		if _, err := e.ad.Quote(t.Context(), e.acc, in); !errors.Is(err, fulfillment.ErrNotSupported) {
			t.Errorf("Quote(%+v) lỗi %v, muốn ErrNotSupported", o, err)
		}
		r := createReq(e.acc, "E261118OPT001", "VHT")
		r.Options = o
		if _, err := e.ad.CreateShipment(t.Context(), e.acc, r); !errors.Is(err, fulfillment.ErrNotSupported) {
			t.Errorf("CreateShipment(%+v) lỗi %v, muốn ErrNotSupported", o, err)
		}
	}
	in := hnToHCM(e.acc)
	in.Options.Inspection = "none"
	if _, err := e.ad.Quote(t.Context(), e.acc, in); err != nil {
		t.Errorf("Inspection none: %v", err)
	}
	if n := e.srv.Calls(carriertest.OpCreate); n != 0 {
		t.Errorf("Calls(create) = %d, muốn 0", n)
	}
}

// Tạo vận đơn với mã E261118ABC123 → máy chủ nhận đúng mã đó ở ORDER_NUMBER (bật CHECK_UNIQUE); trả mã hãng và phí;
// dịch vụ rỗng → dịch vụ rẻ nhất của getPriceAll.
func TestCreateSendsReference(t *testing.T) {
	e := newEnv(t)
	res, err := e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118ABC123", "VHT"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.TrackingCode, "1587818") || res.Fee != 16500 || !res.FeeKnown {
		t.Errorf("CreateShipment = %+v, muốn mã hãng của máy chủ, phí 16500", res)
	}
	if !e.srv.Has("E261118ABC123") {
		t.Error("máy chủ không có đơn E261118ABC123")
	}
	b := bodyField(t, e.srv, carriertest.OpCreate, 0)
	want := map[string]any{
		"ORDER_NUMBER": "E261118ABC123", "CHECK_UNIQUE": true, "ORDER_SERVICE": "VHT", "ORDER_SERVICE_ADD": "",
		"ORDER_PAYMENT": 3.0, "MONEY_COLLECTION": 630000.0, "PRODUCT_PRICE": 630000.0, "PRODUCT_WEIGHT": 900.0,
		"PRODUCT_QUANTITY": 2.0, "PRODUCT_TYPE": "HH", "SENDER_PROVINCE": 1.0, "SENDER_WARD": 49902.0,
		"SENDER_DISTRICT": nil, "RECEIVER_PROVINCE": 2.0, "RECEIVER_WARD": 51284.0, "RECEIVER_DISTRICT": nil,
		"RECEIVER_FULLNAME": "Trần Thị Mẫu", "RECEIVER_PHONE": carriertest.SamplePhone, "RECEIVER_ADDRESS": sampleToLine,
		"SENDER_FULLNAME": "Kho MAIN", "SENDER_PHONE": carriertest.SamplePhone, "SENDER_ADDRESS": samplePickupLine,
		"ORDER_NOTE": "Gọi trước khi giao", "GROUPADDRESS_ID": 0.0, "CUS_ID": 0.0,
	}
	for k, v := range want {
		if got, ok := b[k]; !ok || got != v {
			t.Errorf("createOrder %s = %v (có %v), muốn %v", k, got, ok, v)
		}
	}
	if _, err := e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118ABC124", "")); err != nil {
		t.Fatal(err)
	}
	if got := bodyField(t, e.srv, carriertest.OpCreate, 1)["ORDER_SERVICE"]; got != "PHS" {
		t.Errorf("dịch vụ rỗng: ORDER_SERVICE = %v, muốn PHS (rẻ nhất của getPriceAll)", got)
	}
}

// ORDER_PAYMENT theo người trả cước và COD (tài liệu: 1 không thu hộ, 2 thu hàng và cước, 3 thu hàng, 4 thu cước).
func TestCreateOrderPayment(t *testing.T) {
	e := newEnv(t)
	for i, c := range []struct {
		payer string
		cod   int64
		want  float64
	}{{"shop", 0, 1}, {"", 0, 1}, {"shop", 630000, 3}, {"receiver", 630000, 2}, {"receiver", 0, 4}} {
		r := createReq(e.acc, fmt.Sprintf("E261118PAY%03d", i), "VHT")
		r.Options.Payer, r.CODAmount = c.payer, c.cod
		if _, err := e.ad.CreateShipment(t.Context(), e.acc, r); err != nil {
			t.Fatal(err)
		}
		if got := bodyField(t, e.srv, carriertest.OpCreate, i)["ORDER_PAYMENT"]; got != c.want {
			t.Errorf("payer %q COD %d: ORDER_PAYMENT = %v, muốn %v", c.payer, c.cod, got, c.want)
		}
	}
}

// Quá giờ sau khi hãng đã tạo → không rõ; VTP không có tra theo mã (LookupByCode ErrNotSupported); gửi lại cùng mã →
// hãng trả 206 (đơn đã tồn tại) → vẫn không rõ, không có đơn thứ hai.
func TestCreateTimeoutThenResend(t *testing.T) {
	e := newEnv(t)
	code := "E261118ABC125"
	e.srv.SetMode(carriertest.OpCreate, carriertest.ModeTimeoutAfterCreate)
	ctx, cancel := contextTimeout(t, 300*time.Millisecond)
	_, err := e.ad.CreateShipment(ctx, e.acc, createReq(e.acc, code, "VHT"))
	cancel()
	wantClass(t, "quá giờ sau khi tạo", err, fulfillment.ClassUnknownOutcome)
	if errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Errorf("quá giờ sau khi tạo bọc %v", err)
	}
	if _, err := e.ad.LookupByCode(t.Context(), e.acc, code); !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("LookupByCode = %v, muốn ErrNotSupported (tài liệu không có tra theo mã)", err)
	}
	e.srv.SetMode(carriertest.OpCreate, carriertest.ModeOK)
	_, err = e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, code, "VHT"))
	ce := carrierErr(t, "gửi lại cùng mã", err, fulfillment.ClassUnknownOutcome)
	if ce.Code != "vtp_206_duplicate" {
		t.Errorf("gửi lại cùng mã: mã %q, muốn vtp_206_duplicate", ce.Code)
	}
	if !e.srv.Has(code) || e.srv.Calls(carriertest.OpCreate) != 2 {
		t.Errorf("máy chủ: có đơn %v, Calls(create) = %d", e.srv.Has(code), e.srv.Calls(carriertest.OpCreate))
	}
}

// Lớp lỗi của tạo vận đơn theo mã HTTP và mã trong thân của VTP.
func TestCreateErrorClasses(t *testing.T) {
	e := newEnv(t)
	for i, c := range []struct {
		mode  string
		class fulfillment.ErrorClass
		code  string
		is    error
	}{
		{carriertest.ModeReject, fulfillment.ClassRejected, "vtp_204", fulfillment.ErrRejected},
		{carriertest.ModeFailBeforeCreate, fulfillment.ClassNotSentRetry, "http_503", fulfillment.ErrNotSent},
		{carriertest.ModeRateLimited, fulfillment.ClassRateLimited, "http_429", fulfillment.ErrNotSent},
		{carriertest.ModeAuthRevoked, fulfillment.ClassAuth, "vtp_token", fulfillment.ErrRejected},
	} {
		e.srv.SetMode(carriertest.OpCreate, c.mode)
		_, err := e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, fmt.Sprintf("E261118ERR%03d", i), "VHT"))
		ce := carrierErr(t, c.mode, err, c.class)
		if ce.Code != c.code || !errors.Is(err, c.is) {
			t.Errorf("%s: mã %q bọc %v, muốn %q bọc %v", c.mode, ce.Code, err, c.code, c.is)
		}
		if c.mode == carriertest.ModeRateLimited && ce.RetryAfter != 7*time.Second {
			t.Errorf("429: RetryAfter = %s, muốn 7s (Retry-After của hãng)", ce.RetryAfter)
		}
	}
	e.srv.SetMode(carriertest.OpCreate, carriertest.ModeOK)
	// Đăng nhập lỗi tạm trong lúc tạo vận đơn: createOrder chưa gửi → chưa gửi (không phải "không rõ").
	e.srv.ExpireTokens()
	e.srv.SetMode(vtptest.OpLogin, carriertest.ModeRateLimited)
	_, err := e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118ERR100", "VHT"))
	wantClass(t, "đăng nhập 429 khi tạo", err, fulfillment.ClassRateLimited)
	if !errors.Is(err, fulfillment.ErrNotSent) {
		t.Errorf("đăng nhập 429 khi tạo: %v phải bọc ErrNotSent", err)
	}
}

// Đường dẫn gốc theo môi trường của tài khoản (không gọi mạng thật: transport ghi lại máy đích rồi trả 503).
func TestBaseURLByEnvironment(t *testing.T) {
	var mu sync.Mutex
	var hosts []string
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		hosts = append(hosts, r.URL.Scheme+"://"+r.URL.Host)
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})
	ad := vtp.New(vtp.Config{HTTPClient: &http.Client{Transport: rt}})
	acc := newEnv(t).acc
	for _, envName := range []string{"sandbox", "production"} {
		acc.Environment = envName
		_, err := ad.CheckConnection(t.Context(), acc)
		wantClass(t, envName, err, fulfillment.ClassNotSentRetry)
	}
	if fmt.Sprint(hosts) != "[https://partnerdev.viettelpost.vn https://partner.viettelpost.vn]" {
		t.Errorf("máy đích = %v", hosts)
	}
	acc.Environment = "staging"
	_, err := ad.CheckConnection(t.Context(), acc)
	ce := carrierErr(t, "môi trường lạ", err, fulfillment.ClassPermanent)
	if ce.Code != "unknown_environment" || len(hosts) != 2 {
		t.Errorf("môi trường lạ: mã %q, số lần gọi %d", ce.Code, len(hosts))
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Địa giới V3: tỉnh (dòng thiếu tên trong mẫu bị bỏ), phường theo tỉnh cha; cấp khác không hỗ trợ.
func TestLocations(t *testing.T) {
	e := newEnv(t)
	ps, err := e.ad.Locations(t.Context(), e.acc, "province", "")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", ps) != "[{Level:province CarrierCode:5 Name:Thành phố Cần Thơ ParentCarrierCode:}]" {
		t.Errorf("tỉnh = %+v", ps)
	}
	ws, err := e.ad.Locations(t.Context(), e.acc, "ward", "1")
	if err != nil {
		t.Fatal(err)
	}
	want := "[{Level:ward CarrierCode:9762 Name:XÃ AN THẠNH ParentCarrierCode:1} {Level:ward CarrierCode:9763 Name:XÃ LONG THÀNH BẮC ParentCarrierCode:1}]"
	if fmt.Sprintf("%+v", ws) != want {
		t.Errorf("phường = %+v", ws)
	}
	_, err = e.ad.Locations(t.Context(), e.acc, "ward", "")
	wantClass(t, "phường không có tỉnh cha", err, fulfillment.ClassRejected)
	if _, err := e.ad.Locations(t.Context(), e.acc, "district", "1"); !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("cấp quận: %v, muốn ErrNotSupported (danh mục V3 chỉ 2 cấp)", err)
	}
	if n := e.srv.Calls(vtptest.OpLocations); n != 2 {
		t.Errorf("Calls(locations) = %d, muốn 2", n)
	}
	e.srv.SetMode(vtptest.OpLocations, carriertest.ModeFailBeforeSend)
	_, err = e.ad.Locations(t.Context(), e.acc, "province", "")
	wantClass(t, "địa giới 503", err, fulfillment.ClassNotSentRetry)
}

// Mọi mã trong testdata/statuses.json (chép từ tài liệu) ra đúng Canonical, MapStatus theo StatusFromCanonical; mã
// mâu thuẫn (104) và mã lạ → unknown, không phải trạng thái kết thúc.
func TestMapCanonical(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "statuses.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Map map[string]fulfillment.Canonical `json:"map"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Map) != 21 {
		t.Fatalf("statuses.json: %d mã, %v", len(f.Map), err)
	}
	ad := vtp.New(vtp.Config{})
	for raw, want := range f.Map {
		got, ok := ad.MapCanonical(raw)
		if got != want || ok != (want != fulfillment.CanonUnknown) {
			t.Errorf("MapCanonical(%s) = %q %v, muốn %q", raw, got, ok, want)
		}
		st, sok := ad.MapStatus(raw)
		wst, wok := fulfillment.StatusFromCanonical(want)
		if st != wst || sok != wok {
			t.Errorf("MapStatus(%s) = %q %v, muốn %q %v", raw, st, sok, wst, wok)
		}
	}
	for _, raw := range []string{"104", "999", "", "501 "} {
		if got, ok := ad.MapCanonical(raw); ok || got != fulfillment.CanonUnknown {
			t.Errorf("MapCanonical(%q) = %q %v, muốn unknown false", raw, got, ok)
		}
		if _, ok := ad.MapStatus(raw); ok {
			t.Errorf("MapStatus(%q) phải false", raw)
		}
	}
}

// Khả năng khai theo tài liệu (docs/carriers/vtp.md).
func TestCapabilities(t *testing.T) {
	got := vtp.New(vtp.Config{}).Capabilities()
	want := fulfillment.Capabilities{Quote: true, Create: true, Locations: true, DeclaredValue: true}
	if got != want {
		t.Errorf("Capabilities = %+v, muốn %+v", got, want)
	}
}

// Log của cả các luồng (thành công, lỗi) và chuỗi lỗi không chứa SĐT, địa chỉ, tên mẫu.
func TestNoPersonalDataInLogsOrErrors(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) }) // SetDefault cũng đổi đích của gói log chuẩn

	e := newEnv(t)
	var errs []error
	keep := func(_ any, err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	keep(e.ad.Quote(t.Context(), e.acc, hnToHCM(e.acc)))
	keep(e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118LOG001", "")))
	keep(e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118LOG001", "")))
	for _, m := range []string{carriertest.ModeReject, carriertest.ModeAuthRevoked, carriertest.ModeRateLimited, carriertest.ModeFailBeforeCreate} {
		e.srv.SetMode(carriertest.OpCreate, m)
		keep(e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, "E261118LOG"+m, "VHT")))
	}
	bad := e.acc
	bad.Creds = fulfillment.Credentials{"username": testUser, "password": "sai"}
	keep(e.ad.CheckConnection(t.Context(), bad))
	if len(errs) < 6 {
		t.Fatalf("chỉ có %d lỗi, muốn ≥ 6", len(errs))
	}
	all := buf.String()
	for _, err := range errs {
		all += "\n" + err.Error()
	}
	for _, s := range []string{carriertest.SamplePhone, sampleToLine, samplePickupLine, "Trần Thị Mẫu", testPass, "Lỗi dữ liệu"} {
		if strings.Contains(all, s) {
			t.Errorf("log hay lỗi chứa %q", s)
		}
	}
}

// Hãng báo hạn mã truy cập (expired, mili giây): còn hạn thì dùng lại; tới sát hạn (trước 1 phút) thì đăng nhập lại.
func TestTokenExpiryFromLogin(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	var login map[string]any
	if err := json.Unmarshal(sample(t, "login.json"), &login); err != nil {
		t.Fatal(err)
	}
	login["data"].(map[string]any)["expired"] = now.Add(time.Hour).UnixMilli()
	lb, err := json.Marshal(login)
	if err != nil {
		t.Fatal(err)
	}
	srv := vtptest.New(vtptest.Options{
		Username: testUser, Password: testPass, Login: lb, Provinces: sample(t, "provinces_v3.json"),
		Wards: sample(t, "wards_v3.json"), PriceAll: sample(t, "price_all.json"), Price: sample(t, "price.json"),
		CreateOrder: sample(t, "create_order.json"),
	})
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	var mu sync.Mutex
	clock := now
	ad := vtp.New(vtp.Config{BaseURL: hs.URL, HTTPClient: hs.Client(), Now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}})
	acc := newEnv(t).acc
	for i, step := range []struct {
		at     time.Duration
		logins int
	}{{0, 1}, {58 * time.Minute, 1}, {59 * time.Minute, 2}} {
		mu.Lock()
		clock = now.Add(step.at)
		mu.Unlock()
		if _, err := ad.Locations(t.Context(), acc, "province", ""); err != nil {
			t.Fatal(err)
		}
		if n := srv.Calls(vtptest.OpLogin); n != step.logins {
			t.Errorf("bước %d (+%s): Calls(login) = %d, muốn %d", i, step.at, n, step.logins)
		}
	}
}

// Trả lời không dùng được sau khi đã gửi: tạo vận đơn → không rõ (in_doubt); báo phí → chưa gửi (gửi lại được).
func TestAfterSendErrors(t *testing.T) {
	for _, c := range []struct {
		name, body string
		status     int
		create     fulfillment.ErrorClass
		quote      fulfillment.ErrorClass
	}{
		{"HTTP 500", "", http.StatusInternalServerError, fulfillment.ClassUnknownOutcome, fulfillment.ClassNotSentRetry},
		{"HTTP 502", "", http.StatusBadGateway, fulfillment.ClassUnknownOutcome, fulfillment.ClassNotSentRetry},
		{
			"thân 205", `{"status":205,"error":true,"message":"System error","data":null}`, http.StatusOK,
			fulfillment.ClassUnknownOutcome, fulfillment.ClassNotSentRetry,
		},
		{"thân hỏng", `{"status":`, http.StatusOK, fulfillment.ClassUnknownOutcome, fulfillment.ClassNotSentRetry},
		{"thân không phải phong bì", `{}`, http.StatusOK, fulfillment.ClassUnknownOutcome, fulfillment.ClassNotSentRetry},
		{
			"thân 203", `{"status":203,"error":true,"message":"x","data":null}`, http.StatusOK,
			fulfillment.ClassRejected, fulfillment.ClassRejected,
		},
		{"HTTP 400", "", http.StatusBadRequest, fulfillment.ClassRejected, fulfillment.ClassRejected},
	} {
		t.Run(c.name, func(t *testing.T) {
			rt := roundTrip(func(r *http.Request) (*http.Response, error) {
				body, status := c.body, c.status
				if r.URL.Path == "/v2/user/Login" {
					body, status = `{"status":200,"error":false,"message":"OK","data":{"token":"t","expired":0}}`, http.StatusOK
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})
			ad := vtp.New(vtp.Config{BaseURL: "http://vtp.invalid", HTTPClient: &http.Client{Transport: rt}})
			acc := newEnv(t).acc
			_, err := ad.CreateShipment(t.Context(), acc, createReq(acc, "E261118AFT001", "VHT"))
			wantClass(t, "tạo vận đơn", err, c.create)
			if c.create == fulfillment.ClassUnknownOutcome && (errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected)) {
				t.Errorf("tạo vận đơn không rõ mà bọc %v", err)
			}
			_, err = ad.Quote(t.Context(), acc, hnToHCM(acc))
			wantClass(t, "báo phí", err, c.quote)
		})
	}
}
