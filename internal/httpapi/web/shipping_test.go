package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// priceCarrier là hãng giả chỉ báo phí: trả fee (0: lỗi từ chối), đếm số lần gọi.
type priceCarrier struct {
	fulfillment.Unsupported
	key   string
	fee   atomic.Int64
	calls atomic.Int32
}

func (c *priceCarrier) Key() string { return c.key }

func (*priceCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Quote: true}
}

func (c *priceCarrier) Quote(context.Context, fulfillment.Account, fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	c.calls.Add(1)
	if f := c.fee.Load(); f > 0 {
		return []fulfillment.QuoteResult{{Service: "STD", Total: f}}, nil
	}
	return nil, fulfillment.ErrRejected
}

// shipEnv: env (Valkey thật) cộng Service với hãng giả vtp (30.000) và best (28.000), hai tài khoản active hiện trên
// web (best web_sort 1, vtp 2), một khóa web.shipping.quote.
type shipEnv struct {
	*env
	svc             *fulfillment.Service
	vtp, best       *priceCarrier
	accVTP, accBest uuid.UUID
	key             string
}

func newShipEnv(t *testing.T) *shipEnv {
	t.Helper()
	_, conn := valkeyCache(t)
	e := &shipEnv{env: newEnv(t, conn), vtp: &priceCarrier{key: "vtp"}, best: &priceCarrier{key: "best"}}
	e.vtp.fee.Store(30000)
	e.best.fee.Store(28000)
	reg, err := fulfillment.NewRegistry(e.vtp, e.best)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = fulfillment.New(fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Carriers: reg}).Service
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var wh uuid.UUID
		if err := tx.QueryRow(t.Context(), "SELECT id FROM inventory.warehouses LIMIT 1").Scan(&wh); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, a := range []struct {
			dst  *uuid.UUID
			key  string
			sort int
		}{{&e.accVTP, "vtp", 2}, {&e.accBest, "best", 1}} {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: e.seller, WarehouseID: wh, CarrierKey: a.key, Name: "Hãng " + a.key, ExternalAccountRef: a.key + "-1",
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if _, err := tx.Exec(t.Context(), `UPDATE fulfillment.carrier_accounts SET status = 'active', mode = 'manual',
				show_on_web = true, web_sort = $2 WHERE id = $1`, id, a.sort); err != nil {
				return err //nolint:wrapcheck // test
			}
			*a.dst = id
		}
		return nil
	})
	e.key = e.serviceKey("web.shipping.quote")
	e.serve(func(d *httpapi.Deps) { d.Fulfillment = e.svc })
	return e
}

func (e *shipEnv) save(in fulfillment.WebCarrierSettings) {
	e.t.Helper()
	e.app(func(tx pgx.Tx) error {
		_, err := e.svc.SaveWebCarrierSettings(e.t.Context(), tx, in, fulfillment.Actor{TenantID: e.tenant, UserID: e.owner.UserID})
		return err //nolint:wrapcheck // test
	})
}

type shipOption struct {
	AccountID   string `json:"accountId"`
	CarrierKey  string `json:"carrierKey"`
	CarrierName string `json:"carrierName"`
	Fee         int64  `json:"fee"`
	Preferred   bool   `json:"preferred"`
	Estimated   bool   `json:"estimated"`
}

func (e *shipEnv) quote(body string, want int) []shipOption {
	e.t.Helper()
	rec := e.post(e.key, "/api/v1/web/shipping/quote", body)
	if rec.Code != want {
		e.t.Fatalf("báo phí: %d %s, muốn %d", rec.Code, rec.Body.String(), want)
	}
	var out struct {
		Options []shipOption `json:"options"`
		Error   string       `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatal(err)
	}
	if want != http.StatusOK && out.Error != "retry_later" {
		e.t.Fatalf("mã lỗi %q, muốn retry_later", out.Error)
	}
	return out.Options
}

const shipBody = `{"provinceCode":"01","wardCode":"00004","weightGrams":%d,"value":300000,"cod":300000}`

func shipReq(grams int) string { return fmt.Sprintf(shipBody, grams) }

// Danh sách hãng: hai hãng theo web_sort, đệm 60 s; khóa thiếu quyền web.shipping.quote → 403; ưu tiên vtp → vtp đứng
// đầu (lần đọc sau khi đệm làm mới).
func TestWebShippingCarriers(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	if rec := e.get(e.env.key, "/api/v1/web/shipping/carriers"); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa web.catalog.read: %d, muốn 403", rec.Code)
	}
	read := func() []map[string]any {
		rec := e.get(e.key, "/api/v1/web/shipping/carriers")
		if rec.Code != http.StatusOK {
			t.Fatalf("hãng trên web: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Carriers []map[string]any `json:"carriers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Carriers
	}
	got := read()
	if len(got) != 2 || got[0]["accountId"] != e.accBest.String() || got[1]["accountId"] != e.accVTP.String() ||
		got[0]["name"] != "Hãng best" || got[0]["preferred"] != false {
		t.Fatalf("hãng = %v, muốn best rồi vtp", got)
	}
	if ttl := e.rc.ttl(cache.SpaceCarriers); ttl != 60*time.Second {
		t.Fatalf("hạn đệm hãng = %s, muốn 60 s", ttl)
	}
	e.save(fulfillment.WebCarrierSettings{PreferredAccountID: &e.accVTP, Version: 1})
	if err := e.rc.BumpSpace(t.Context(), e.tenant, cache.SpaceCarriers); err != nil { // việc của consumer webcache
		t.Fatal(err)
	}
	if got := read(); got[0]["accountId"] != e.accVTP.String() || got[0]["preferred"] != true {
		t.Fatalf("sau ưu tiên vtp: %v", got)
	}
}

// Báo phí: hai lựa chọn rẻ trước; gọi lại cùng mức làm tròn (300 g, 400 g → 500 g) không gọi hãng (đệm 10 phút); lưu
// cài đặt (ưu tiên vtp) đổi version → gọi lại hãng, vtp đứng đầu; mọi hãng lỗi, không phí dự phòng → 503 retry_later,
// không đệm (lần sau gọi lại hãng); phí dự phòng 30.000 → một lựa chọn estimated.
func TestWebShippingQuote(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	if rec := e.post(e.env.key, "/api/v1/web/shipping/quote", shipReq(300)); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d, muốn 403", rec.Code)
	}
	opts := e.quote(shipReq(300), http.StatusOK)
	if len(opts) != 2 || opts[0].AccountID != e.accBest.String() || opts[0].Fee != 28000 || opts[1].Fee != 30000 ||
		opts[0].CarrierKey != "best" || opts[0].CarrierName != "Hãng best" || opts[0].Estimated {
		t.Fatalf("báo phí: %+v", opts)
	}
	e.quote(shipReq(400), http.StatusOK)
	if n := e.vtp.calls.Load() + e.best.calls.Load(); n != 2 {
		t.Fatalf("hãng được gọi %d lần sau hai lần báo cùng mức, muốn 2 (lần hai từ bộ đệm)", n)
	}
	if ttl := e.rc.ttl(cache.SpaceShipQuote); ttl != 10*time.Minute {
		t.Fatalf("hạn đệm báo phí = %s, muốn 10 phút", ttl)
	}
	e.save(fulfillment.WebCarrierSettings{PreferredAccountID: &e.accVTP, Version: 1})
	opts = e.quote(shipReq(300), http.StatusOK)
	if n := e.vtp.calls.Load() + e.best.calls.Load(); n != 4 || opts[0].AccountID != e.accVTP.String() || !opts[0].Preferred {
		t.Fatalf("sau lưu cài đặt: %d lần gọi, %+v; muốn 4 lần, vtp đứng đầu", n, opts)
	}
	e.vtp.fee.Store(0)
	e.best.fee.Store(0)
	e.quote(shipReq(1000), http.StatusServiceUnavailable)
	e.quote(shipReq(1000), http.StatusServiceUnavailable)
	if n := e.vtp.calls.Load() + e.best.calls.Load(); n != 8 {
		t.Fatalf("503 bị đệm: %d lần gọi, muốn 8", n)
	}
	e.save(fulfillment.WebCarrierSettings{FallbackFee: new(int64(30000)), Version: 2})
	opts = e.quote(shipReq(1000), http.StatusOK)
	if len(opts) != 1 || !opts[0].Estimated || opts[0].Fee != 30000 || opts[0].AccountID != "" || opts[0].CarrierKey != "" {
		t.Fatalf("phí dự phòng: %+v", opts)
	}
	if rec := e.post(e.key, "/api/v1/web/shipping/quote", `{"provinceCode":"01","wardCode":"00004","weightGrams":0,"value":1,"cod":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("khối lượng 0: %d, muốn 400", rec.Code)
	}
	if rec := e.post(e.key, "/api/v1/web/shipping/quote", `{"provinceCode":"01:x","wardCode":"00004","weightGrams":1,"value":1,"cod":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("mã tỉnh có ký tự lạ: %d, muốn 400", rec.Code)
	}
}

// Ước thời gian giao: chưa có vận đơn → danh sách rỗng, đệm 1 giờ.
func TestWebDeliveryEstimates(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	rec := e.get(e.key, "/api/v1/web/shipping/estimates")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"provinces":[]}` {
		t.Fatalf("ước thời gian: %d %s", rec.Code, rec.Body.String())
	}
	if ttl := e.rc.ttl(cache.SpaceEstimates); ttl != time.Hour {
		t.Fatalf("hạn đệm = %s, muốn 1 giờ", ttl)
	}
}
