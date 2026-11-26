package live_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Quy luật (rapid, 100 lượt; LV07, LV08, LV09, DH08): mỗi lượt 1–3 biến thể có tồn ngẫu nhiên 0–5, 1–3 khách; một chuỗi
// ngẫu nhiên 1–6 lô thao tác chạy song song trong lô (chốt comment mới, thêm vào nháp đang có, gửi lại đúng comment đã
// chốt) → tổng số lượng giữ của các đơn nháp mỗi biến thể = tồn ban đầu − "có thể bán" và không bao giờ vượt tồn ban
// đầu; mỗi comment ≤ 1 dòng ghi nhận, ≤ 1 tin riêng; mọi lỗi là lỗi nghiệp vụ dự kiến.
func TestClaimProperty_NeverOversellOneClaimPerComment(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		stock := map[uuid.UUID]int{}
		var variants []uuid.UUID
		for i := range rapid.IntRange(1, 3).Draw(rt, "biến thể") {
			s := rapid.IntRange(0, 5).Draw(rt, fmt.Sprintf("tồn%d", i))
			v := e.variant(int32(s)) //nolint:gosec // ≤ 5
			stock[v], variants = s, append(variants, v)
		}
		customers := make([]uuid.UUID, rapid.IntRange(1, 3).Draw(rt, "khách"))
		for i := range customers {
			customers[i] = e.customer(fmt.Sprintf("Khách %d-%d", n, i))
		}
		var claimed []liveComment
		for b := range rapid.IntRange(1, 6).Draw(rt, "lô") {
			ops := rapid.SliceOfN(rapid.IntRange(0, 2), 1, 4).Draw(rt, fmt.Sprintf("thao tác%d", b))
			claimed = append(claimed, e.propBatch(rt, n, b, ops, variants, customers, claimed)...)
		}
		e.checkProperty(rt, stock)
	})
}

// propBatch chạy song song các thao tác ops (0 chốt comment mới, 1 thêm vào nháp đang có, 2 gửi lại comment đã chốt);
// trả các comment mới được chốt.
func (e *claimEnv) propBatch(rt *rapid.T, n, b int, ops []int, variants, customers []uuid.UUID, claimed []liveComment) []liveComment {
	type job struct {
		c  liveComment
		in conversation.LiveClaimInput
	}
	jobs := make([]job, 0, len(ops))
	for i, op := range ops {
		v := rapid.SampledFrom(variants).Draw(rt, fmt.Sprintf("v%d.%d", b, i))
		qty := rapid.IntRange(1, 3).Draw(rt, fmt.Sprintf("sl%d.%d", b, i))
		cust := rapid.IntRange(0, len(customers)-1).Draw(rt, fmt.Sprintf("k%d.%d", b, i))
		in := newClaim(v, qty)
		in.ForceNew = rapid.Bool().Draw(rt, fmt.Sprintf("forceNew%d.%d", b, i))
		from := fmt.Sprintf("61%03d%02d%07d", n, cust, 1)
		switch {
		case op == 2 && len(claimed) > 0:
			jobs = append(jobs, job{c: rapid.SampledFrom(claimed).Draw(rt, fmt.Sprintf("lại%d.%d", b, i)), in: in})
			continue
		case op == 1:
			if d := e.anyDraft(customers[cust]); d != nil {
				in.Mode, in.OrderID, in.ExpectedVersion = conversation.LiveClaimAddToDraft, &d.ID, d.Version
			}
		}
		jobs = append(jobs, job{c: e.comment(e.fbPage, customers[cust], from, "chốt", time.Now().Add(-time.Minute)), in: in})
	}
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() { _, errs[i] = e.claim(e.an, j.c.id, j.in) })
	}
	wg.Wait()
	var out []liveComment
	for i, err := range errs {
		switch {
		case err == nil:
			out = append(out, jobs[i].c)
		case errors.Is(err, order.ErrOutOfStock), errors.Is(err, conversation.ErrCommentClaimed),
			errors.Is(err, conversation.ErrOpenDraftExists), errors.Is(err, order.ErrVersionConflict):
		default:
			rt.Fatalf("lỗi lạ: %v", err)
		}
	}
	return out
}

// anyDraft trả một đơn nháp còn draft của khách cust (nil: không có).
func (e *claimEnv) anyDraft(cust uuid.UUID) *order.Order {
	e.t.Helper()
	var o order.Order
	found := e.count(`SELECT count(*) FROM ord.orders WHERE customer_id = $1 AND status = 'draft'`, cust) > 0
	if !found {
		return nil
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, version FROM ord.orders WHERE customer_id = $1 AND status = 'draft'
			ORDER BY created_at LIMIT 1`, cust).Scan(&o.ID, &o.Version)
	})
	return &o
}

// checkProperty: mỗi biến thể, giữ của đơn nháp = tồn ban đầu − có thể bán ≤ tồn ban đầu; mỗi comment ≤ 1 ghi nhận,
// ≤ 1 tin riêng đang sống.
func (e *claimEnv) checkProperty(rt *rapid.T, stock map[uuid.UUID]int) {
	for v, s := range stock {
		held := e.count(`SELECT coalesce(sum(i.quantity), 0)::int FROM ord.order_items i JOIN ord.orders o ON o.id = i.order_id
			WHERE i.variant_id = $1 AND o.status = 'draft'`, v)
		if avail := e.available(v); held > s || avail < 0 || held != s-avail {
			rt.Fatalf("biến thể %s: tồn %d, giữ của nháp %d, có thể bán %d", v, s, held, avail)
		}
	}
	if n := e.count(`SELECT count(*) FROM (SELECT message_id FROM conversation.live_comment_claims GROUP BY message_id
		HAVING count(*) > 1) x`); n != 0 {
		rt.Fatalf("%d comment có hơn một ghi nhận", n)
	}
	if n := e.count(`SELECT count(*) FROM (SELECT parent_external_id FROM conversation.messages WHERE kind = 'private_reply'
		GROUP BY parent_external_id HAVING count(*) > 1) x`); n != 0 {
		rt.Fatalf("%d comment có hơn một tin riêng", n)
	}
}
