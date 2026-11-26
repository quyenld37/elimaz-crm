package fulfillment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// webEnv: shipEnv cộng Service có hai hãng giả chỉ báo phí "vtp" (30.000) và "best" (28.000) và ba tài khoản active
// của người bán ELIMAZ ở kho MAIN: vtp (hiện trên web, web_sort 2), best (hiện, web_sort 1), ghn (không hiện; khóa
// hãng không có adapter như tài khoản manual trên production).
type webEnv struct {
	*shipEnv
	svc             *fulfillment.Service
	vtp, best       *quoteCarrier
	accVTP, accBest uuid.UUID
	accGHN          uuid.UUID
}

func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	e := &webEnv{shipEnv: newShipEnv(t)}
	e.vtp = &quoteCarrier{key: "vtp", results: []fulfillment.QuoteResult{{Service: "STD", ServiceName: "Chuẩn", Total: 30000}}}
	e.best = &quoteCarrier{key: "best", results: []fulfillment.QuoteResult{{Service: "STD", ServiceName: "Chuẩn", Total: 28000}}}
	e.svc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
		Carriers: mustRegistry(t, e.vtp, e.best),
	}).Service
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.accVTP = shiptest.ManualAccount(t, tx, e.seller, e.shop.Warehouse, "vtp", "vtp-1")
		e.accBest = shiptest.ManualAccount(t, tx, e.seller, e.shop.Warehouse, "best", "best-1")
		e.accGHN = shiptest.ManualAccount(t, tx, e.seller, e.shop.Warehouse, "ghn", "ghn-1")
		return nil
	})
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET show_on_web = true, web_sort = 2 WHERE id = $1", e.accVTP)
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET show_on_web = true, web_sort = 1 WHERE id = $1", e.accBest)
	return e
}

func (e *webEnv) settings() fulfillment.WebCarrierSettings {
	e.t.Helper()
	return readAs(e.shipEnv, fulfillment.GetWebCarrierSettings)
}

func (e *webEnv) save(in fulfillment.WebCarrierSettings) (fulfillment.WebCarrierSettingsChange, error) {
	var ch fulfillment.WebCarrierSettingsChange
	err := e.app(func(tx pgx.Tx) (err error) {
		ch, err = e.svc.SaveWebCarrierSettings(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *webEnv) mustSave(in fulfillment.WebCarrierSettings) fulfillment.WebCarrierSettingsChange {
	e.t.Helper()
	ch, err := e.save(in)
	if err != nil {
		e.t.Fatalf("lưu cài đặt hãng trên web: %v", err)
	}
	return ch
}

func (e *webEnv) carriers() []fulfillment.WebCarrier {
	e.t.Helper()
	return readAs(e.shipEnv, e.svc.WebCarriers)
}

func (e *webEnv) quote(in fulfillment.WebShipQuoteInput) ([]fulfillment.ShipOption, error) {
	return e.svc.QuoteForWeb(e.t.Context(), e.staff.App, e.tenant, in) //nolint:wrapcheck // test
}

// hide tắt show_on_web của tài khoản id qua UpdateAccount của M3 (version đọc ngay trước).
func (e *webEnv) hide(id uuid.UUID) error {
	off := false
	return e.app(func(tx pgx.Tx) error {
		v, err := e.svc.GetAccount(e.t.Context(), tx, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = e.svc.UpdateAccount(e.t.Context(), tx, id, fulfillment.AccountPatch{ShowOnWeb: &off}, v.Version, e.actor)
		return err //nolint:wrapcheck // test
	})
}

func webTo() fulfillment.WebShipQuoteInput {
	return fulfillment.WebShipQuoteInput{ProvinceCode: "01", WardCode: "00004", WeightGrams: 500, Value: 300000, COD: 300000}
}

// Ba tài khoản active, hai cái show_on_web (web_sort 2, 1) → đúng hai tài khoản theo web_sort; chưa lưu cài đặt →
// version 1, không ưu tiên, không phí dự phòng; tắt show_on_web một cái qua UpdateAccount → không còn trong danh sách.
func TestWebCarriers_ShowOnWebOrder(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	if s := e.settings(); s.Version != 1 || s.PreferredAccountID != nil || s.FallbackFee != nil {
		t.Fatalf("cài đặt chưa lưu = %+v, muốn version 1, không ưu tiên, không phí dự phòng", s)
	}
	got := e.carriers()
	if len(got) != 2 || got[0].AccountID != e.accBest || got[1].AccountID != e.accVTP {
		t.Fatalf("hãng trên web = %+v, muốn best rồi vtp", got)
	}
	if got[0].CarrierKey != "best" || got[0].Name != "Tay best-1" || got[0].Preferred || got[1].Preferred {
		t.Fatalf("hãng trên web = %+v: sai khóa, tên hay ưu tiên", got)
	}
	if err := e.hide(e.accBest); err != nil {
		t.Fatal(err)
	}
	if got := e.carriers(); len(got) != 1 || got[0].AccountID != e.accVTP {
		t.Fatalf("sau khi tắt best: %+v, muốn chỉ vtp", got)
	}
}

// Lưu ưu tiên vtp → vtp đứng đầu, preferred; sự kiện carrier.web_settings_changed (Seq = version mới) cùng tx; ưu tiên
// không hiện trên web, đang tạm dừng hay không có → ErrCarrierNotOnWeb; version cũ → ErrVersionConflict; phí dự phòng
// ngoài 0–500.000 → ErrSettingOutOfRange. Lỗi không đổi gì.
func TestSaveWebCarrierSettings(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	ch := e.mustSave(fulfillment.WebCarrierSettings{PreferredAccountID: &e.accVTP, FallbackFee: new(int64(30000)), Version: 1})
	if ch.Before.Version != 1 || ch.After.Version != 2 || *ch.After.PreferredAccountID != e.accVTP || *ch.After.FallbackFee != 30000 {
		t.Fatalf("lưu: %+v", ch)
	}
	if got := e.carriers(); len(got) != 2 || got[0].AccountID != e.accVTP || !got[0].Preferred || got[1].Preferred {
		t.Fatalf("sau khi ưu tiên vtp: %+v, muốn vtp đứng đầu, preferred", got)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = $1 AND seq = 2`,
		fulfillment.TopicWebCarrierSettingsChanged); n != 1 {
		t.Fatalf("sự kiện %s = %d, muốn 1", fulfillment.TopicWebCarrierSettingsChanged, n)
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'paused', show_on_web = true WHERE id = $1", e.accGHN)
	for _, c := range []struct {
		name string
		in   fulfillment.WebCarrierSettings
		want error
	}{
		{"không hiện trên web", fulfillment.WebCarrierSettings{PreferredAccountID: new(e.shop.Account), Version: 2}, fulfillment.ErrCarrierNotOnWeb},
		{"đang tạm dừng", fulfillment.WebCarrierSettings{PreferredAccountID: &e.accGHN, Version: 2}, fulfillment.ErrCarrierNotOnWeb},
		{"không có", fulfillment.WebCarrierSettings{PreferredAccountID: new(uuid.New()), Version: 2}, fulfillment.ErrCarrierNotOnWeb},
		{"version cũ", fulfillment.WebCarrierSettings{Version: 1}, fulfillment.ErrVersionConflict},
		{"phí âm", fulfillment.WebCarrierSettings{FallbackFee: new(int64(-1)), Version: 2}, fulfillment.ErrSettingOutOfRange},
		{"phí quá trần", fulfillment.WebCarrierSettings{FallbackFee: new(fulfillment.MaxWebFallbackFee + 1), Version: 2}, fulfillment.ErrSettingOutOfRange},
	} {
		if _, err := e.save(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", c.name, err, c.want)
		}
	}
	if s := e.settings(); s.Version != 2 || *s.PreferredAccountID != e.accVTP {
		t.Fatalf("lỗi đã đổi cài đặt: %+v", s)
	}
	// Bỏ ưu tiên và phí dự phòng (gửi nil) được.
	if ch := e.mustSave(fulfillment.WebCarrierSettings{Version: 2}); ch.After.PreferredAccountID != nil || ch.After.FallbackFee != nil {
		t.Fatalf("bỏ ưu tiên: %+v", ch.After)
	}
}

// Báo phí: vtp 30.000, best 28.000 → hai lựa chọn rẻ trước khi không ưu tiên; ưu tiên vtp → vtp đứng đầu dù đắt hơn;
// vtp treo 5 s → trả best trong dưới 3,5 s.
func TestQuoteForWeb_PreferredAndTimeout(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	opts, err := e.quote(webTo())
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 || opts[0].AccountID != e.accBest || opts[0].Fee != 28000 || opts[1].Fee != 30000 {
		t.Fatalf("không ưu tiên: %+v, muốn best 28.000 rồi vtp 30.000", opts)
	}
	if opts[0].CarrierKey != "best" || opts[0].CarrierName != "Tay best-1" || opts[0].Service != "STD" || opts[0].Estimated {
		t.Fatalf("lựa chọn best = %+v", opts[0])
	}
	e.mustSave(fulfillment.WebCarrierSettings{PreferredAccountID: &e.accVTP, Version: 1})
	opts, err = e.quote(webTo())
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 || opts[0].AccountID != e.accVTP || !opts[0].Preferred || opts[1].Preferred {
		t.Fatalf("ưu tiên vtp: %+v, muốn vtp đứng đầu", opts)
	}
	e.vtp.hang = 5 * time.Second
	opts, err = e.quote(webTo())
	// Hạn mỗi hãng cho web 3 s: kiểm bằng hạn ctx hãng nhận và việc lần treo bị cắt (không đo giờ thật, Task 496).
	if dl, at, cut := e.vtp.lastCall(); dl.IsZero() || dl.Sub(at) > 3*time.Second || !cut {
		t.Fatalf("vtp treo: hạn còn %s lúc gọi, bị ctx cắt %v; muốn hạn ≤ 3 s, bị cắt", dl.Sub(at), cut)
	}
	if err != nil || len(opts) != 1 || opts[0].AccountID != e.accBest {
		t.Fatalf("vtp treo: %+v, %v; muốn chỉ best", opts, err)
	}
}

// Mọi hãng lỗi (hãng không có adapter như tài khoản manual khi tích hợp hãng tạm dừng; hãng có adapter trả lỗi):
// không phí dự phòng → ErrNoQuote; phí dự phòng 30.000 → một lựa chọn Estimated. Không hãng nào hiện trên web cũng vậy.
func TestQuoteForWeb_Fallback(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	e.vtp.err = &fulfillment.CarrierError{Class: fulfillment.ClassRejected}
	e.best.results = nil
	if _, err := e.quote(webTo()); !errors.Is(err, fulfillment.ErrNoQuote) {
		t.Fatalf("mọi hãng lỗi, không dự phòng: %v, muốn ErrNoQuote", err)
	}
	e.mustSave(fulfillment.WebCarrierSettings{FallbackFee: new(int64(30000)), Version: 1})
	opts, err := e.quote(webTo())
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 1 || opts[0].Fee != 30000 || !opts[0].Estimated || opts[0].AccountID != uuid.Nil || opts[0].Preferred {
		t.Fatalf("dự phòng: %+v, muốn một lựa chọn estimated 30.000", opts)
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET show_on_web = false")
	if opts, err := e.quote(webTo()); err != nil || len(opts) != 1 || !opts[0].Estimated {
		t.Fatalf("không hãng nào trên web: %+v, %v; muốn phí dự phòng", opts, err)
	}
}

// openTxCarrier là hãng giả kiểm lúc được gọi: database thử không có phiên nào đang mở transaction (QuoteForWeb đã đóng
// transaction đọc trước khi gọi hãng, ADR-46).
type openTxCarrier struct {
	quoteCarrier
	e    *webEnv
	open []int
	mu   sync.Mutex
}

func (c *openTxCarrier) Quote(ctx context.Context, a fulfillment.Account, in fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	var n int
	err := c.e.staff.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid() AND state LIKE 'idle in transaction%'`).Scan(&n)
	if err != nil {
		return nil, err //nolint:wrapcheck // test
	}
	c.mu.Lock()
	c.open = append(c.open, n)
	c.mu.Unlock()
	return c.quoteCarrier.Quote(ctx, a, in)
}

func TestQuoteForWeb_NoOpenTransaction(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	c := &openTxCarrier{quoteCarrier: quoteCarrier{key: "vtp", results: []fulfillment.QuoteResult{{Service: "STD", Total: 30000}}}, e: e}
	e.svc = fulfillment.New(fulfillment.Deps{DB: e.staff.App, Carriers: mustRegistry(t, c)}).Service
	if _, err := e.quote(webTo()); err != nil {
		t.Fatal(err)
	}
	if len(c.open) != 1 || c.open[0] != 0 {
		t.Fatalf("phiên đang mở transaction lúc gọi hãng: %v, muốn [0]", c.open)
	}
}

// Ước thời gian: 25 vận đơn giao Hà Nội (01) trung vị 2 ngày, 10 vận đơn Cà Mau (96), 1 vận đơn Hà Nội giao quá 60
// ngày trước → chỉ có Hà Nội, 2 ngày.
func TestDeliveryEstimates(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	now := time.Now().UTC()
	for range 36 {
		o, _ := e.order(1)
		e.mustCreateShipment(o.ID)
	}
	e.ownerExec(`WITH n AS (SELECT id, row_number() OVER (ORDER BY id) AS i FROM fulfillment.shipments)
		UPDATE fulfillment.shipments s
		SET ship_to = jsonb_set(s.ship_to, '{provinceCode}', to_jsonb(CASE WHEN n.i <= 26 THEN '01' ELSE '96' END)),
		    picked_up_at = $1::timestamptz - make_interval(days => CASE WHEN n.i = 26 THEN 70 ELSE 0 END)
		                 - make_interval(hours => (24 * (1 + n.i % 3))::int),
		    delivered_at = $1::timestamptz - make_interval(days => CASE WHEN n.i = 26 THEN 70 ELSE 0 END)
		FROM n WHERE n.id = s.id`, now)
	got := readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (map[string]float64, error) {
		return e.svc.DeliveryEstimates(ctx, tx, now.Add(time.Minute))
	})
	if len(got) != 1 || got["01"] != 2 {
		t.Fatalf("ước thời gian = %v, muốn map[01:2]", got)
	}
}

// Luật Task 450: tắt show_on_web của hãng active cuối cùng trên web → ErrLastWebCarrier, không đổi gì; tắt hãng đang
// ưu tiên → bỏ ưu tiên cùng transaction (version + 1, có sự kiện); hãng không active thì tắt được.
func TestUpdateAccount_WebCarrierRules(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	e.mustSave(fulfillment.WebCarrierSettings{PreferredAccountID: &e.accBest, FallbackFee: new(int64(25000)), Version: 1})
	if err := e.hide(e.accBest); err != nil {
		t.Fatal(err)
	}
	s := e.settings()
	if s.PreferredAccountID != nil || s.Version != 3 || s.FallbackFee == nil || *s.FallbackFee != 25000 {
		t.Fatalf("tắt hãng ưu tiên: %+v, muốn bỏ ưu tiên, giữ phí, version 3", s)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = $1`, fulfillment.TopicWebCarrierSettingsChanged); n != 2 {
		t.Fatalf("sự kiện = %d, muốn 2", n)
	}
	if err := e.hide(e.accVTP); !errors.Is(err, fulfillment.ErrLastWebCarrier) {
		t.Fatalf("tắt hãng cuối: %v, muốn ErrLastWebCarrier", err)
	}
	if got := e.carriers(); len(got) != 1 || got[0].AccountID != e.accVTP {
		t.Fatalf("sau lỗi: %+v, muốn vẫn còn vtp", got)
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'paused', show_on_web = true WHERE id = $1", e.accGHN)
	if err := e.hide(e.accGHN); err != nil {
		t.Fatalf("tắt hãng đang tạm dừng: %v", err)
	}
}

// Hai lệnh tắt hai hãng cuối cùng cùng lúc: đúng một lệnh qua, lệnh kia ErrLastWebCarrier; không deadlock.
func TestUpdateAccount_LastWebCarrierConcurrent(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, id := range []uuid.UUID{e.accVTP, e.accBest} {
		wg.Go(func() { errs[i] = e.hide(id) })
	}
	wg.Wait()
	ok, last := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, fulfillment.ErrLastWebCarrier):
			last++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || last != 1 || len(e.carriers()) != 1 {
		t.Fatalf("qua %d, ErrLastWebCarrier %d, còn %d hãng; muốn 1, 1, 1", ok, last, len(e.carriers()))
	}
}
