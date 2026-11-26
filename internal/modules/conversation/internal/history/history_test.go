package history_test

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
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
	// Lệnh chỉ đọc của Task 155 (hồ sơ khách mới) không tính: chỉ đòi không có lệnh gửi, tự động.
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind LIKE 'command.%' AND kind NOT IN ($1, $2)`,
		history.FetchHistoryKind, "command.fb.fetch_profile"); n != 0 {
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

// Tin lịch sử có đính kèm (Task 496; phát hiện của 164): OnDone của lệnh lịch sử ghi mục pending kèm URL và xếp đúng
// lệnh tải tệp của Task 155 (payload mã tin + chỉ số, mỗi mục có URL một lệnh; mục không URL không lệnh), như tin vào;
// worker chạy lệnh tải → mục rời pending (máy lạ → blocked), URL bị xóa khỏi mục. Tin đã có (cùng mã ngoài) không
// xếp thêm lệnh.
func TestFetchHistory_AttachmentsDownloaded(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	e.receive("m_new", "chị hỏi mẫu này", now)
	conv := e.conversationID()
	done := e.historyDone()
	result := `{"messages":[
		{"mid":"m_h1","text":"ảnh mẫu","attachments":[{"Type":"image","URL":"https://evil.example/a.jpg"},
			{"Type":"sticker","StickerID":"369239263222822"}],"sentAt":"` + now.Add(-time.Hour).Format(time.RFC3339) + `"},
		{"mid":"m_new","text":"chị hỏi mẫu này","attachments":[{"Type":"image","URL":"https://evil.example/b.jpg"}],
			"sentAt":"` + now.Format(time.RFC3339) + `"}]}`
	tenant := e.tenant
	job := queue.Job{
		ID: uuid.New(), TenantID: &tenant, Kind: history.FetchHistoryKind,
		Payload: json.RawMessage(`{"conversationId":"` + conv.String() + `"}`),
	}
	if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		return done.OnDone(t.Context(), job, json.RawMessage(result), tx)
	}); err != nil {
		t.Fatalf("OnDone: %v", err)
	}
	var msg uuid.UUID
	var raw []byte
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id, attachments FROM conversation.messages WHERE external_id = 'm_h1'
			AND origin = 'history'`).Scan(&msg, &raw)
	})
	its := attachments(t, raw)
	if len(its) != 2 || its[0].Status != "pending" || its[0].URL == "" || its[1].Status != "pending" || its[1].URL != "" {
		t.Fatalf("mục của tin lịch sử: %+v, muốn hai mục pending, mục ảnh có URL", its)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'command.fb.fetch_attachment'`); n != 1 {
		t.Fatalf("%d lệnh tải tệp, muốn 1 (một mục có URL của tin lịch sử mới; tin đã có không thêm)", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'command.fb.fetch_attachment'
		AND payload = jsonb_build_object('messageId', $1::text, 'index', 0) AND dedupe_key = $1::text || ':0'`, msg); n != 1 {
		t.Fatalf("lệnh tải không đúng payload, khóa chống trùng của tin %s mục 0", msg)
	}
	testkit.DrainDue(t, e.staff.Owner, e.worker)
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT attachments FROM conversation.messages WHERE id = $1`, msg).Scan(&raw)
	})
	if its := attachments(t, raw); its[0].Status != "blocked" || its[0].URL != "" || its[1].Status != "pending" {
		t.Fatalf("sau lệnh tải: %+v, muốn mục ảnh blocked không URL, sticker giữ pending", its)
	}
}

// item là một mục đính kèm trong cột messages.attachments.
type item struct {
	Status string `json:"status"`
	URL    string `json:"url"`
}

func attachments(t *testing.T, raw []byte) []item {
	t.Helper()
	var its []item
	if err := json.Unmarshal(raw, &its); err != nil {
		t.Fatalf("đính kèm %s: %v", raw, err)
	}
	return its
}

// historyDone trả lệnh đọc lịch sử (phần OnDone) của history.Parts.
func (e *env) historyDone() commands.DoneHook { //nolint:ireturn // hook của commands là interface
	e.t.Helper()
	for _, c := range history.Parts(e.d).Commands {
		if h, ok := c.(commands.DoneHook); ok && c.Kind().Name == history.FetchHistoryKind {
			return h
		}
	}
	e.t.Fatal("không có lệnh đọc lịch sử")
	return nil
}
