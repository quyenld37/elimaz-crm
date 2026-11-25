package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production, là nguồn webhook duy nhất của M4 lúc này (adapter
// Facebook chưa hiện thực); production không có kênh nào. Mỗi nguồn có đúng một handler vào của conversation (Task
// 153); các phần khác (lệnh, lịch, luồng nền) chưa có; ingest vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng →
// lỗi khởi động.
func TestWireM4_Frame(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cfg  config.Config
		keys []string
	}{
		{config.Config{Env: "production"}, nil},
		{config.Config{Env: "dev"}, []string{"fakechat"}},
		{config.Config{Env: "dev", Meta: config.MetaConfig{Enabled: true}}, []string{"fakechat"}},
	} {
		p, err := wireM4(c.cfg, m4Deps{})
		if err != nil {
			t.Fatalf("%+v: %v", c.cfg.Env, err)
		}
		if got := p.Channels.Keys(); !slices.Equal(got, c.keys) && len(got)+len(c.keys) != 0 {
			t.Errorf("%s: kênh = %v, muốn %v", c.cfg.Env, got, c.keys)
		}
		if len(p.Sources) != len(c.keys) {
			t.Errorf("%s: %d nguồn webhook, muốn %d", c.cfg.Env, len(p.Sources), len(c.keys))
		}
		var inbound []string
		for _, h := range p.Inbound {
			inbound = append(inbound, h.Source())
		}
		if slices.Sort(inbound); !slices.Equal(inbound, c.keys) && len(inbound)+len(c.keys) != 0 {
			t.Errorf("%s: handler vào của nguồn %v, muốn %v", c.cfg.Env, inbound, c.keys)
		}
		if len(p.Commands)+len(p.Jobs)+len(p.Schedules)+len(p.Background) != 0 {
			t.Errorf("%s: khung M4 chưa được có phần chạy nền: %+v", c.cfg.Env, p)
		}
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}
