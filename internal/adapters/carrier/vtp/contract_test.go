package vtp_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp/vtptest"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const (
	testUser = "0900000001"
	testPass = "mat-khau-thu"
	// Địa chỉ mẫu dùng để quét rò rỉ (cùng SĐT mẫu của carriertest).
	samplePickupLine = "1 Hàng Bài"
	sampleToLine     = "12 Lý Thường Kiệt"
)

// sample đọc trường "reply" của testdata/<name> (mẫu chép từ tài liệu VTP).
func sample(t *testing.T, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Source string          `json:"_source"`
		Reply  json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(b, &f); err != nil || f.Source == "" || len(f.Reply) == 0 {
		t.Fatalf("testdata/%s: thiếu _source hay reply: %v", name, err)
	}
	return f.Reply
}

// locMap là bảng đổi mã địa giới CRM → mã VTP của test.
type locMap map[string]string

func (m locMap) Carrier(level, our string) (string, bool) {
	c, ok := m[level+":"+our]
	return c, ok
}

// Mã CRM của carriertest (01/00004, 01/00070) và của ca Hà Nội → TP. Hồ Chí Minh (79/26734) đổi sang ID VTP.
func testLocations() locMap {
	return locMap{
		"province:01": "1", "ward:00004": "49876", "ward:00070": "49902",
		"province:79": "2", "ward:26734": "51284",
	}
}

type env struct {
	srv *vtptest.Server
	hs  *httptest.Server
	ad  *vtp.Adapter
	acc fulfillment.Account
}

func newEnv(t *testing.T) *env {
	t.Helper()
	srv := vtptest.New(vtptest.Options{
		Username: testUser, Password: testPass,
		Login: sample(t, "login.json"), Provinces: sample(t, "provinces_v3.json"), Wards: sample(t, "wards_v3.json"),
		PriceAll: sample(t, "price_all.json"), Price: sample(t, "price.json"), CreateOrder: sample(t, "create_order.json"),
		UpdateOrder: sample(t, "update_order.json"), PrintingCode: sample(t, "printing_code.json"),
		Hang: 2 * time.Second,
	})
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	ad := vtp.New(vtp.Config{BaseURL: hs.URL, PrintURL: hs.URL, HTTPClient: hs.Client()})
	acc := fulfillment.Account{
		ID: uuid.New(), ExternalAccountRef: "vtp-1", CarrierKey: vtp.Key, Environment: "sandbox", RouteKey: "rk-vtp-1",
		Creds: fulfillment.Credentials{"username": testUser, "password": testPass},
		Pickup: fulfillment.Address{
			Name: "Kho MAIN", Phone: carriertest.SamplePhone, Line: samplePickupLine, ProvinceCode: "01", WardCode: "00070",
		},
		Locations: testLocations(),
	}
	return &env{srv: srv, hs: hs, ad: ad, acc: acc}
}

func contextTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(t.Context(), d)
}

// Bộ contract chung trên máy chủ giả vtptest: khả năng khai true qua ca của nó, khai false trả ErrNotSupported.
func TestContractVTP(t *testing.T) {
	e := newEnv(t)
	carriertest.Run(t, carriertest.Factory{Adapter: e.ad, Server: e.srv, Account: e.acc})
}
