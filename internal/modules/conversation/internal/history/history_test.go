package history_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
)

// Q33: hội thoại hộp thư mới → đúng 1 lệnh command.fb.fetch_history; máy Meta giả có 25 tin (20 của khách, 4 của Page,
// tin vừa nhận) → sau OnDone hội thoại có 20 tin: 19 origin history (khách: in, Page: out) + tin vừa nhận (customer,
// không nhân đôi); unread_count vẫn 1; không lệnh nào khác (không gửi, không tự động); một báo hiệu cho cả lô.
func TestFetchHistory_NewConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	for i := range 20 {
		e.fg.AddInboundMessage(pageExt, psid, "m_old_"+strconv.Itoa(i), "tin cũ "+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Minute))
	}
	acc := channel.ChannelAccount{ID: e.page, Channel: "facebook", ExternalID: pageExt, Status: "active", SendMode: "enabled"}
	for i := range 4 {
		out, err := e.fb.Send(t.Context(), acc, channel.Credentials{AccessToken: pageToken}, channel.OutboundCommand{
			Purpose: channel.PurposeMessage, RecipientID: psid, Text: "shop trả lời " + strconv.Itoa(i),
			Mode: channel.SendStandard, ClientRef: "ref-" + strconv.Itoa(i),
		})
		if err != nil || out.Status != channel.OutcomeSent {
			t.Fatalf("gửi tin Page: %+v, %v", out, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	e.fg.AddInboundMessage(pageExt, psid, "m_new", "chị hỏi mẫu này", now)

	e.receive("m_new", "chị hỏi mẫu này", now)

	conv := e.conversationID()
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, history.FetchHistoryKind); n != 1 {
		t.Fatalf("%d lệnh %s, mong 1", n, history.FetchHistoryKind)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'`, history.FetchHistoryKind); n != 1 {
		t.Fatalf("lệnh lịch sử chưa done")
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1`, conv); n != 20 {
		t.Errorf("%d tin trong hội thoại, mong 20", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND origin = 'history'`, conv); n != 19 {
		t.Errorf("%d tin history, mong 19", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND origin = 'history'
		AND direction = 'out'`, conv); n != 4 {
		t.Errorf("%d tin history của Page, mong 4", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE external_id = 'm_new' AND origin = 'customer'`); n != 1 {
		t.Errorf("tin vừa nhận: %d dòng customer, mong 1 (không nhân đôi)", n)
	}
	if n := e.count(`SELECT unread_count FROM conversation.conversations WHERE id = $1`, conv); n != 1 {
		t.Errorf("unread_count = %d, mong 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind LIKE 'command.%' AND kind <> $1`, history.FetchHistoryKind); n != 0 {
		t.Errorf("%d lệnh khác (gửi, tự động), mong 0", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE status IN ('dead', 'in_doubt')`); n != 0 {
		t.Errorf("%d việc dead/in_doubt", n)
	}
	if n := changed(e, conv); n != 2 {
		t.Errorf("%d báo hiệu hội thoại đổi, mong 2 (tin vào + một cho cả lô lịch sử)", n)
	}
}

// Hội thoại đã có từ trước nhận tin mới → không lệnh lịch sử nào thêm.
func TestFetchHistory_ExistingConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	e.receive("m_first", "chào shop", now.Add(-time.Minute))
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, history.FetchHistoryKind); n != 1 {
		t.Fatalf("%d lệnh lịch sử sau tin đầu, mong 1", n)
	}
	e.receive("m_second", "còn size M không", now)
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, history.FetchHistoryKind); n != 1 {
		t.Errorf("%d lệnh lịch sử sau tin thứ hai, mong vẫn 1", n)
	}
	if n := e.count(`SELECT unread_count FROM conversation.conversations WHERE id = $1`, e.conversationID()); n != 2 {
		t.Errorf("unread_count = %d, mong 2", n)
	}
}

// changed đếm báo hiệu conversation.changed của hội thoại id.
func changed(e *env, id uuid.UUID) int {
	n := 0
	for _, s := range e.bus.Signals() {
		if s.Topic == deps.TopicConversationChanged && s.Entity.ID == id.String() {
			n++
		}
	}
	return n
}
