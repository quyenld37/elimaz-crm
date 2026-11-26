package live_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// LV07: An và Bình cùng bấm một comment (hai yêu cầu song song) → đúng một thành công và một *CommentClaimedError có
// tên người thắng; 1 đơn; "có thể bán" giảm đúng 2.
func TestClaim_LV07_TwoStaffOneComment(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(10)
	c := e.comment(e.fbPage, e.customer("Lan"), "6100000000000021", "chốt", time.Now().Add(-time.Minute))
	in := newClaim(v, 2)
	in.AskAddress = false
	type out struct {
		res conversation.LiveClaimResult
		err error
		who string
	}
	results := make(chan out, 2)
	var wg sync.WaitGroup
	for _, u := range []struct {
		name string
		f    func() (conversation.LiveClaimResult, error)
	}{
		{"An", func() (conversation.LiveClaimResult, error) { return e.claim(e.an, c.id, in) }},
		{"Bình", func() (conversation.LiveClaimResult, error) { return e.claim(e.binh, c.id, in) }},
	} {
		wg.Go(func() {
			r, err := u.f()
			results <- out{r, err, u.name}
		})
	}
	wg.Wait()
	close(results)
	var winner, loserErr string
	var claimed *conversation.CommentClaimedError
	for r := range results {
		switch cc, ok := errors.AsType[*conversation.CommentClaimedError](r.err); {
		case r.err == nil:
			winner = r.who
		case ok:
			claimed, loserErr = cc, r.who
		default:
			t.Fatalf("%s: %v", r.who, r.err)
		}
	}
	if winner == "" || claimed == nil || claimed.ByName != winner || loserErr == winner {
		t.Fatalf("thắng %q, người sau thấy %+v", winner, claimed)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("có %d đơn, muốn 1", n)
	}
	if n := e.available(v); n != 8 {
		t.Errorf("có thể bán = %d, muốn 8", n)
	}
}

// DH08 trên đường Live: 30 comment của 30 khách cùng chốt 1 chiếc của biến thể còn 10, song song → đúng 10 đơn nháp,
// 20 lỗi out_of_stock, "có thể bán" = 0, không deadlock.
func TestClaim_LastUnits(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(10)
	comments := make([]liveComment, 30)
	for i := range comments {
		comments[i] = e.comment(e.fbPage, e.customer(fmt.Sprintf("Khách %02d", i)), fmt.Sprintf("61000000000001%02d", i), "chốt",
			time.Now().Add(-time.Minute))
	}
	in := newClaim(v, 1)
	in.AskAddress = false
	dl := e.deadlocks()
	errs := make([]error, len(comments))
	var wg sync.WaitGroup
	for i, c := range comments {
		wg.Go(func() { _, errs[i] = e.claim(e.an, c.id, in) })
	}
	wg.Wait()
	ok, oos := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, order.ErrOutOfStock):
			oos++
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 10 || oos != 20 || e.available(v) != 0 {
		t.Fatalf("thành công %d, hết hàng %d, có thể bán %d; muốn 10, 20, 0", ok, oos, e.available(v))
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE status = 'draft'`); n != 10 {
		t.Errorf("đơn nháp = %d", n)
	}
	if d := e.deadlocks(); d != dl {
		t.Errorf("deadlock tăng %d → %d", dl, d)
	}
}

// C11 (ADR-46, khách trước hội thoại): 200 vòng, mỗi vòng chốt một comment của khách K song song với một tin vào hộp
// thư và một comment live mới của cùng K (worker thật, kênh giả) → không deadlock, không việc dead, không yêu cầu nào lỗi
// (hết lượt thử lại của RetryTx là lỗi).
func TestClaim_C11_CustomerBeforeConversation(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	const rounds = 200
	v := e.variant(rounds + 10)
	const user = "uK"
	e.inboundEvent(e.messageEvent(user, "m-0"))
	e.inboundEvent(e.commentEvent(user, "c-0"))
	testkit.DrainDue(t, e.staff.Owner, e.inboundOnly)
	in := newClaim(v, 1)
	in.AskAddress, in.ForceNew = false, true
	dl := e.deadlocks()
	for i := range rounds {
		c := e.commentByExt(fmt.Sprintf("c-%d", i))
		var wg sync.WaitGroup
		var claimErr error
		wg.Go(func() { _, claimErr = e.claim(e.an, c, in) })
		wg.Go(func() {
			e.inboundEvent(e.messageEvent(user, fmt.Sprintf("m-%d", i+1)))
			e.inboundEvent(e.commentEvent(user, fmt.Sprintf("c-%d", i+1)))
			testkit.DrainDue(t, e.staff.Owner, e.inboundOnly)
		})
		wg.Wait()
		if claimErr != nil {
			t.Fatalf("vòng %d: %v", i, claimErr)
		}
	}
	if d := e.deadlocks(); d != dl {
		t.Errorf("deadlock tăng %d → %d", dl, d)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE status = 'dead'`); n != 0 {
		t.Errorf("%d việc dead", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims`); n != rounds {
		t.Errorf("ghi nhận = %d, muốn %d", n, rounds)
	}
	if n := e.count(`SELECT count(DISTINCT customer_id) FROM conversation.conversations WHERE channel_account_id = $1`, e.fakePage); n != 1 {
		t.Errorf("khách của hội thoại = %d, muốn 1", n)
	}
}

// messageEvent là tin vào hộp thư của người user trên Page kênh giả, mã mid.
func (e *claimEnv) messageEvent(user, mid string) webhooks.InboundEvent {
	e.t.Helper()
	at := time.Now().UTC()
	body, err := json.Marshal(channel.MessagePayload{PageID: fakePageEx, UserID: user, MID: mid, Text: "còn hàng không", SentAt: at})
	if err != nil {
		e.t.Fatal(err)
	}
	return webhooks.InboundEvent{
		Type: channel.EventMessageReceived, DedupeKey: "m:" + mid, OrderKey: fake.Key + ":" + fakePageEx + ":" + user,
		OccurredAt: at, Payload: body,
	}
}

// commentEvent là comment live (bài liveVideo) của người user trên Page kênh giả, mã id.
func (e *claimEnv) commentEvent(user, id string) webhooks.InboundEvent {
	e.t.Helper()
	at := time.Now().UTC()
	body, err := json.Marshal(channel.CommentPayload{
		PageID: fakePageEx, PostID: liveVideo, CommentID: id, FromID: user, FromName: "Khách K", Text: "chốt", Verb: "add", CreatedAt: at,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return webhooks.InboundEvent{Type: channel.EventCommentChanged, DedupeKey: "c:" + id, OrderKey: "post:" + liveVideo, OccurredAt: at, Payload: body}
}

// inboundEvent ghi việc vào của kênh giả cho ev (chưa chạy).
func (e *claimEnv) inboundEvent(ev webhooks.InboundEvent) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		created, err := webhooks.EnqueueInbound(e.t.Context(), tx, e.d.Queue, e.tenant, fake.Key, ev, time.Now().UTC(), webhooks.ViaWebhook)
		if err == nil && !created {
			e.t.Errorf("việc vào %s trùng", ev.DedupeKey)
		}
		return err //nolint:wrapcheck // test
	})
}

// commentByExt trả mã tin của comment mã ngoài ext trên Page kênh giả (đã là comment live).
func (e *claimEnv) commentByExt(ext string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM conversation.messages WHERE channel_account_id = $1
			AND external_id = $2 AND is_live`, e.fakePage, ext).Scan(&id)
	})
	return id
}

// C11, ca chắc chắn (lỗi ci-task lần 1, 4 deadlock trên máy từ xa): đường tin vào đã khóa hộp thư I (upsert) rồi sửa lại
// chính dòng đó (BumpConversationInbound) — PostgreSQL kiểm lại khóa ngoại customer_id (FOR KEY SHARE trên dòng khách)
// vì dòng do chính transaction đó vừa ghi. Lệnh chốt chen giữa: khóa khách K rồi chờ I. Khóa khách của lệnh chốt không
// được chặn FOR KEY SHARE: tin vào xong, lệnh chốt xong, không deadlock.
func TestClaim_C11_InboundFKRecheckWhileClaimWaits(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(5)
	k := e.customer("Lan")
	c := e.comment(e.fakePage, k, "uFK", "chốt", time.Now().Add(-time.Minute))
	box := e.inbox(e.fakePage, k, "uFK", time.Now().Add(-time.Hour))
	ctx := t.Context()
	dl := e.deadlocks()
	inbound, err := e.staff.App.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inbound.Rollback(ctx) //nolint:errcheck // đã commit ở dưới khi đúng
	if _, err := inbound.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, e.tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := inbound.Exec(ctx, `UPDATE conversation.conversations SET updated_at = now() WHERE id = $1`, box); err != nil {
		t.Fatal(err)
	}
	in := newClaim(v, 1)
	in.AskAddress = false
	var claimErr error
	var wg sync.WaitGroup
	wg.Go(func() { _, claimErr = e.claim(e.an, c.id, in) })
	eventually(t, 10*time.Second, "lệnh chốt chờ khóa hộp thư", func() bool {
		return e.staff.Count(t, `SELECT count(*)::int FROM pg_stat_activity WHERE datname = current_database()
			AND wait_event_type = 'Lock' AND query LIKE '%LockConversationsByID%'`) == 1
	})
	if _, err := inbound.Exec(ctx, `UPDATE conversation.conversations SET last_message_at = now() WHERE id = $1`, box); err != nil {
		t.Fatalf("tin vào sửa lại hộp thư: %v", err)
	}
	if err := inbound.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if claimErr != nil {
		t.Fatalf("chốt: %v", claimErr)
	}
	if d := e.deadlocks(); d != dl {
		t.Errorf("deadlock tăng %d → %d", dl, d)
	}
}

// ADR-46 trong bảng khách (review Task 172): khách của comment là hồ sơ con K đã gộp vào gốc R, K < R. Một giao dịch
// khác khóa như tạo đơn cho K (lockPair: K rồi R, FOR UPDATE) chen giữa lệnh chốt → lệnh chốt chờ ở K (không giữ R),
// không deadlock; đơn nháp gắn K, cả hai xong.
func TestClaim_MergedCustomerLockOrder(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(5)
	child := e.customer("Lan (cũ)")
	root := e.customer("Lan")
	e.exec(`UPDATE customer.customers SET root_id = $2 WHERE id = $1`, child, root)
	c := e.comment(e.fakePage, child, "uMerged", "chốt", time.Now().Add(-time.Minute))
	ctx := t.Context()
	dl := e.deadlocks()
	other, err := e.staff.App.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback(ctx) //nolint:errcheck // đã commit ở dưới khi đúng
	if _, err := other.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, e.tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec(ctx, `SELECT 1 FROM customer.customers WHERE id = $1 FOR UPDATE`, child); err != nil {
		t.Fatal(err)
	}
	in := newClaim(v, 1)
	in.AskAddress = false
	var res conversation.LiveClaimResult
	var claimErr error
	var wg sync.WaitGroup
	wg.Go(func() { res, claimErr = e.claim(e.an, c.id, in) })
	eventually(t, 10*time.Second, "lệnh chốt chờ khóa khách", func() bool {
		return e.staff.Count(t, `SELECT count(*)::int FROM pg_stat_activity WHERE datname = current_database()
			AND wait_event_type = 'Lock'`) == 1
	})
	if _, err := other.Exec(ctx, `SELECT 1 FROM customer.customers WHERE id = $1 FOR UPDATE`, root); err != nil {
		t.Fatalf("giao dịch khác khóa gốc: %v", err)
	}
	if err := other.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if claimErr != nil || res.Order.CustomerID != child {
		t.Fatalf("chốt: %v, khách của đơn %s", claimErr, res.Order.CustomerID)
	}
	if d := e.deadlocks(); d != dl {
		t.Errorf("deadlock tăng %d → %d", dl, d)
	}
}
