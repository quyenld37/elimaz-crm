package inbound_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// enqueueVia ghi một sự kiện facebook của Page 1 tới qua đường via rồi chạy worker tới hết việc tới hạn.
func (e *env) enqueueVia(via, typ, dedupe, user string, payload any) {
	e.t.Helper()
	e.enqueueFrom(facebook.Key, via, typ, dedupe, pageExt1, user, at(0), payload)
	e.drain()
}

// marks đọc mốc last_webhook_at, last_feed_at của Page 1 (nil = chưa có).
func (e *env) marks() (webhook, feed *time.Time) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT last_webhook_at, last_feed_at FROM channel.channel_accounts WHERE id = $1`,
			e.page1).Scan(&webhook, &feed)
	})
	return webhook, feed
}

// Gộp 150 + 153/154: chỉ sự kiện tới qua webhook ghi mốc last_webhook_at (hỏi bù, đọc comment không che "im lặng bất
// thường"); comment qua webhook ghi cả last_feed_at (chế độ đọc comment auto), tin nhắn thì không.
func TestInbound_OnlyWebhookNotesInbound(t *testing.T) {
	t.Parallel()
	fg := fakegraphFor(t)
	fg.AddPost(fakePost(post1, "Áo thun"))
	e := newEnvWith(t, fg)
	e.enqueueVia(webhooks.ViaCommentPoll, channel.EventCommentChanged, "c:p1:add", post1+":u-1",
		commentEv("add", post1, "p1", "", "u-1", "Lan", "Còn size M?", 0))
	e.enqueueVia(webhooks.ViaBackfill, channel.EventMessageReceived, "m:b1", "u-2", message("u-2", "b1", "Chào shop", 1))
	if w, f := e.marks(); w != nil || f != nil {
		t.Fatalf("sự kiện hỏi bù, đọc comment đã ghi mốc: webhook %v, feed %v", w, f)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 2 {
		t.Fatalf("sự kiện hỏi bù, đọc comment vẫn phải lưu tin: %d", n)
	}
	e.enqueueVia(webhooks.ViaWebhook, channel.EventMessageReceived, "m:w1", "u-2", message("u-2", "w1", "Shop ơi", 2))
	if w, f := e.marks(); w == nil || f != nil {
		t.Fatalf("tin qua webhook: webhook %v, feed %v; muốn có webhook, chưa có feed", w, f)
	}
	e.enqueueVia(webhooks.ViaWebhook, channel.EventCommentChanged, "c:w2:add", post1+":u-3",
		commentEv("add", post1, "w2", "", "u-3", "Hoa", "Giá bao nhiêu?", 3))
	if _, f := e.marks(); f == nil {
		t.Fatal("comment qua webhook chưa ghi last_feed_at")
	}
}

// Gộp 150 + 153: handler vào nguồn facebook chuyển sự kiện policy.enforcement cho HandlePolicy (Page → degraded
// meta_policy:<hành động>), không rơi vào inbound_ignored.
func TestInbound_PolicyEnforcementRouted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueueVia(webhooks.ViaWebhook, channel.EventPolicyEnforcement, "p:"+pageExt1+":1", pageExt1,
		channel.PolicyPayload{PageID: pageExt1, Action: "block", Reason: "x"})
	var status, reason string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, coalesce(status_reason, '') FROM channel.channel_accounts WHERE id = $1`,
			e.page1).Scan(&status, &reason)
	})
	if status != channel.StatusDegraded || reason != "meta_policy:block" {
		t.Fatalf("trạng thái = %s %s; muốn degraded meta_policy:block", status, reason)
	}
}
