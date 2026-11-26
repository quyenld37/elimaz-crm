package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production, là nguồn webhook duy nhất của M4 lúc này (adapter
// Facebook chưa hiện thực); production không có kênh nào. Lệnh: chỉ đăng ký, hủy đăng ký webhook của Page (Task 149);
// lịch duy nhất là conversation.reminders (Task 169) cùng handler của nó; chưa có handler vào, luồng nền; ingest vẫn
// dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
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
		if len(p.Inbound)+len(p.Background) != 0 {
			t.Errorf("%s: khung M4 chưa được có handler vào, luồng nền: %+v", c.cfg.Env, p)
		}
		var cmds []string
		for _, h := range p.Commands {
			cmds = append(cmds, h.Kind().Name)
		}
		if !slices.Equal(cmds, []string{channel.SubscribePageKind, channel.UnsubscribePageKind}) {
			t.Errorf("%s: lệnh M4 = %v", c.cfg.Env, cmds)
		}
		if len(p.Schedules) != 1 || p.Schedules[0].Name != "conversation.reminders" || !p.Schedules[0].PerTenant ||
			len(p.Jobs) != 1 || p.Jobs[0].Kind().Name != p.Schedules[0].Kind.Name {
			t.Errorf("%s: lịch, việc nền của M4: %+v, %d handler", c.cfg.Env, p.Schedules, len(p.Jobs))
		}
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}
