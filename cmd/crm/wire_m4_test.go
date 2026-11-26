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
// (Task 156), trả lời, nhắn riêng, ẩn comment (Task 158), tự ẩn (Task 165), nhận biết và đối chiếu live (Task 170,
// 171), lịch sử tin (Task 164) — wantM4Commands; một luồng nền đọc comment live khi có kênh Facebook (Task 171); lịch
// xem checkM4Schedules, mỗi lịch một handler cùng Kind; ingest vẫn dựng được (m4Deps rỗng). FIELD_ENC_KEYS sai dạng → lỗi khởi động.
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
		// Luồng nền đọc comment live (Task 171) chỉ khi có kênh Facebook.
		if wantBg := len(slices.DeleteFunc(slices.Clone(c.keys), func(k string) bool { return k != "facebook" })); len(p.Background) != wantBg {
			t.Errorf("%s: %d luồng nền, muốn %d", c.cfg.Env, len(p.Background), wantBg)
		}
		wantCmds := wantM4Commands(slices.Contains(c.keys, "facebook"))
		if cmds := m4CommandKinds(p); !slices.Equal(cmds, wantCmds) {
			t.Errorf("%s: lệnh M4 = %v", c.cfg.Env, cmds)
		}
		checkM4Schedules(t, c.cfg.Env, slices.Contains(c.keys, "facebook"), p)
	}
	if _, err := wireM4(config.Config{FieldEncKeys: "sai"}, m4Deps{}); err == nil {
		t.Error("FIELD_ENC_KEYS sai dạng phải là lỗi khởi động")
	}
}

// wantM4Commands: lệnh M4 theo đúng thứ tự đăng ký (channel, rồi các phần của conversation: inbound, send,
// automation, live, history); facebook: có kênh Facebook (đọc bài, nhận biết live, lịch sử tin).
func wantM4Commands(facebook bool) []string {
	want := []string{
		channel.SubscribePageKind, channel.UnsubscribePageKind, channel.HealthCheckKind, channel.BackfillKind,
		channel.CommentPollKind,
		"command.fb.fetch_attachment", "command.fb.fetch_profile", // Task 155: tệp đính kèm, hồ sơ khách
	}
	if facebook {
		want = append(want, "command.fb.fetch_post") // Task 154: đọc bài của comment (kênh đọc được bài)
	}
	want = append(want, conversation.SendMessageKind, // Task 156: gửi tin, mọi cấu hình
		conversation.ReplyCommentKind, conversation.PrivateReplyKind, conversation.HideCommentKind, // Task 158
		"command.fb.auto_hide_comment", // Task 165: tự ẩn comment, mọi cấu hình
		"command.fb.auto_send")         // Task 167: gửi tin tự động (tin chào), mọi cấu hình
	if facebook {
		want = append(want, "command.fb.live_detect") // Task 170: nhận biết live bằng hỏi định kỳ
	}
	want = append(want, "command.fb.live_reconcile") // Task 171: đối chiếu comment cuối phiên, mọi cấu hình
	if facebook {
		want = append(want, "command.fb.fetch_history") // Task 164: lịch sử tin (kênh đọc được lịch sử)
	}
	return want
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
	want := []string{
		channel.HealthSchedule, channel.SilenceSchedule, channel.BackfillSchedule, channel.GraphVersionSchedule,
		conversation.SendRevealSchedule,
	}
	if facebook {
		want = append(want, "live.detect")
	}
	want = append(want, "conversation.presence_cleanup", "conversation.reminders", "channel.comment_poll",
		"conversation.retention") // Task 164: dọn theo hạn giữ
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
