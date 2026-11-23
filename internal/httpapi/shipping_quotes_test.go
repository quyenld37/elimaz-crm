package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// apiQuoteCarrier là hãng giả chỉ báo phí: trả results hay err.
type apiQuoteCarrier struct {
	fulfillment.Unsupported
	key     string
	results []fulfillment.QuoteResult
	err     error
}

func (c apiQuoteCarrier) Key() string { return c.key }

func (apiQuoteCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Quote: true}
}

func (c apiQuoteCarrier) Quote(context.Context, fulfillment.Account, fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	return c.results, c.err
}

// so phí qua API: 202 hai tài khoản; bấm lại cùng khóa → cùng requestId, không thêm việc; fulfillment.quote chỉ ở kho
// khác → 403 (cả GET); sau worker danh sách 28.000, 41.000 rồi lỗi (không feeTotal); đơn lạ → 404; đơn đã có vận đơn
// → 409 shipment_exists.
func TestShippingQuotesAPI(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	tenant := e.shop.Tenant
	a := apiQuoteCarrier{key: "qa", results: []fulfillment.QuoteResult{
		{Service: "EXP", Total: 41000}, {Service: "STD", ServiceName: "Tiêu chuẩn", Total: 28000, ExpectedDays: 3},
	}}
	b := apiQuoteCarrier{key: "qb", err: fulfillment.ErrRejected}
	accounts := map[string]uuid.UUID{}
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		for _, k := range []string{"qa", "qb"} {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: k, Name: "Hãng " + k,
				ExternalAccountRef: k + "-1",
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			accounts[k] = id
		}
		return nil
	})
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.quote"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.quote"}, "warehouse", uuid.NewString())
	reg, err := fulfillment.NewRegistry(a, b)
	if err != nil {
		t.Fatal(err)
	}
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(), Carriers: reg,
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = m.Service
	srv := NewServer(d)
	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := uuid.MustParse(fmt.Sprint(e.mustCreate("q1", orderBody(30000, line(v, 1)))["id"]))
	path := fmt.Sprintf("/api/v1/orders/%s/shipping-quotes", orderID)

	if rec := serveAs(srv, e.keys, http.MethodPost, path, shipW2, "k0", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("quyền ở kho khác: POST %d %s, muốn 403", rec.Code, rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, path, shipW2, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("quyền ở kho khác: GET %d, muốn 403", rec.Code)
	}
	reqID := postQuotesTwice(t, srv, e.keys, path)
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, fulfillment.QuoteKindName); n != 2 {
		t.Fatalf("việc báo phí = %d, muốn 2", n)
	}
	w := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(),
		Commands: commands.NewRunner(e.staff.App, []commands.Handler{fulfillment.NewQuoteCommand(m.Service)}),
	})
	testkit.DrainDue(t, e.staff.Owner, w)
	checkQuoteList(t, serveAs(srv, e.keys, http.MethodGet, path+"?requestId="+fmt.Sprint(reqID), shipAll, "", ""), reqID, accounts)
	// Không requestId → lần gần nhất.
	if latest := decodeJSON(t, serveAs(srv, e.keys, http.MethodGet, path, shipAll, "", "")); latest["requestId"] != reqID {
		t.Fatalf("lần gần nhất = %v, muốn %v", latest["requestId"], reqID)
	}
	other := "/api/v1/orders/" + uuid.NewString() + "/shipping-quotes"
	if rec := serveAs(srv, e.keys, http.MethodPost, other, shipAll, "k2", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("đơn lạ = %d, muốn 404", rec.Code)
	}
	if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		_, err := fulfillment.CreateForOrder(t.Context(), tx, orderID, seedCarrierAccount(t, e), fulfillment.Actor{TenantID: tenant})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	rec := serveAs(srv, e.keys, http.MethodPost, path, shipAll, "k3", `{}`)
	if rec.Code != http.StatusConflict || decodeJSON(t, rec)["error"] != "shipment_exists" {
		t.Fatalf("đơn đã có vận đơn = %d %s, muốn 409 shipment_exists", rec.Code, rec.Body.String())
	}
}

// postQuotesTwice gửi so phí hai lần cùng khóa k1: cả hai 202 hai tài khoản, cùng requestId; trả requestId.
func postQuotesTwice(t *testing.T, srv http.Handler, keys *cfaccesstest.AccessKeys, path string) any {
	t.Helper()
	var reqID any
	for i := range 2 {
		rec := serveAs(srv, keys, http.MethodPost, path, shipAll, "k1", `{"weightGrams":500}`)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("lần %d: %d %s, muốn 202", i+1, rec.Code, rec.Body.String())
		}
		body := decodeJSON(t, rec)
		if accs, _ := body["accounts"].([]any); len(accs) != 2 {
			t.Fatalf("accounts = %v, muốn 2", body["accounts"])
		}
		if i == 0 {
			reqID = body["requestId"]
		} else if body["requestId"] != reqID {
			t.Fatalf("bấm lại cùng khóa: requestId %v, muốn %v", body["requestId"], reqID)
		}
	}
	return reqID
}

// checkQuoteList: 200, đúng requestId, 28.000 và 41.000 (qa, ok, có quoteId) rồi qb error rejected không feeTotal.
func checkQuoteList(t *testing.T, rec *httptest.ResponseRecorder, reqID any, accounts map[string]uuid.UUID) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %d %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	items, _ := body["items"].([]any)
	if len(items) != 3 || body["requestId"] != reqID {
		t.Fatalf("danh sách = %v", body)
	}
	want := []struct {
		account, status string
		quoteID         bool
		amount          any
		code            any
	}{
		{"qa", "ok", true, float64(28000), nil},
		{"qa", "ok", true, float64(41000), nil},
		{"qb", "error", false, nil, "rejected"},
	}
	for i, w := range want {
		it, _ := items[i].(map[string]any)
		_, hasQuote := it["quoteId"]
		if it["carrierAccountId"] != accounts[w.account].String() || it["status"] != w.status ||
			it["feeTotal"] != w.amount || it["errorCode"] != w.code || hasQuote != w.quoteID {
			t.Errorf("dòng %d = %v, muốn %s %s phí %v mã %v", i, it, w.account, w.status, w.amount, w.code)
		}
	}
}
