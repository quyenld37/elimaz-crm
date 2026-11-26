package live_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// askRow đọc tin xin địa chỉ id: loại, trạng thái gửi, lý do, hội thoại.
func (e *claimEnv) askRow(id uuid.UUID) (kind, status, reason string, conv uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT kind, coalesce(send_status, ''), coalesce(fail_reason, ''), conversation_id
			FROM conversation.messages WHERE id = $1`, id).Scan(&kind, &status, &reason, &conv)
	})
	return kind, status, reason, conv
}

// Xin địa chỉ (Q29): Meta từ chối nhắn riêng → tin riêng failed có lý do (đơn, ghi nhận vẫn còn); comment quá 7 ngày mà
// hộp thư còn cửa sổ → tin thường queued trong hộp thư; cả hai không được → skipped_policy câu tiếng Việt, đơn vẫn tạo;
// Page chỉ xem → skipped_policy lý do chỉ xem; không bấm xin địa chỉ hay Page chưa có mẫu → not_requested.
func TestClaim_AskAddress(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(20)
	now := time.Now()

	fresh := e.comment(e.fbPage, e.customer("Lan"), "6100000000000011", "chốt", now.Add(-time.Minute))
	e.fg.FailNext(claimPageExt+"/messages", fakegraph.GraphErrorSpec{Code: 10900, Type: "OAuthException", Message: "Activity already replied to"})
	res := e.mustClaim(fresh, newClaim(v, 1))
	if res.Ask.Status != "queued" || res.Ask.MessageID == nil {
		t.Fatalf("ask = %+v", res.Ask)
	}
	e.drain()
	if kind, status, reason, _ := e.askRow(*res.Ask.MessageID); kind != "private_reply" || status != "failed" || reason == "" {
		t.Errorf("tin riêng bị từ chối = %s %s %q", kind, status, reason)
	}

	cust := e.customer("Hoa")
	old := e.comment(e.fbPage, cust, "6100000000000012", "chốt", now.Add(-8*24*time.Hour))
	box := e.inbox(e.fbPage, cust, "6100000000000012", now.Add(-time.Hour))
	res = e.mustClaim(old, newClaim(v, 1))
	if kind, status, _, conv := e.askRow(*res.Ask.MessageID); res.Ask.Status != "queued" || kind != "message" || status != "queued" || conv != box {
		t.Errorf("tin thường qua hộp thư = %+v %s %s %s", res.Ask, kind, status, conv)
	}

	cust = e.customer("Mai")
	none := e.comment(e.fbPage, cust, "6100000000000013", "chốt", now.Add(-8*24*time.Hour))
	e.inbox(e.fbPage, cust, "6100000000000013", now.Add(-48*time.Hour))
	res = e.mustClaim(none, newClaim(v, 1))
	if res.Ask.Status != "skipped_policy" || !strings.Contains(res.Ask.Reason, "7 ngày") || !strings.Contains(res.Ask.Reason, "24 giờ") ||
		res.Ask.MessageID != nil {
		t.Errorf("không gửi được = %+v", res.Ask)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims WHERE message_id = $1 AND ask_status = 'skipped_policy'
		AND ask_reason = $2`, none.id, res.Ask.Reason); n != 1 {
		t.Errorf("ghi nhận bỏ xin địa chỉ = %d", n)
	}

	noInbox := e.comment(e.fbPage, e.customer("Thu"), "6100000000000014", "chốt", now.Add(-8*24*time.Hour))
	if res = e.mustClaim(noInbox, newClaim(v, 1)); res.Ask.Status != "skipped_policy" || !strings.Contains(res.Ask.Reason, "hộp thư") {
		t.Errorf("chưa có hộp thư = %+v", res.Ask)
	}

	quiet := newClaim(v, 1)
	quiet.AskAddress = false
	if res = e.mustClaim(e.comment(e.fbPage, e.customer("Na"), "6100000000000015", "chốt", now), quiet); res.Ask.Status != conversation.LiveAskNotRequested {
		t.Errorf("không xin địa chỉ = %+v", res.Ask)
	}
	e.exec(`UPDATE conversation.page_settings SET ask_address_template_id = NULL WHERE channel_account_id = $1`, e.fbPage)
	if res = e.mustClaim(e.comment(e.fbPage, e.customer("Ly"), "6100000000000016", "chốt", now), newClaim(v, 1)); res.Ask.Status != conversation.LiveAskNotRequested ||
		res.Ask.Reason == "" {
		t.Errorf("Page chưa có mẫu = %+v", res.Ask)
	}
	chosen := newClaim(v, 1)
	chosen.TemplateID = &e.tpl
	if res = e.mustClaim(e.comment(e.fbPage, e.customer("Vy"), "6100000000000017", "chốt", now), chosen); res.Ask.Status != "queued" {
		t.Errorf("mẫu chọn tay = %+v", res.Ask)
	}

	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fbPage)
	res = e.mustClaim(e.comment(e.fbPage, e.customer("Yến"), "6100000000000018", "chốt", now), chosen)
	if res.Order.Status != "draft" || res.Ask.Status != "skipped_policy" || !strings.Contains(res.Ask.Reason, "chỉ xem") {
		t.Errorf("Page chỉ xem = %+v %+v", res.Order.Status, res.Ask)
	}
}

// mustClaim chốt comment c bằng An, lỗi thì dừng.
func (e *claimEnv) mustClaim(c liveComment, in conversation.LiveClaimInput) conversation.LiveClaimResult {
	e.t.Helper()
	res, err := e.claim(e.an, c.id, in)
	if err != nil {
		e.t.Fatalf("chốt %s: %v", c.ext, err)
	}
	return res
}
