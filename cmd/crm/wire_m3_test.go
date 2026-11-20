package main

import (
	"errors"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM3 giữ đúng điều kiện của M1 (Task M3-04, commit cấu trúc): consumer sổ COD luôn có; hai lệnh vận chuyển, nguồn
// webhook và handler tín hiệu của hãng giả chỉ khi ngoài production và có FAKECARRIER_URL; khung các vùng chưa thêm gì.
func TestWireM3_KeepsM1Parts(t *testing.T) {
	t.Parallel()
	fakeCfg := config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1"}
	cases := []struct {
		name     string
		cfg      config.Config
		commands []string
		source   bool
	}{
		{"hãng giả", fakeCfg, []string{"command.cash.statement_parse", "fulfillment.create_shipment", "fulfillment.refresh_status"}, true},
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
	if len(p.Consumers) != 1 || p.Consumers[0].Name() != "cash" {
		t.Errorf("consumer phải đúng một cash, có %d", len(p.Consumers))
	}
	want := 0
	if source {
		want = 1
	}
	if len(p.Sources) != want || len(p.Inbound) != want ||
		(source && (p.Sources[0].Key() != fake.Key || p.Inbound[0].Source() != fake.Key)) {
		t.Errorf("nguồn %d, handler vào %d, mong hãng giả = %v", len(p.Sources), len(p.Inbound), source)
	}
	if len(p.Schedules)+len(p.Cleanups) != 0 {
		t.Errorf("khung chưa có lịch, dọn: %d %d", len(p.Schedules), len(p.Cleanups))
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
