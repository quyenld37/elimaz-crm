package live_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// LV03: biến thể còn 5, chốt 2 → 1 đơn draft của khách, kênh facebook, hội thoại comment, Page, nguồn "Live ca trưa"
// (phiên 12:15), hạn nháp now + 24 giờ; "có thể bán" còn 3; 1 dòng ghi nhận; 1 tin riêng queued "Chào Lan, …" có mã
// đơn; chạy worker → Meta nhận 1 nhắn riêng recipient.comment_id đúng; báo hiệu live.comments và conversation.changed.
func TestClaim_LV03(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(5)
	lan := e.customer("Lan")
	c := e.comment(e.fbPage, lan, "6100000000000001", "chốt áo đỏ size S", time.Now().Add(-time.Minute))
	before := time.Now().UTC()
	res, err := e.claim(e.an, c.id, newClaim(v, 2))
	if err != nil {
		t.Fatal(err)
	}
	o := res.Order
	if o.Status != order.StatusDraft || o.CustomerID != lan || o.Channel != order.ChannelFacebook ||
		o.ConversationID == nil || *o.ConversationID != c.conv || o.ChannelAccountID == nil || *o.ChannelAccountID != e.fbPage ||
		o.TrafficSourceID == nil || *o.TrafficSourceID != e.srcNoon || o.ExternalRef == nil || *o.ExternalRef != "fb_comment:"+c.ext {
		t.Fatalf("đơn = %+v", o)
	}
	if o.DraftExpiresAt == nil || o.DraftExpiresAt.Sub(before) < 24*time.Hour-time.Minute || o.DraftExpiresAt.Sub(before) > 24*time.Hour+time.Minute {
		t.Errorf("hạn nháp = %v, muốn now + 24 giờ", o.DraftExpiresAt)
	}
	if n := e.available(v); n != 3 {
		t.Errorf("có thể bán = %d, muốn 3", n)
	}
	if res.Claim.ByName != "An" || res.Claim.OrderCode != o.Code || res.Ask.Status != "queued" || res.Ask.MessageID == nil {
		t.Fatalf("ghi nhận = %+v, ask = %+v", res.Claim, res.Ask)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims WHERE message_id = $1 AND order_id = $2
		AND live_session_id = $3 AND ask_status = 'queued' AND ask_message_id = $4`, c.id, o.ID, e.fbSession, *res.Ask.MessageID); n != 1 {
		t.Errorf("dòng ghi nhận = %d", n)
	}
	var body, kind, status string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT body, kind, send_status FROM conversation.messages WHERE id = $1`,
			*res.Ask.MessageID).Scan(&body, &kind, &status)
	})
	if kind != "private_reply" || status != "queued" || !strings.HasPrefix(body, "Chào Lan, ") || !strings.Contains(body, o.Code) {
		t.Fatalf("tin xin địa chỉ = %s %s %q", kind, status, body)
	}
	e.drain()
	if n := e.privateCalls(c.ext); n != 1 {
		t.Errorf("Meta nhận %d nhắn riêng, muốn 1", n)
	}
	if !e.signaled("live.comments", e.fbSession.String()) || !e.signaled("conversation.changed", c.conv.String()) {
		t.Errorf("thiếu báo hiệu: %+v", e.bus.Signals())
	}
}

// LV08: biến thể còn 0 → out_of_stock (biến thể đó, có thể bán 0), không đơn, không ghi nhận, không tin, không việc;
// hai dòng, một dòng hết → cả yêu cầu bị từ chối, dòng kia không bị giữ; biến thể ngừng bán → ErrNotForSale.
func TestClaim_LV08(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	empty, ok := e.variant(0), e.variant(4)
	c := e.comment(e.fbPage, e.customer("Mai"), "6100000000000002", "chốt", time.Now().Add(-time.Minute))
	jobs := e.count(`SELECT count(*) FROM platform.jobs`)
	for _, in := range []conversation.LiveClaimInput{newClaim(empty, 1), newClaim(ok, 2, empty, 1)} {
		_, err := e.claim(e.an, c.id, in)
		oos, isOOS := errors.AsType[*order.OutOfStockError](err)
		if !isOOS || !errors.Is(err, order.ErrOutOfStock) || oos.VariantID != empty || oos.Available != 0 {
			t.Fatalf("hết hàng: %v", err)
		}
	}
	if n := e.available(ok); n != 4 {
		t.Errorf("dòng còn hàng bị giữ: có thể bán %d, muốn 4", n)
	}
	e.assertNothing(c, jobs)
	stopped := e.variant(3)
	e.exec(`UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1`, stopped)
	if _, err := e.claim(e.an, c.id, newClaim(stopped, 1)); !errors.Is(err, order.ErrNotForSale) {
		t.Fatalf("ngừng bán: %v", err)
	}
	e.assertNothing(c, jobs)
}

// assertNothing: comment c không có đơn, ghi nhận, tin ra; số việc vẫn là jobs.
func (e *claimEnv) assertNothing(c liveComment, jobs int) {
	e.t.Helper()
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		e.t.Errorf("có %d đơn", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims`); n != 0 {
		e.t.Errorf("có %d ghi nhận", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE direction = 'out'`); n != 0 {
		e.t.Errorf("có %d tin ra", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs`); n != jobs {
		e.t.Errorf("việc: %d, muốn %d", n, jobs)
	}
	_ = c
}

// LV09: khách đã có đơn nháp từ phiên → comment thứ hai mode new → open_draft_exists kèm đơn đó (dòng, version);
// add_to_draft với version đúng → cùng đơn, thêm dòng (trùng biến thể cộng số lượng), "có thể bán" giảm thêm; version cũ
// → ErrVersionConflict; forceNew → đơn nháp thứ hai. Comment đã chốt → comment_claimed.
func TestClaim_LV09(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v1, v2 := e.variant(10), e.variant(10)
	k := e.customer("Hoa")
	c1 := e.comment(e.fbPage, k, "6100000000000003", "chốt 1", time.Now().Add(-2*time.Minute))
	c2 := e.comment(e.fbPage, k, "6100000000000003", "chốt thêm", time.Now().Add(-time.Minute))
	first, err := e.claim(e.an, c1.id, newClaim(v1, 1))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.claim(e.an, c2.id, newClaim(v2, 1))
	od, ok := errors.AsType[*conversation.OpenDraftExistsError](err)
	if !ok || len(od.Drafts) != 1 || od.Drafts[0].OrderID != first.Order.ID || od.Drafts[0].Version != first.Order.Version ||
		len(od.Drafts[0].Items) != 1 || od.Drafts[0].Items[0].VariantID != v1 || od.Drafts[0].Items[0].Qty != 1 {
		t.Fatalf("open_draft_exists: %v %+v", err, od)
	}
	add := conversation.LiveClaimInput{
		Mode: conversation.LiveClaimAddToDraft, OrderID: &first.Order.ID, ExpectedVersion: first.Order.Version,
		Items: []conversation.LiveClaimItem{{VariantID: v2, Qty: 2}, {VariantID: v1, Qty: 1}},
	}
	got, err := e.claim(e.an, c2.id, add)
	if err != nil {
		t.Fatal(err)
	}
	if got.Order.ID != first.Order.ID || len(got.Order.Items) != 2 || got.Order.Version != first.Order.Version+1 {
		t.Fatalf("thêm vào nháp: %+v", got.Order)
	}
	if e.available(v1) != 8 || e.available(v2) != 8 {
		t.Errorf("có thể bán v1 %d, v2 %d; muốn 8, 8", e.available(v1), e.available(v2))
	}
	if got.Ask.Status != conversation.LiveAskNotRequested {
		t.Errorf("ask = %+v", got.Ask)
	}
	c3 := e.comment(e.fbPage, k, "6100000000000003", "chốt nữa", time.Now())
	add.ExpectedVersion = first.Order.Version
	if _, err := e.claim(e.an, c3.id, add); !errors.Is(err, order.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	forced := newClaim(v1, 1)
	forced.ForceNew = true
	second, err := e.claim(e.an, c3.id, forced)
	if err != nil || second.Order.ID == first.Order.ID || second.Order.Status != order.StatusDraft {
		t.Fatalf("forceNew: %v %+v", err, second.Order)
	}
	_, err = e.claim(e.binh, c1.id, forced)
	cc, ok := errors.AsType[*conversation.CommentClaimedError](err)
	if !ok || cc.ByName != "An" || cc.OrderID != first.Order.ID || cc.OrderCode != first.Order.Code {
		t.Fatalf("đã chốt: %v", err)
	}
}

// ADR-29: Page của người bán B (partner), biến thể của người bán A → ErrSellerMismatch, không đơn. Page khác, comment
// không phải live, người không có live.operate trên Page → ErrNotFound. Yêu cầu sai → ErrClaimInvalid.
func TestClaim_SellerAndAccess(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(5)
	c := e.comment(e.fbPage, e.customer("Lan"), "6100000000000004", "chốt", time.Now().Add(-time.Minute))
	e.exec(`UPDATE channel.channel_accounts SET seller_id = $2 WHERE id = $1`, e.fbPage, e.shop.Partner)
	if _, err := e.claim(e.an, c.id, newClaim(v, 1)); !errors.Is(err, conversation.ErrSellerMismatch) {
		t.Fatalf("khác người bán: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("có %d đơn", n)
	}
	e.exec(`UPDATE channel.channel_accounts SET seller_id = $2 WHERE id = $1`, e.fbPage, e.shop.Seller)
	other := identity.AuthUser{UserID: e.an.UserID, TenantID: e.tenant, Grants: []identity.Grant{
		{Permission: "live.operate", Scope: identity.Scope{Type: identity.ScopePage, ID: e.fakePage.String()}},
	}}
	if _, err := e.claim(other, c.id, newClaim(v, 1)); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("Page khác: %v", err)
	}
	if _, err := e.claim(e.an, uuid.New(), newClaim(v, 1)); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("comment lạ: %v", err)
	}
	e.exec(`UPDATE conversation.messages SET is_live = false WHERE id = $1`, c.id)
	if _, err := e.claim(e.an, c.id, newClaim(v, 1)); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("không phải comment live: %v", err)
	}
	e.exec(`UPDATE conversation.messages SET is_live = true WHERE id = $1`, c.id)
	for _, in := range []conversation.LiveClaimInput{
		{Mode: "new"},
		newClaim(v, 21), newClaim(v, 0),
		{Mode: "lạ", Items: newClaim(v, 1).Items},
		{Mode: conversation.LiveClaimAddToDraft, Items: newClaim(v, 1).Items},
	} {
		if _, err := e.claim(e.an, c.id, in); !errors.Is(err, conversation.ErrClaimInvalid) {
			t.Errorf("yêu cầu sai %+v: %v", in, err)
		}
	}
	if res, err := e.claim(e.an, c.id, newClaim(v, 1)); err != nil || res.Order.SellerID != e.shop.Seller {
		t.Fatalf("đúng người bán: %v", err)
	}
}

// signaled cho biết bus đã nhận báo hiệu topic của thực thể id.
func (e *claimEnv) signaled(topic, id string) bool {
	for _, s := range e.bus.Signals() {
		if s.Topic == topic && s.Entity.ID == id {
			return true
		}
	}
	return false
}
