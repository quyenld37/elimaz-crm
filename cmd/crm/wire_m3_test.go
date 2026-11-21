package main

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// wireM3 giữ đúng điều kiện của M1 (Task M3-04, commit cấu trúc): consumer sổ COD luôn có; hai lệnh vận chuyển, nguồn
// webhook và handler tín hiệu của hãng giả chỉ khi ngoài production và có FAKECARRIER_URL; vùng returns (M3-23) thêm
// consumer fulfillment_return_received; vùng statements (M3-32) thêm consumer cash_statements; vùng payments (M3-30)
// thêm consumer cash_refunds; vùng cancel (M3-14) thêm consumer fulfillment_order_cancel và, khi có hãng, lệnh
// command.shipment.cancel.
func TestWireM3_KeepsM1Parts(t *testing.T) {
	t.Parallel()
	fakeCfg := config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1"}
	cases := []struct {
		name     string
		cfg      config.Config
		commands []string
		source   bool
	}{
		{"hãng giả", fakeCfg, []string{"command.cash.statement_parse", "command.shipment.cancel", "fulfillment.create_shipment", "fulfillment.refresh_status"}, true},
		{"production", config.Config{Env: "production", FakeCarrierURL: "http://127.0.0.1:1"}, []string{"command.cash.statement_parse"}, false},
		{"không URL", config.Config{Env: "development"}, []string{"command.cash.statement_parse"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkM3Parts(t, c.cfg, c.commands, c.source)
		})
	}
	if _, err := wireM3(config.RoleWorker, config.Config{FieldEncKeys: "sai"}, m3Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}

// checkM3Parts dựng wireM3 cho worker với cfg và so lệnh (Kind, đã sắp), consumer, nguồn và handler vào của hãng giả.
func checkM3Parts(t *testing.T, cfg config.Config, commands []string, source bool) {
	t.Helper()
	p, err := wireM3(config.RoleWorker, cfg, m3Deps{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(p.Commands))
	for _, h := range p.Commands {
		kinds = append(kinds, h.Kind().Name)
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, commands) {
		t.Errorf("lệnh = %v, mong %v", kinds, commands)
	}
	consumers := make([]string, 0, len(p.Consumers))
	for _, c := range p.Consumers {
		consumers = append(consumers, c.Name())
	}
	slices.Sort(consumers)
	// cash (M1), fulfillment_order_cancel (vùng cancel, Task M3-14), fulfillment_return_received (vùng returns, M3-23).
	if want := []string{"cash", "cash_refunds", "cash_statements", "fulfillment_order_cancel", "fulfillment_return_received"}; !slices.Equal(consumers, want) {
		t.Errorf("consumer = %v, mong %v", consumers, want)
	}
	want := 0
	if source {
		want = 1
	}
	if len(p.Sources) != want || len(p.Inbound) != want ||
		(source && (p.Sources[0].Key() != fake.CarrierKey || p.Inbound[0].Source() != fake.CarrierKey)) {
		t.Errorf("nguồn %d, handler vào %d, mong hãng giả = %v", len(p.Sources), len(p.Inbound), source)
	}
	// Dọn: một hàm của vùng quotes (báo phí quá 7 ngày, Task M3-13); chưa vùng nào có lịch.
	if len(p.Schedules) != 0 || len(p.Cleanups) != 1 {
		t.Errorf("lịch, dọn: %d %d, mong 0 1", len(p.Schedules), len(p.Cleanups))
	}
}

// Hai phần cùng tên giữa hai module (hay với phần hãng giả nối thêm) là lỗi khởi động.
func TestCheckM3Unique(t *testing.T) {
	t.Parallel()
	p, err := wireM3(config.RoleWorker, config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1"}, m3Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkM3Unique(p); err != nil {
		t.Fatalf("không trùng: %v", err)
	}
	p.Consumers = append(p.Consumers, p.Consumers[0])
	p.Sources = append(p.Sources, p.Sources[0])
	if err := checkM3Unique(p); !errors.Is(err, background.ErrDuplicatePart) {
		t.Fatalf("trùng consumer, nguồn: mong ErrDuplicatePart, có %v", err)
	}
}

// Nguồn hãng giả (Task M3-10): /hooks/fake/<route_key>, kiểm chữ ký bằng FAKECARRIER_WEBHOOK_SECRET chỉ ở vai trò
// ingest; worker dựng nguồn với bí mật rỗng (chỉ tách gói, chữ ký không bao giờ đúng).
func TestWireM3_FakeWebhookSecretByRole(t *testing.T) {
	t.Parallel()
	const secret = "0123456789abcdef0123456789abcdef"
	cfg := config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1", FakeCarrierWebhookSecret: secret}
	body := []byte(`{"events":[]}`)
	h := http.Header{}
	h.Set(fake.SignatureHeader, fake.Sign(body, secret))
	for role, want := range map[config.Role]bool{config.RoleIngest: true, config.RoleWorker: false} {
		p, err := wireM3(role, cfg, m3Deps{})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Sources) != 1 || p.Sources[0].Key() != fake.CarrierKey {
			t.Fatalf("%s: nguồn = %d", role, len(p.Sources))
		}
		if _, routed := p.Sources[0].(webhooks.RoutedSource); !routed {
			t.Fatalf("%s: nguồn hãng giả không nhận đường có route_key", role)
		}
		if got := p.Sources[0].VerifySignature(body, h); got != want {
			t.Errorf("%s: chữ ký đúng → %v, muốn %v", role, got, want)
		}
	}
}
