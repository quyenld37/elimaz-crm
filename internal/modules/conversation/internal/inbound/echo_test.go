package inbound_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// fixtureUser là PSID khách trong các fixture Facebook.
const fixtureUser = "7000000000000011"

// CH14: echo từ Meta Business Suite (fixture thật, app_id 263902037430900) → 1 tin out page_external; chưa đọc không
// đổi; mốc tin nhân viên, tin cuối, đoạn trích cập nhật; giao lại → vẫn một tin.
func TestEchoFromBusinessSuite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	evs := fixtureEvents(t, "echo_business_suite.json")
	if len(evs) != 1 || evs[0].Type != channel.EventMessageEcho {
		t.Fatalf("fixture: %+v", evs)
	}
	e.receiveAt(fixtureUser, "mid-in-1", "còn hàng không", evs[0].OccurredAt.Add(-time.Minute))
	e.enqueueEvent(evs[0])
	e.drain()
	again := evs[0]
	again.DedupeKey += ":lai"
	e.enqueueEvent(again)
	e.drain()
	e.noDeadJobs()

	c := e.conversation(e.page1, fixtureUser)
	ms := e.messages(c.ID)
	if len(ms) != 2 {
		t.Fatalf("tin = %d, mong 2", len(ms))
	}
	out := ms[1]
	if out.Direction != "out" || out.Origin != "page_external" || deref(out.ExternalID) != "m_echo_bs_0001" ||
		deref(out.Body) != "Shop trả lời từ điện thoại" || out.SendStatus != nil {
		t.Fatalf("echo: %+v", out)
	}
	if c.Unread != 1 || c.LastStaffAt == nil || !c.LastStaffAt.Equal(out.SentAt) || !c.LastMessageAt.Equal(out.SentAt) ||
		deref(c.Snippet) != "Shop trả lời từ điện thoại" {
		t.Fatalf("hội thoại: %+v", c)
	}
	if len(e.changedSignals(c.ID)) < 2 {
		t.Error("echo không phát báo hiệu")
	}
}

// Echo tới trước mọi tin của khách (Page nhắn trước từ Business Suite): tạo khách, hội thoại; chưa đọc 0.
func TestEchoCreatesConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.deliver(channel.EventMessageEcho, "m:mid-e", "u-1", at(0), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-1", MID: "mid-e", Text: "shop chào chị", IsEcho: true, SentAt: at(0),
	})
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	if c.Unread != 0 || c.LastCustomerAt != nil || c.LastStaffAt == nil || len(e.messages(c.ID)) != 1 {
		t.Fatalf("hội thoại: %+v", c)
	}
}

// insertQueued chèn bằng chủ bảng một tin nhân viên đang chờ gửi (queued) mang client_ref ref vào hội thoại c.
func (e *env) insertQueued(c uuid.UUID, ref uuid.UUID, status string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			client_ref, direction, kind, origin, body, send_status, sent_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'out', 'message', 'staff', 'Dạ còn ạ', $4, $5)`,
			c, e.page1, ref, status, at(-10))
		return err //nolint:wrapcheck // test
	})
}

// Echo của CRM (app_id = META_APP_ID) có metadata = client_ref của tin queued, checking hay in_doubt → đúng tin đó
// thành sent có mã ngoài; không thêm dòng. Tin đã failed, expired, discarded cũng thành sent (sự thật thắng, Task 157):
// echo chứng minh Meta đã gửi.
func TestEchoOwnMatchesClientRef(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive(fixtureUser, "mid-in-1", "còn hàng không", -3600)
	c := e.conversation(e.page1, fixtureUser)
	// Fixture echo_crm.json mang metadata 0193a4b2-7c00-7000-8000-000000000001.
	ref := uuid.MustParse("0193a4b2-7c00-7000-8000-000000000001")
	e.insertQueued(c.ID, ref, "queued")
	evs := fixtureEvents(t, "echo_crm.json")
	if len(evs) != 1 {
		t.Fatalf("fixture: %+v", evs)
	}
	e.enqueueEvent(evs[0])
	e.drain()
	e.noDeadJobs()
	ms := e.messages(c.ID)
	if len(ms) != 2 {
		t.Fatalf("tin = %d, mong 2: %+v", len(ms), ms)
	}
	var own msg
	for _, m := range ms {
		if m.Origin == "staff" {
			own = m
		}
	}
	if deref(own.SendStatus) != "sent" || deref(own.ExternalID) != "m_echo_crm_0001" {
		t.Fatalf("tin của CRM: %+v", own)
	}
	if after := e.conversation(e.page1, fixtureUser); after.Unread != 1 {
		t.Errorf("chưa đọc = %d, mong 1", after.Unread)
	}

	for _, status := range []string{"checking", "in_doubt", "failed", "expired", "discarded"} {
		r := uuid.New()
		e.insertQueued(c.ID, r, status)
		mid := "mid-own-" + status
		e.deliver(channel.EventMessageEcho, "m:"+mid, fixtureUser, at(20), channel.MessagePayload{
			PageID: pageExt1, UserID: fixtureUser, MID: mid, Text: "Dạ còn ạ", IsEcho: true, EchoAppID: ownAppID,
			Metadata: r.String(), SentAt: at(20),
		})
		var gotStatus, unmatched int
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM conversation.messages WHERE client_ref = $1
				AND send_status = 'sent' AND external_id = $2`, r, mid).Scan(&gotStatus); err != nil {
				return err //nolint:wrapcheck // test
			}
			return tx.QueryRow(t.Context(), `SELECT count(*) FROM conversation.messages WHERE external_id = $1
				AND origin = 'own_echo_unmatched'`, mid).Scan(&unmatched)
		})
		wantSent, wantUnmatched := 1, 0
		if gotStatus != wantSent || unmatched != wantUnmatched {
			t.Errorf("%s: sent %d, own_echo_unmatched %d; mong %d, %d", status, gotStatus, unmatched, wantSent, wantUnmatched)
		}
	}
	e.noDeadJobs()
}

// Echo của CRM không metadata, chưa có tin nào mang mid đó → 1 dòng own_echo_unmatched; giao lại → vẫn 1.
func TestEchoOwnUnmatched(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p := channel.MessagePayload{PageID: pageExt1, UserID: "u-1", MID: "mid-own", Text: "Dạ", IsEcho: true, EchoAppID: ownAppID, SentAt: at(0)}
	e.deliver(channel.EventMessageEcho, "m:mid-own", "u-1", at(0), p)
	e.deliver(channel.EventMessageEcho, "m:mid-own:lai", "u-1", at(0), p)
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	ms := e.messages(c.ID)
	if len(ms) != 1 || ms[0].Origin != "own_echo_unmatched" || ms[0].Direction != "out" || c.Unread != 0 {
		t.Fatalf("tin: %+v, hội thoại %+v", ms, c)
	}
}
