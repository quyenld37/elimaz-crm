package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production, là nguồn webhook duy nhất của M4 lúc này (adapter
// Facebook chưa hiện thực); production không có kênh nào. Chưa có lệnh, luồng nền; lịch là conversation.presence_cleanup
// (Task 163) và conversation.reminders (Task 169), mỗi lịch một handler cùng Kind; ingest vẫn dựng được (m4Deps rỗng).
// FIELD_ENC_KEYS sai dạng → lỗi khởi động.
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
		if len(p.Inbound)+len(p.Commands)+len(p.Background) != 0 {
			t.Errorf("%s: khung M4 chưa được có phần chạy nền: %+v", c.cfg.Env, p)
		}
		want := []string{"conversation.presence_cleanup", "conversation.reminders"}
		if len(p.Schedules) != len(want) || len(p.Jobs) != len(want) {
			t.Errorf("%s: lịch, việc nền của M4: %+v, %d handler", c.cfg.Env, p.Schedules, len(p.Jobs))
			continue
		}
		for i, name := range want {
			if p.Schedules[i].Name != name || !p.Schedules[i].PerTenant || p.Jobs[i].Kind().Name != p.Schedules[i].Kind.Name {
				t.Errorf("%s: lịch %d = %+v, việc %q; muốn %s", c.cfg.Env, i, p.Schedules[i], p.Jobs[i].Kind().Name, name)
			}
		}
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}
