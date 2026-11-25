package main

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4 (Task 143): khung chưa có adapter nào hiện thực, nên ở mọi môi trường bộ adapter rỗng, không nguồn webhook,
// lệnh, lịch, luồng nền; ingest vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
func TestWireM4_Frame(t *testing.T) {
	t.Parallel()
	for _, cfg := range []config.Config{
		{Env: "production"}, {Env: "dev"}, {Env: "dev", Meta: config.MetaConfig{Enabled: true}},
	} {
		p, err := wireM4(cfg, m4Deps{})
		if err != nil {
			t.Fatalf("%+v: %v", cfg.Env, err)
		}
		if len(p.Channels.Keys()) != 0 || len(p.Sources)+len(p.Inbound)+len(p.Commands)+len(p.Jobs)+len(p.Schedules)+len(p.Background) != 0 {
			t.Errorf("%s: khung M4 chưa được có phần nào: %+v", cfg.Env, p)
		}
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}
