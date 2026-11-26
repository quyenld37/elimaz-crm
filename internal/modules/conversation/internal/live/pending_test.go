package live_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Danh sách "đã chốt, thiếu địa chỉ" và nhắc một chạm (Task 173; LV04, LV05, LV06).

// pendingCust là một khách đã chốt trong phiên: comment, mã người (PSID), hộp thư, đơn.
type pendingCust struct {
	c     liveComment
	psid  string
	inbox uuid.UUID
	order order.Order
}

// chốt: khách name (mã người psid) comment lúc now − 10 phút, hộp thư có tin cuối của khách lúc lastIn, chốt 1 biến
// thể v có xin địa chỉ (comment mới: nhắn riêng).
func (e *claimEnv) claimed(name, psid string, v uuid.UUID, lastIn, now time.Time) pendingCust {
	e.t.Helper()
	cust := e.customer(name)
	c := e.comment(e.fbPage, cust, psid, "chốt", now.Add(-10*time.Minute))
	box := e.inbox(e.fbPage, cust, psid, lastIn)
	res := e.mustClaim(c, newClaim(v, 1))
	if res.Ask.Status != "queued" {
		e.t.Fatalf("chốt %s: ask = %+v", name, res.Ask)
	}
	return pendingCust{c: c, psid: psid, inbox: box, order: res.Order}
}

// fiveClaims dựng phiên của LV05: 5 khách (mã người psid + "01".."05") chốt mỗi người 1 biến thể v có xin địa chỉ (nhắn
// riêng lúc chốt, worker đã gửi); hộp thư của khách thứ 3 có tin cuối 48 giờ trước, các khách khác 30 phút trước.
func (e *claimEnv) fiveClaims(v uuid.UUID, now time.Time, psid string) (a, b, c, d, x pendingCust) {
	e.t.Helper()
	e.staff.SeedAdminUnits(e.t)
	open := now.Add(-30 * time.Minute)
	a = e.claimed("An Nhiên", psid+"01", v, open, now)
	b = e.claimed("Bảo", psid+"02", v, open, now)
	c = e.claimed("Chi", psid+"03", v, now.Add(-48*time.Hour), now)
	d = e.claimed("Dung", psid+"04", v, open, now)
	x = e.claimed("Xuân", psid+"05", v, open, now)
	e.drain()
	return a, b, c, d, x
}

// pending đọc danh sách đơn nháp của phiên Facebook bằng u lúc now.
func (e *claimEnv) pending(u identity.AuthUser, now time.Time) (conversation.PendingDraftList, error) {
	var out conversation.PendingDraftList
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = conversation.ListPendingDrafts(e.t.Context(), tx, e.sendEnv(), u, e.fbSession, now)
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

func (e *claimEnv) mustPending(now time.Time) conversation.PendingDraftList {
	e.t.Helper()
	l, err := e.pending(e.an, now)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

// remind nhắc các đơn ids (rỗng: mọi nháp) của phiên session bằng u lúc now, mẫu e.tpl.
func (e *claimEnv) remind(u identity.AuthUser, session uuid.UUID, now time.Time, ids ...uuid.UUID,
) ([]conversation.RemindOutcome, error) {
	var out []conversation.RemindOutcome
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = conversation.RemindPendingDrafts(e.t.Context(), tx, e.sendEnv(), u, session,
			conversation.RemindInput{OrderIDs: ids, TemplateID: e.tpl}, now)
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

// customerSays chèn tin của khách vào hộp thư box lúc at (như đường vào ghi).
func (e *claimEnv) customerSays(box uuid.UUID, body string, at time.Time) {
	e.t.Helper()
	e.exec(`INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction,
		kind, origin, body, sent_at)
		SELECT tenant_id, id, channel_account_id, 'm_' || gen_random_uuid()::text, 'in', 'message', 'customer', $2, $3
		FROM conversation.conversations WHERE id = $1`, box, body, at.UTC())
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $2, last_message_at = $2 WHERE id = $1`, box, at.UTC())
}

// confirmWithAddress hoàn tất đơn o như màn hội thoại (LV04): sửa địa chỉ (updateOrder) rồi xác nhận (confirmOrder).
func (e *claimEnv) confirmWithAddress(o order.Order) order.Order {
	e.t.Helper()
	w := order.Writer{Events: e.d.Events, Queue: e.d.Queue, Realtime: e.bus}
	a := order.Actor{TenantID: e.tenant, UserID: e.an.UserID}
	var out order.Order
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		addr := order.ShipAddress{Name: "Chị Lan", Phone: "0901234567", Line: "12 Lê Lợi", ProvinceCode: "T01", WardCode: "T01001"}
		res, err := w.Edit(e.t.Context(), tx, o.SellerID, o.ID, order.EditInput{Version: o.Version, ShipAddress: &addr}, a, time.Now())
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = w.Confirm(e.t.Context(), tx, o.SellerID, o.ID, res.Order.Version, a, time.Now())
		return err //nolint:wrapcheck // test
	})
	return out
}

// expireDrafts chạy việc order.expire_drafts của M2 lúc at; trả số đơn đã hủy.
func (e *claimEnv) expireDrafts(at time.Time) int {
	e.t.Helper()
	w := order.Writer{Events: e.d.Events, Queue: e.d.Queue, Realtime: e.bus}
	var n int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var err error
		n, err = w.ExpireDrafts(e.t.Context(), tx, at, 200)
		return err //nolint:wrapcheck // test
	})
	return n
}

// sendCalls đếm lần máy Meta giả nhận tin thường tới PSID psid (POST {page}/messages có recipient.id).
func (e *claimEnv) sendCalls(psid string) int {
	n := 0
	for _, c := range e.fg.Calls(claimPageExt + "/messages") {
		var b struct {
			Recipient struct {
				ID string `json:"id"`
			} `json:"recipient"`
		}
		if json.Unmarshal(c.Body, &b) == nil && b.Recipient.ID == psid {
			n++
		}
	}
	return n
}

// LV04, LV05 (danh sách), LV06: phiên có 5 lần chốt — 3 còn nháp, 1 đã xác nhận (sửa địa chỉ rồi xác nhận), 1 quá hạn
// (việc order.expire_drafts) → danh sách 3 dòng theo giờ chốt, confirmedCount 1, expiredCount 1; dòng có mã đơn,
// khách, dòng hàng, tổng, hạn nháp, người chốt, kết quả xin địa chỉ; khách trả lời trong hộp thư sau lúc chốt →
// customerRepliedAfterAsk; canRemind theo chính sách (cửa sổ còn → được; quá 24 giờ và đã nhắn riêng → không, lý do).
// Đơn xác nhận giữ nguyên "có thể bán" (hàng vẫn giữ). Phiên của Page không được xem → ErrNotFound.
func TestPending_ListCounts(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v, now := e.variant(20), time.Now()
	a, b, c, d, x := e.fiveClaims(v, now, "61000000001730")
	before := e.available(v)
	e.customerSays(a.inbox, "12 Lê Lợi, phường Bến Thành", now.Add(time.Second))

	if got := e.confirmWithAddress(d.order); got.Status != order.StatusConfirmed {
		t.Fatalf("hoàn tất đơn = %s", got.Status)
	}
	if got := e.available(v); got != before {
		t.Errorf("có thể bán sau xác nhận = %d, muốn %d (hàng vẫn giữ)", got, before)
	}
	e.exec(`UPDATE ord.orders SET draft_expires_at = $2 WHERE id = $1`, x.order.ID, now.Add(-time.Minute).UTC())
	if n := e.expireDrafts(now); n != 1 {
		t.Fatalf("expire_drafts hủy %d đơn, muốn 1", n)
	}
	if got := e.available(v); got != before+1 {
		t.Errorf("có thể bán sau hết hạn = %d, muốn %d", got, before+1)
	}

	l := e.mustPending(now)
	if len(l.Items) != 3 || l.ConfirmedCount != 1 || l.ExpiredCount != 1 {
		t.Fatalf("danh sách = %d dòng, xác nhận %d, hết hạn %d", len(l.Items), l.ConfirmedCount, l.ExpiredCount)
	}
	want := []pendingCust{a, b, c}
	for i, r := range l.Items {
		w := want[i]
		if r.OrderID != w.order.ID || r.OrderCode != w.order.Code || r.CustomerID != w.c.customer || r.GrandTotal != w.order.GrandTotal ||
			r.ExpiresAt == nil || len(r.Items) != 1 || r.Items[0].VariantID != v || r.Items[0].Qty != 1 ||
			r.ClaimedBy != e.an.UserID || r.ClaimedByName != "An" || r.Ask.Status != "queued" {
			t.Errorf("dòng %d = %+v", i, r)
		}
	}
	if !l.Items[0].CustomerRepliedAfterAsk || l.Items[1].CustomerRepliedAfterAsk || l.Items[2].CustomerRepliedAfterAsk {
		t.Errorf("customerRepliedAfterAsk = %v %v %v", l.Items[0].CustomerRepliedAfterAsk, l.Items[1].CustomerRepliedAfterAsk,
			l.Items[2].CustomerRepliedAfterAsk)
	}
	if !l.Items[0].CanRemind.Allowed || !l.Items[1].CanRemind.Allowed {
		t.Errorf("canRemind cửa sổ còn = %+v %+v", l.Items[0].CanRemind, l.Items[1].CanRemind)
	}
	if r := l.Items[2].CanRemind; r.Allowed || r.Reason != conversation.RemindNoChannelText {
		t.Errorf("canRemind quá 24 giờ = %+v", r)
	}

	other := e.an
	other.Grants = []identity.Grant{{Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopePage, ID: e.fakePage.String()}}}
	if _, err := e.pending(other, now); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("Page không được xem: %v", err)
	}
}

// LV06: draft_ttl_minutes = 60 (M2) → chốt đặt hạn nháp now + 60 phút; 59 phút sau việc expire_drafts không hủy;
// 61 phút sau → đơn cancelled lý do draft_expired, "có thể bán" về như trước khi chốt, danh sách bỏ dòng,
// expiredCount 1; chạy lại việc → không đổi gì.
func TestPending_DraftExpires(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	e.exec(`UPDATE ord.order_settings SET draft_ttl_minutes = 60`)
	v := e.variant(5)
	now := time.Now()
	p := e.claimed("Lan", "6100000000173011", v, now.Add(-time.Hour), now)
	if p.order.DraftExpiresAt == nil || p.order.DraftExpiresAt.Sub(now) < 59*time.Minute || p.order.DraftExpiresAt.Sub(now) > 61*time.Minute {
		t.Fatalf("hạn nháp = %v", p.order.DraftExpiresAt)
	}
	if got := e.available(v); got != 4 {
		t.Fatalf("có thể bán sau chốt = %d", got)
	}
	if n := e.expireDrafts(now.Add(59 * time.Minute)); n != 0 {
		t.Fatalf("59 phút: hủy %d", n)
	}
	if l := e.mustPending(now.Add(59 * time.Minute)); len(l.Items) != 1 || l.ExpiredCount != 0 {
		t.Fatalf("59 phút: %+v", l)
	}
	if n := e.expireDrafts(now.Add(61 * time.Minute)); n != 1 {
		t.Fatalf("61 phút: hủy %d", n)
	}
	var status, reason string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, coalesce(cancel_reason, '') FROM ord.orders_v WHERE id = $1`,
			p.order.ID).Scan(&status, &reason)
	})
	if status != "cancelled" || reason != order.CancelDraftExpired {
		t.Errorf("đơn sau hạn = %s %s", status, reason)
	}
	if got := e.available(v); got != 5 {
		t.Errorf("có thể bán sau hạn = %d, muốn 5", got)
	}
	l := e.mustPending(now.Add(61 * time.Minute))
	if len(l.Items) != 0 || l.ExpiredCount != 1 || l.ConfirmedCount != 0 {
		t.Errorf("danh sách sau hạn = %+v", l)
	}
	if n := e.expireDrafts(now.Add(62 * time.Minute)); n != 0 {
		t.Errorf("chạy lại: hủy %d", n)
	}
	if got := e.available(v); got != 5 {
		t.Errorf("có thể bán sau chạy lại = %d", got)
	}
	if l := e.mustPending(now.Add(62 * time.Minute)); len(l.Items) != 0 || l.ExpiredCount != 1 {
		t.Errorf("chạy lại: %+v", l)
	}
}
