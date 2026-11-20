package fulfillment_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// quoteEnv: shipEnv cộng các tài khoản hãng báo phí (mỗi hãng giả một tài khoản của người bán ELIMAZ ở kho MAIN),
// Service với đồng hồ cố định, báo hiệu ghi lại, worker chỉ chạy lệnh báo phí (hạn gọi hãng 1 s).
type quoteEnv struct {
	*shipEnv
	svc      *fulfillment.Service
	cmd      *fulfillment.QuoteCommand
	bus      *testkit.RecordingBus
	now      time.Time
	accounts map[string]uuid.UUID
}

func newQuoteEnv(t *testing.T, carriers ...*quoteCarrier) *quoteEnv {
	t.Helper()
	e := &quoteEnv{
		shipEnv: newShipEnv(t), bus: &testkit.RecordingBus{}, accounts: map[string]uuid.UUID{},
		now: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC),
	}
	adapters := make([]fulfillment.CarrierAdapter, 0, len(carriers))
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, c := range carriers {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: c.key, Name: "Hãng " + c.key,
				ExternalAccountRef: c.key + "-1",
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			e.accounts[c.key] = id
			adapters = append(adapters, c)
		}
		return nil
	})
	e.svc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: e.bus,
		Carriers: mustRegistry(t, adapters...), Now: func() time.Time { return e.now },
	}).Service
	e.cmd = fulfillment.NewQuoteCommandTimeout(e.svc, time.Second)
	e.useCommands([]commands.Handler{e.cmd})
	return e
}

func (e *quoteEnv) request(o order.Order, in fulfillment.QuoteRequest) (fulfillment.QuoteAccepted, error) {
	var out fulfillment.QuoteAccepted
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = e.svc.RequestQuotes(e.t.Context(), tx, o, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *quoteEnv) mustRequest(o order.Order, in fulfillment.QuoteRequest) fulfillment.QuoteAccepted {
	e.t.Helper()
	acc, err := e.request(o, in)
	if err != nil {
		e.t.Fatalf("RequestQuotes: %v", err)
	}
	return acc
}

func (e *quoteEnv) list(o order.Order, req *uuid.UUID) (uuid.UUID, []fulfillment.QuoteRow) {
	e.t.Helper()
	var id uuid.UUID
	var rows []fulfillment.QuoteRow
	if err := e.app(func(tx pgx.Tx) (err error) {
		id, rows, err = e.svc.ListQuotes(e.t.Context(), tx, o.ID, req)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return id, rows
}

// statusOf trả trạng thái (và mã lỗi) của dòng tài khoản key trong rows (dòng đầu tiên gặp).
func statusOf(rows []fulfillment.QuoteRow, account uuid.UUID) (string, string) {
	for _, r := range rows {
		if r.CarrierAccountID == account {
			return r.Status, r.ErrorCode
		}
	}
	return "", ""
}

// VC02: ba tài khoản (A 28.000 STD + 41.000 EXP, B 31.500, C treo) → 202 ba tài khoản; sau worker A hai dòng ok, B một,
// C error timeout; danh sách 28.000, 31.500, 41.000 rồi C; không phí 0; ok có valid_until = now + 30 phút và
// input_hash = InputHash(đầu vào, dịch vụ); mỗi lần ghi một báo hiệu shipping_quote.ready ở kho của đơn.
func TestQuotes_VC02(t *testing.T) {
	a, b, c := vc02Carriers()
	e := newQuoteEnv(t, a, b, c)
	o, _ := e.order(2)
	if _, rows := e.list(o, nil); len(rows) != 0 {
		t.Fatalf("đơn chưa so phí có %d dòng", len(rows))
	}
	acc := e.mustRequest(o, fulfillment.QuoteRequest{})
	if len(acc.Accounts) != 3 || acc.RequestID == uuid.Nil {
		t.Fatalf("accepted = %+v, muốn 3 tài khoản", acc)
	}
	req, rows := e.list(o, nil)
	if req != acc.RequestID || len(rows) != 3 {
		t.Fatalf("trước worker: lần %s, %d dòng; muốn %s, 3 pending", req, len(rows), acc.RequestID)
	}
	for _, r := range rows {
		if r.Status != "pending" || r.FeeTotal != nil {
			t.Fatalf("trước worker: dòng %+v", r)
		}
	}
	e.drain()
	_, rows = e.list(o, &acc.RequestID)
	e.checkVC02Rows(rows)
	e.checkInputHash(o, rows[0])
	e.checkReady(o, 3)
	// Chạy lại worker sau khi đã có kết quả: không đổi dòng nào, không báo hiệu thêm.
	e.drain()
	if _, again := e.list(o, &acc.RequestID); len(again) != 4 {
		t.Fatalf("chạy lại: %d dòng", len(again))
	}
	e.checkReady(o, 3)
}

// checkVC02Rows: 28.000 (A STD), 31.500 (B), 41.000 (A EXP) ok có phí, valid_until = now + 30 phút; rồi C error
// timeout không phí.
func (e *quoteEnv) checkVC02Rows(rows []fulfillment.QuoteRow) {
	e.t.Helper()
	want := []struct {
		account, status, code string
		fee                   int64
	}{{"qa", "ok", "", 28000}, {"qb", "ok", "", 31500}, {"qa", "ok", "", 41000}, {"qc", "error", "timeout", 0}}
	if len(rows) != len(want) {
		e.t.Fatalf("sau worker %d dòng: %+v", len(rows), rows)
	}
	until := e.now.Add(30 * time.Minute)
	for i, w := range want {
		r := rows[i]
		if r.CarrierAccountID != e.accounts[w.account] || r.Status != w.status || r.ErrorCode != w.code {
			e.t.Errorf("dòng %d = %s %s %q, muốn %s %s %q", i, r.CarrierKey, r.Status, r.ErrorCode, w.account, w.status, w.code)
		}
		ok := w.status == "ok"
		if got := feeOf(r); got != w.fee || (ok && (r.ValidUntil == nil || !r.ValidUntil.Equal(until))) {
			e.t.Errorf("dòng %d phí %d hạn %v, muốn %d hạn %v (ok)", i, got, r.ValidUntil, w.fee, until)
		}
	}
	if rows[0].Service != "STD" || rows[0].ServiceName != "Tiêu chuẩn" || rows[0].ExpectedDays == nil || *rows[0].ExpectedDays != 3 {
		e.t.Errorf("dòng rẻ nhất = %+v", rows[0])
	}
}

// feeOf: phí của dòng (0 khi không có: dòng lỗi không bao giờ có phí, dòng ok không bao giờ phí 0).
func feeOf(r fulfillment.QuoteRow) int64 {
	if r.FeeTotal == nil {
		return 0
	}
	return *r.FeeTotal
}

// checkInputHash: input_hash của dòng r = InputHash(địa chỉ đơn, kiện mặc định 300 g × số món, COD, dịch vụ, tùy
// chọn rỗng).
func (e *quoteEnv) checkInputHash(o order.Order, r fulfillment.QuoteRow) {
	e.t.Helper()
	got := readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) ([]byte, error) {
		var h []byte
		err := tx.QueryRow(ctx, "SELECT input_hash FROM fulfillment.shipping_quotes WHERE id = $1", r.QuoteID).Scan(&h)
		return h, err //nolint:wrapcheck // test
	})
	sa := o.ShipAddress
	to := fulfillment.Address{Name: sa.Name, Phone: sa.Phone, Line: string(sa.Line), ProvinceCode: sa.ProvinceCode, WardCode: sa.WardCode}
	parcel := fulfillment.QuoteParcel(o, 0, 0, fulfillment.DefaultSettings())
	if parcel.WeightGrams != 600 || parcel.ItemCount != 2 {
		e.t.Fatalf("kiện mặc định = %+v, muốn 600 g, 2 món", parcel)
	}
	if !bytes.Equal(got, fulfillment.InputHash(to, parcel, o.CODAmount, r.Service, fulfillment.Options{})) {
		e.t.Errorf("input_hash của dòng %s không khớp InputHash(đầu vào, dịch vụ)", r.Service)
	}
}

// checkReady: đúng n báo hiệu shipping_quote.ready, mỗi cái đòi fulfillment.quote ở kho của đơn, entity là đơn.
func (e *quoteEnv) checkReady(o order.Order, n int) {
	e.t.Helper()
	var ready int
	for _, s := range e.bus.Signals() {
		if s.Topic != "shipping_quote.ready" {
			continue
		}
		ready++
		if s.Requires != "fulfillment.quote" || s.Scope == nil || s.Scope.ID != o.WarehouseID.String() ||
			s.Entity.Type != "order" || s.Entity.ID != o.ID.String() || s.TenantID != e.tenant {
			e.t.Errorf("báo hiệu %+v, muốn fulfillment.quote ở kho %s, entity đơn %s", s, o.WarehouseID, o.ID)
		}
	}
	if ready != n {
		e.t.Errorf("báo hiệu shipping_quote.ready = %d, muốn %d (mỗi tài khoản một lần ghi)", ready, n)
	}
}

// Tài khoản B tạm dừng giữa lúc yêu cầu và lúc chạy → dòng B unavailable, không gọi hãng B.
func TestQuotes_AccountPausedMidway(t *testing.T) {
	a, b, _ := vc02Carriers()
	e := newQuoteEnv(t, a, b)
	o, _ := e.order(1)
	acc := e.mustRequest(o, fulfillment.QuoteRequest{})
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE id = $1", e.accounts["qb"])
		return err //nolint:wrapcheck // test
	})
	e.drain()
	_, rows := e.list(o, &acc.RequestID)
	if st, code := statusOf(rows, e.accounts["qb"]); st != "unavailable" || code != "" {
		t.Fatalf("dòng B = %s %q, muốn unavailable", st, code)
	}
	if b.calls.Load() != 0 {
		t.Fatalf("gọi hãng B %d lần, muốn 0", b.calls.Load())
	}
	if st, _ := statusOf(rows, e.accounts["qa"]); st != "ok" {
		t.Fatalf("dòng A = %s, muốn ok", st)
	}
}

// Hãng có Locations mà địa chỉ đơn chưa khớp carrier_locations → error address_unmapped, không gọi hãng.
func TestQuotes_AddressUnmapped(t *testing.T) {
	d := &quoteCarrier{key: "qd", locations: true, results: []fulfillment.QuoteResult{{Service: "S", Total: 20000}}}
	e := newQuoteEnv(t, d)
	o, _ := e.order(1)
	acc := e.mustRequest(o, fulfillment.QuoteRequest{})
	e.drain()
	_, rows := e.list(o, &acc.RequestID)
	if len(rows) != 1 || rows[0].Status != "error" || rows[0].ErrorCode != fulfillment.QuoteErrAddressUnmapped || rows[0].FeeTotal != nil {
		t.Fatalf("dòng = %+v, muốn error address_unmapped", rows)
	}
	if d.calls.Load() != 0 {
		t.Fatalf("gọi hãng %d lần, muốn 0", d.calls.Load())
	}
}

// Hãng từ chối khóa → error auth và tài khoản sang auth_failed; 429 Retry-After quá 20 s → error rate_limited ngay;
// 429 Retry-After 5 s → lệnh chờ (dòng còn pending).
func TestQuotes_AuthAndRateLimit(t *testing.T) {
	auth := &quoteCarrier{key: "qauth", err: &fulfillment.CarrierError{Class: fulfillment.ClassAuth, Code: "bad_token"}}
	slow := &quoteCarrier{key: "qslow", err: &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, RetryAfter: time.Minute}}
	wait := &quoteCarrier{key: "qwait", err: &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, RetryAfter: 5 * time.Second}}
	e := newQuoteEnv(t, auth, slow, wait)
	o, _ := e.order(1)
	acc := e.mustRequest(o, fulfillment.QuoteRequest{})
	e.drain()
	_, rows := e.list(o, &acc.RequestID)
	for key, want := range map[string][2]string{
		"qauth": {"error", "auth"}, "qslow": {"error", "rate_limited"}, "qwait": {"pending", ""},
	} {
		if st, code := statusOf(rows, e.accounts[key]); st != want[0] || code != want[1] {
			t.Errorf("dòng %s = %s %q, muốn %s %q", key, st, code, want[0], want[1])
		}
	}
	st := readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (string, error) {
		var s string
		err := tx.QueryRow(ctx, "SELECT status FROM fulfillment.carrier_accounts WHERE id = $1", e.accounts["qauth"]).Scan(&s)
		return s, err //nolint:wrapcheck // test
	})
	if st != "auth_failed" {
		t.Errorf("tài khoản bị từ chối khóa = %s, muốn auth_failed", st)
	}
}

// Lệnh dead hay expired khi dòng còn pending → error timeout; in_doubt không đổi gì (lệnh chạy lại).
func TestQuotes_OnTerminal(t *testing.T) {
	a, _, _ := vc02Carriers()
	e := newQuoteEnv(t, a)
	o, _ := e.order(1)
	acc := e.mustRequest(o, fulfillment.QuoteRequest{})
	j := readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (queue.Job, error) {
		var jb queue.Job
		err := tx.QueryRow(ctx, "SELECT id, payload FROM platform.jobs WHERE kind = $1", fulfillment.QuoteKindName).
			Scan(&jb.ID, &jb.Payload)
		return jb, err //nolint:wrapcheck // test
	})
	j.TenantID = &e.tenant
	for _, status := range []string{queue.StatusInDoubt, queue.StatusExpired} {
		if err := e.app(func(tx pgx.Tx) error { return e.cmd.OnTerminal(t.Context(), j, status, tx) }); err != nil {
			t.Fatalf("OnTerminal %s: %v", status, err)
		}
		_, rows := e.list(o, &acc.RequestID)
		want := map[string]string{queue.StatusInDoubt: "pending", queue.StatusExpired: "error"}[status]
		if rows[0].Status != want {
			t.Fatalf("sau OnTerminal %s: %s, muốn %s", status, rows[0].Status, want)
		}
	}
	_, rows := e.list(o, &acc.RequestID)
	if rows[0].ErrorCode != fulfillment.QuoteErrTimeout {
		t.Fatalf("mã = %q, muốn timeout", rows[0].ErrorCode)
	}
	// Kết quả về sau khi đã ghi timeout: dòng không còn pending → không đổi.
	e.drain()
	if _, rows = e.list(o, &acc.RequestID); len(rows) != 1 || rows[0].Status != "error" {
		t.Fatalf("sau worker: %+v", rows)
	}
}

// Từ chối: đơn đã có vận đơn → ErrShipmentExists; tài khoản của người bán khác → ErrAccountNotFound; tài khoản không
// báo phí được (hãng không có adapter) → ErrAccountUnavailable; không lỗi nào ghi dòng hay lệnh.
func TestQuotes_RequestRefusals(t *testing.T) {
	a, _, _ := vc02Carriers()
	e := newQuoteEnv(t, a)
	o, _ := e.order(1)
	for name, c := range map[string]struct {
		ids  []uuid.UUID
		want error
	}{
		"người bán khác": {[]uuid.UUID{e.shop.PartnerAccount}, fulfillment.ErrAccountNotFound},
		"không có":       {[]uuid.UUID{uuid.New()}, fulfillment.ErrAccountNotFound},
		"không báo phí":  {[]uuid.UUID{e.accounts["qa"], e.shop.Account}, fulfillment.ErrAccountUnavailable},
	} {
		if _, err := e.request(o, fulfillment.QuoteRequest{AccountIDs: c.ids}); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", name, err, c.want)
		}
	}
	// Chọn đúng một tài khoản (trùng hai lần) → một dòng.
	acc := e.mustRequest(o, fulfillment.QuoteRequest{AccountIDs: []uuid.UUID{e.accounts["qa"], e.accounts["qa"]}})
	if len(acc.Accounts) != 1 {
		t.Fatalf("chọn một tài khoản: %+v", acc.Accounts)
	}
	e.mustCreateShipment(o.ID)
	if _, err := e.request(e.getOrder(o.ID), fulfillment.QuoteRequest{}); !errors.Is(err, fulfillment.ErrShipmentExists) {
		t.Fatalf("đơn đã có vận đơn: lỗi %v, muốn ErrShipmentExists", err)
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM fulfillment.shipping_quotes"); n != 1 {
		t.Fatalf("dòng báo phí = %d, muốn 1", n)
	}
}

// Dọn: báo phí tạo trước now − 7 ngày bị xóa (crm_app có quyền DELETE, 0320), mới hơn giữ.
func TestQuotes_Cleanup(t *testing.T) {
	a, _, _ := vc02Carriers()
	e := newQuoteEnv(t, a)
	o, _ := e.order(1)
	e.mustRequest(o, fulfillment.QuoteRequest{})
	e.drain()
	clean := func(now time.Time) int64 {
		return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (int64, error) {
			return fulfillment.CleanupQuotes(ctx, tx, now)
		})
	}
	if n := clean(time.Now().Add(6 * 24 * time.Hour)); n != 0 {
		t.Fatalf("dọn sau 6 ngày xóa %d, muốn 0", n)
	}
	if n := clean(time.Now().Add(8 * 24 * time.Hour)); n != 2 {
		t.Fatalf("dọn sau 8 ngày xóa %d, muốn 2", n)
	}
}
