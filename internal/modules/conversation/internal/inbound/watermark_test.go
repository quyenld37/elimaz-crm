package inbound_test

import (
	"encoding/json"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// echoAt giao echo page_external mid lúc when.
func (e *env) echoAt(user, mid, text string, when time.Time) {
	e.t.Helper()
	e.deliver(channel.EventMessageEcho, "m:"+mid, user, when, channel.MessagePayload{
		PageID: pageExt1, UserID: user, MID: mid, Text: text, IsEcho: true, EchoAppID: businessSuiteAppID, SentAt: when,
	})
}

// Đã nhận với watermark giữa hai tin ra → chỉ tin cũ hơn có delivered_at; tin ra quá 7 ngày không đổi; đã xem sau
// tin thứ hai → cả hai có read_at, tin chưa "đã nhận" được đặt delivered_at cùng mốc; tin khách không bị đổi.
func TestWatermarkDeliveredAndRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	e.echoAt("u-1", "mid-old", "rất cũ", base.Add(-8*24*time.Hour))
	e.receiveAt("u-1", "mid-in", "hỏi", base)
	e.echoAt("u-1", "mid-o1", "trả lời 1", base.Add(time.Minute))
	e.echoAt("u-1", "mid-o2", "trả lời 2", base.Add(3*time.Minute))
	wm := func(typ string, when time.Time) {
		e.deliver(typ, typ+":"+when.String(), "u-1", when, channel.WatermarkPayload{PageID: pageExt1, UserID: "u-1", Watermark: when})
	}
	wm(channel.EventMessageDelivered, base.Add(2*time.Minute))
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	byMID := func() map[string]msg {
		out := map[string]msg{}
		for _, m := range e.messages(c.ID) {
			out[deref(m.ExternalID)] = m
		}
		return out
	}
	ms := byMID()
	if d := ms["mid-o1"].DeliveredAt; d == nil || !d.Equal(base.Add(2*time.Minute)) {
		t.Errorf("mid-o1 delivered_at = %v", d)
	}
	for _, mid := range []string{"mid-o2", "mid-old", "mid-in"} {
		if ms[mid].DeliveredAt != nil {
			t.Errorf("%s delivered_at = %v, mong rỗng", mid, ms[mid].DeliveredAt)
		}
	}

	wm(channel.EventMessageRead, base.Add(5*time.Minute))
	ms = byMID()
	for _, mid := range []string{"mid-o1", "mid-o2"} {
		if r := ms[mid].ReadAt; r == nil || !r.Equal(base.Add(5*time.Minute)) {
			t.Errorf("%s read_at = %v", mid, r)
		}
	}
	if d := ms["mid-o1"].DeliveredAt; d == nil || !d.Equal(base.Add(2*time.Minute)) {
		t.Errorf("mid-o1 delivered_at bị đè: %v", d)
	}
	if d := ms["mid-o2"].DeliveredAt; d == nil || !d.Equal(base.Add(5*time.Minute)) {
		t.Errorf("mid-o2 delivered_at = %v, mong mốc đã xem", d)
	}
	if ms["mid-old"].ReadAt != nil || ms["mid-in"].ReadAt != nil {
		t.Error("tin quá 7 ngày hay tin khách bị đặt read_at")
	}
	e.noDeadJobs()
}

// Sửa tin → 1 dòng message_revisions giữ nội dung cũ (nguồn customer), body mới, edited_at; giao lại cùng lần sửa,
// hay lần sửa cũ hơn tới muộn → không thêm dòng, không đổi body. Sửa tin chưa có → bỏ qua, việc xong.
func TestMessageEditKeepsRevision(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-1", "size M", 0)
	edit := func(dedupe, text string, sec int) {
		e.deliver(channel.EventMessageEdited, dedupe, "u-1", at(sec), channel.EditPayload{
			PageID: pageExt1, UserID: "u-1", MID: "mid-1", Text: text, EditedAt: at(sec),
		})
	}
	edit("e:1", "size L", 10)
	edit("e:1:lai", "size L", 10)
	edit("e:0", "size S", 5)
	e.deliver(channel.EventMessageEdited, "e:x", "u-1", at(11), channel.EditPayload{PageID: pageExt1, UserID: "u-1", MID: "mid-khong-co", Text: "?", EditedAt: at(11)})
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	ms := e.messages(c.ID)
	if len(ms) != 1 || deref(ms[0].Body) != "size L" || ms[0].EditedAt == nil || !ms[0].EditedAt.Equal(at(10)) {
		t.Fatalf("tin sau sửa: %+v", ms)
	}
	revs := e.texts(`SELECT body || '/' || source FROM conversation.message_revisions WHERE message_id = $1`, ms[0].ID)
	if len(revs) != 1 || revs[0] != "size M/customer" {
		t.Fatalf("lịch sử: %v", revs)
	}
}

// Cảm xúc: thả → reactions {"customer": "love"}; bỏ → không còn khóa customer.
func TestMessageReaction(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-1", "ok", 0)
	e.echoAt("u-1", "mid-o", "dạ", at(1))
	react := func(dedupe, action string) map[string]string {
		e.deliver(channel.EventMessageReaction, dedupe, "u-1", at(2), channel.ReactionPayload{
			PageID: pageExt1, UserID: "u-1", MID: "mid-o", Reaction: "love", Action: action,
		})
		for _, m := range e.messages(e.conversation(e.page1, "u-1").ID) {
			if deref(m.ExternalID) == "mid-o" {
				out := map[string]string{}
				if len(m.Reactions) > 0 {
					if err := json.Unmarshal(m.Reactions, &out); err != nil {
						t.Fatal(err)
					}
				}
				return out
			}
		}
		t.Fatal("không thấy tin mid-o")
		return nil
	}
	if got := react("x:1", "react"); got["customer"] != "love" {
		t.Errorf("sau react: %v", got)
	}
	if got := react("x:2", "unreact"); len(got) != 0 {
		t.Errorf("sau unreact: %v", got)
	}
	e.noDeadJobs()
}
