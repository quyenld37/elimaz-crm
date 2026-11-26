package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/config"
)

// wireM4: kênh nhắn tin giả fakechat (Task 145) có ngoài production; kênh Facebook khi Meta bật và có app secret (Task
// 148, nối ở Task 153); production không cấu hình Meta thì không có kênh nào. Mỗi nguồn có đúng một handler vào của
// conversation (Task 153); lệnh: đăng ký, hủy đăng ký webhook của Page (Task 149), kiểm sức khỏe, hỏi bù, đọc
// comment (Task 150), tải tệp đính kèm, đọc hồ sơ khách (Task 155), đọc bài khi có kênh Facebook (Task 154), gửi tin
// (Task 156), trả lời, nhắn riêng, ẩn comment (Task 158); chưa có luồng nền; lịch là bốn lịch sức khỏe kênh (Task
// 150), conversation.presence_cleanup (Task 163), conversation.reminders (Task 169), channel.comment_poll (Task 150),
// mỗi lịch một handler cùng Kind; ingest vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
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
		wantCmds := []string{
			channel.SubscribePageKind, channel.UnsubscribePageKind, channel.HealthCheckKind, channel.BackfillKind,
			channel.CommentPollKind,
			"command.fb.fetch_attachment", "command.fb.fetch_profile", // Task 155: tệp đính kèm, hồ sơ khách
		}
		if slices.Contains(c.keys, "facebook") {
			wantCmds = append(wantCmds, "command.fb.fetch_post") // Task 154: đọc bài của comment (kênh đọc được bài)
		}
		wantCmds = append(wantCmds, conversation.SendMessageKind, // Task 156: gửi tin, mọi cấu hình
			conversation.ReplyCommentKind, conversation.PrivateReplyKind, conversation.HideCommentKind) // Task 158
		if slices.Contains(c.keys, "facebook") {
			wantCmds = append(wantCmds, "command.fb.live_detect") // Task 170: nhận biết live bằng hỏi định kỳ
		}
		if cmds := m4CommandKinds(p); !slices.Equal(cmds, wantCmds) {
			t.Errorf("%s: lệnh M4 = %v", c.cfg.Env, cmds)
		}
		checkM4Schedules(t, c.cfg.Env, slices.Contains(c.keys, "facebook"), p)
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

// checkM4Schedules: lịch của M4 đúng tên, đúng thứ tự, chạy theo từng tenant (trừ channel.graph_version, một việc
// không tenant), mỗi lịch một handler cùng Kind: việc nền (p.Jobs, cùng thứ tự), riêng live.detect (Task 170, chỉ khi
// có kênh Facebook) ghi lệnh ra ngoài nên handler là lệnh command.fb.live_detect trong p.Commands.
func checkM4Schedules(t *testing.T, env string, facebook bool, p m4Parts) {
	t.Helper()
	want := []string{channel.HealthSchedule, channel.SilenceSchedule, channel.BackfillSchedule, channel.GraphVersionSchedule}
	if facebook {
		want = append(want, "live.detect")
	}
	want = append(want, "conversation.presence_cleanup", "conversation.reminders", "channel.comment_poll")
	jobs := len(want)
	if facebook {
		jobs--
	}
	if len(p.Schedules) != len(want) || len(p.Jobs) != jobs {
		t.Errorf("%s: lịch, việc nền của M4: %+v, %d handler", env, p.Schedules, len(p.Jobs))
		return
	}
	j := 0
	for i, name := range want {
		s := p.Schedules[i]
		perTenant := name != channel.GraphVersionSchedule
		if s.Name != name || s.PerTenant != perTenant {
			t.Errorf("%s: lịch %d = %+v; muốn %s", env, i, s, name)
			continue
		}
		if name == "live.detect" {
			if !slices.Contains(m4CommandKinds(p), s.Kind.Name) {
				t.Errorf("%s: lịch live.detect ghi lệnh %q không có handler", env, s.Kind.Name)
			}
			continue
		}
		if p.Jobs[j].Kind().Name != s.Kind.Name {
			t.Errorf("%s: lịch %d = %+v, việc %q; muốn %s", env, i, s, p.Jobs[j].Kind().Name, name)
		}
		j++
	}
}
