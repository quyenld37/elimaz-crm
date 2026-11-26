package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production, là nguồn webhook duy nhất của M4 lúc này (adapter
// Facebook chưa hiện thực); production không có kênh nào. Lệnh: đăng ký, hủy đăng ký webhook của Page (Task 149), kiểm
// sức khỏe, hỏi bù, đọc comment (Task 150); lịch là bốn lịch sức khỏe kênh (Task 150), conversation.presence_cleanup
// (Task 163), conversation.reminders (Task 169), channel.comment_poll (Task 150), mỗi lịch một handler cùng Kind;
// chưa có handler vào, luồng nền; ingest vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
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
		if !slices.Equal(cmds, []string{
			channel.SubscribePageKind, channel.UnsubscribePageKind, channel.HealthCheckKind, channel.BackfillKind,
			channel.CommentPollKind,
		}) {
			t.Errorf("%s: lệnh M4 = %v", c.cfg.Env, cmds)
		}
		checkM4Schedules(t, c.cfg.Env, p)
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}

// checkM4Schedules: lịch của M4 đúng tên, đúng thứ tự, chạy theo từng tenant (trừ channel.graph_version, một việc
// không tenant), mỗi lịch một handler cùng Kind.
func checkM4Schedules(t *testing.T, env string, p m4Parts) {
	t.Helper()
	want := []string{
		channel.HealthSchedule, channel.SilenceSchedule, channel.BackfillSchedule, channel.GraphVersionSchedule,
		"conversation.presence_cleanup", "conversation.reminders", "channel.comment_poll",
	}
	if len(p.Schedules) != len(want) || len(p.Jobs) != len(want) {
		t.Errorf("%s: lịch, việc nền của M4: %+v, %d handler", env, p.Schedules, len(p.Jobs))
		return
	}
	for i, name := range want {
		perTenant := name != channel.GraphVersionSchedule
		if p.Schedules[i].Name != name || p.Schedules[i].PerTenant != perTenant || p.Jobs[i].Kind().Name != p.Schedules[i].Kind.Name {
			t.Errorf("%s: lịch %d = %+v, việc %q; muốn %s", env, i, p.Schedules[i], p.Jobs[i].Kind().Name, name)
		}
	}
}
