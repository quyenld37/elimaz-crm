package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const e2eCarrierName = "Hãng giao hàng A"

// fakeCarrierService dựng fulfillment.Service có adapter hãng giả nối tới máy chủ tools/fakecarrier (phí cố định
// quoteFee) chạy trong httptest, trên database của e.
func fakeCarrierService(t *testing.T, e *seedEnv, quoteFee int64) *fulfillment.Service {
	t.Helper()
	const secret = "0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(server.New(server.Options{Secret: secret, QuoteFee: quoteFee, Logger: slog.New(slog.DiscardHandler)}))
	t.Cleanup(srv.Close)
	reg, err := fulfillment.NewRegistry(fake.NewClient(srv.URL, secret, &http.Client{Timeout: 5 * time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	return fulfillment.New(fulfillment.Deps{DB: e.s.App, Queue: queue.NewQueue(), Events: e.seeder.Events, Carriers: reg}).Service
}

// Hãng giả trên web (Task 511): không cờ → không tạo tài khoản hãng; có cờ → tài khoản hãng giả đúng tên, hiện trên
// web, là ưu tiên, phí dự phòng đúng phí; báo phí cho web qua QuoteForWeb (đường thật của website) ra đúng một lựa
// chọn tên hãng đó với phí cố định 30.000; chạy lại → 0 thay đổi; đổi tên, phí → sửa đúng phần đó, có audit, sự kiện.
func TestSeedWebFixture_WebCarrier(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	svc := fakeCarrierService(t, e, 30000)
	raw := readFixture(t)
	e.seed(raw)
	if n := e.count("SELECT count(*) FROM fulfillment.carrier_accounts"); n != 0 {
		t.Fatalf("không cờ --web-carrier mà có %d tài khoản hãng", n)
	}
	e.seeder.Fulfillment, e.seeder.Carrier = svc, webCarrierSeed{Name: e2eCarrierName, Fee: 30000}
	rep := e.seed(raw)
	if rep.Updated["carrier_account"] != 1 || rep.Updated["carrier_location"] != 4 || rep.Updated["web_carrier_settings"] != 1 || rep.Total() != 6 {
		t.Fatalf("bật hãng lần đầu: %+v", rep)
	}
	opts, err := svc.QuoteForWeb(t.Context(), e.s.App, e.actor.TenantID, fulfillment.WebShipQuoteInput{
		ProvinceCode: "101", WardCode: "10101", WeightGrams: 1200, Value: 499000, COD: 529000,
	})
	if err != nil || len(opts) != 1 || opts[0].CarrierName != e2eCarrierName || opts[0].Fee != 30000 ||
		!opts[0].Preferred || opts[0].Estimated || opts[0].CarrierKey != fake.CarrierKey {
		t.Fatalf("báo phí cho web = %+v %v", opts, err)
	}
	set := e.webSettings()
	if set.FallbackFee == nil || *set.FallbackFee != 30000 || set.PreferredAccountID == nil || *set.PreferredAccountID != opts[0].AccountID {
		t.Fatalf("cài đặt hãng trên web: %+v", set)
	}
	audits := "SELECT count(*) FROM identity.audit_logs WHERE action = ANY($1::text[])"
	actions := []string{"fulfillment.carrier_account.update", "fulfillment.web_carrier_settings.update"}
	if n := e.count(audits, actions); n != 2 {
		t.Fatalf("audit của bước hãng = %d, mong 2", n)
	}
	if rep := e.seed(raw); rep.Total() != 0 {
		t.Fatalf("chạy lại phải 0 thay đổi: %+v", rep)
	}
	e.seeder.Carrier = webCarrierSeed{Name: "Hãng giao hàng B", Fee: 25000}
	if rep := e.seed(raw); rep.Updated["carrier_account"] != 1 || rep.Updated["web_carrier_settings"] != 1 || rep.Total() != 2 {
		t.Fatalf("đổi tên, phí: %+v", rep)
	}
	cars := e.webCarriers(svc)
	if len(cars) != 1 || cars[0].Name != "Hãng giao hàng B" || !cars[0].Preferred {
		t.Fatalf("hãng trên web sau khi đổi tên: %+v", cars)
	}
	if set := e.webSettings(); set.FallbackFee == nil || *set.FallbackFee != 25000 {
		t.Fatalf("phí dự phòng sau khi đổi: %+v", set)
	}
	if n := e.count(audits, actions); n != 4 {
		t.Fatalf("audit sau khi đổi = %d, mong 4", n)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = $1", fulfillment.TopicWebCarrierSettingsChanged); n != 2 {
		t.Fatalf("sự kiện %s = %d, mong 2", fulfillment.TopicWebCarrierSettingsChanged, n)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.carrier_accounts"); n != 1 {
		t.Fatalf("chạy nhiều lần phải dùng lại một tài khoản, có %d", n)
	}
}

// Lỗi ở bước hãng (sau mẫu, trước mã, tồn) → cả transaction lùi: không mẫu, không tài khoản hãng.
func TestSeedWebFixture_WebCarrierFailureRollsBack(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	e.seeder.Fulfillment = fakeCarrierService(t, e, 30000)
	e.seeder.Carrier = webCarrierSeed{Name: e2eCarrierName, Fee: fulfillment.MaxWebFallbackFee + 1}
	if err := e.seedSample(); !errors.Is(err, fulfillment.ErrSettingOutOfRange) {
		t.Fatalf("phí quá trần phải lỗi ErrSettingOutOfRange, có %v", err)
	}
	for _, q := range []string{"SELECT count(*) FROM catalog.products", "SELECT count(*) FROM fulfillment.carrier_accounts"} {
		if n := e.count(q); n != 0 {
			t.Errorf("%s = %d sau khi lùi", q, n)
		}
	}
}

// Ngày tạo của mẫu = mốc cố định + position giây (sắp xếp «Mới nhất» của website ổn định theo tệp).
func TestSeedWebFixture_CreatedAtOrder(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	raw := readFixture(t)
	fx, err := parseWebFixture(raw)
	if err != nil {
		t.Fatal(err)
	}
	e.seed(raw)
	got := map[uuid.UUID]time.Time{}
	e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT id, created_at FROM catalog.products")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return err //nolint:wrapcheck // test
			}
			got[id] = at
		}
		return rows.Err()
	})
	if len(got) != len(fx.Products) {
		t.Fatalf("có %d mẫu, mong %d", len(got), len(fx.Products))
	}
	for _, p := range fx.Products {
		if want := fixtureCreatedAt(p.Position); !got[p.ID].Equal(want) {
			t.Errorf("mẫu %s: created_at %s, mong %s", p.Key, got[p.ID], want)
		}
	}
	newest := slices.SortedFunc(slices.Values(fx.Products), func(a, b fixtureProduct) int { return got[b.ID].Compare(got[a.ID]) })
	if newest[0].Position != len(fx.Products)-1 {
		t.Errorf("«Mới nhất» đứng đầu phải là mẫu position lớn nhất, có %s (%d)", newest[0].Key, newest[0].Position)
	}
}

func (e *seedEnv) webSettings() fulfillment.WebCarrierSettings {
	e.t.Helper()
	var set fulfillment.WebCarrierSettings
	e.app(func(tx pgx.Tx) error {
		var err error
		set, err = fulfillment.GetWebCarrierSettings(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	return set
}

func (e *seedEnv) webCarriers(svc *fulfillment.Service) []fulfillment.WebCarrier {
	e.t.Helper()
	var out []fulfillment.WebCarrier
	e.app(func(tx pgx.Tx) error {
		var err error
		out, err = svc.WebCarriers(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	return out
}
