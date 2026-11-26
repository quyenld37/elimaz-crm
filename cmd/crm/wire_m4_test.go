package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production; kênh Facebook khi Meta bật và có app secret (Task
// 148, nối ở Task 153); production không cấu hình Meta thì không có kênh nào. Mỗi nguồn có đúng một handler vào của
// conversation (Task 153); lệnh: chỉ đăng ký, hủy đăng ký webhook của Page (Task 149); chưa có luồng nền; lịch là
// conversation.presence_cleanup (Task 163) và conversation.reminders (Task 169), mỗi lịch một handler cùng Kind; ingest
// vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
func TestWireM4_Frame(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cfg  config.Config
		keys []string
	}{
		{config.Config{Env: "production"}, nil},
		{config.Config{Env: "dev"}, []string{"fakechat"}},
		{config.Config{Env: "dev", Meta: config.MetaConfig{Enabled: true}}, []string{"fakechat"}},
		{config.Config{Env: "dev", Meta: config.MetaConfig{Enabled: true, AppSecret: "s"}}, []string{"facebook", "fakechat"}},
		{config.Config{Env: "production", Meta: config.MetaConfig{Enabled: true, AppSecret: "s"}}, []string{"facebook"}},
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
		if inbound := m4InboundSources(p); !slices.Equal(inbound, c.keys) && len(inbound)+len(c.keys) != 0 {
			t.Errorf("%s: handler vào của nguồn %v, muốn %v", c.cfg.Env, inbound, c.keys)
		}
		if len(p.Background) != 0 {
			t.Errorf("%s: khung M4 chưa được có luồng nền: %+v", c.cfg.Env, p)
		}
		if cmds := m4CommandKinds(p); !slices.Equal(cmds, []string{channel.SubscribePageKind, channel.UnsubscribePageKind}) {
			t.Errorf("%s: lệnh M4 = %v", c.cfg.Env, cmds)
		}
		checkM4Schedules(t, c.cfg.Env, p)
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}

// m4InboundSources trả nguồn của các handler vào của M4, đã sắp xếp.
func m4InboundSources(p m4Parts) []string {
	var out []string
	for _, h := range p.Inbound {
		out = append(out, h.Source())
	}
	slices.Sort(out)
	return out
}

// m4CommandKinds trả tên loại lệnh của M4 theo thứ tự đăng ký.
func m4CommandKinds(p m4Parts) []string {
	var out []string
	for _, h := range p.Commands {
		out = append(out, h.Kind().Name)
	}
	return out
}

// checkM4Schedules: lịch của M4 đúng tên, đúng thứ tự, chạy theo từng tenant, mỗi lịch một handler cùng Kind.
func checkM4Schedules(t *testing.T, env string, p m4Parts) {
	t.Helper()
	want := []string{"conversation.presence_cleanup", "conversation.reminders"}
	if len(p.Schedules) != len(want) || len(p.Jobs) != len(want) {
		t.Errorf("%s: lịch, việc nền của M4: %+v, %d handler", env, p.Schedules, len(p.Jobs))
		return
	}
	for i, name := range want {
		if p.Schedules[i].Name != name || !p.Schedules[i].PerTenant || p.Jobs[i].Kind().Name != p.Schedules[i].Kind.Name {
			t.Errorf("%s: lịch %d = %+v, việc %q; muốn %s", env, i, p.Schedules[i], p.Jobs[i].Kind().Name, name)
		}
	}
}
