package query_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// order chèn đơn của khách cust trạng thái status lúc at (người bán ELIMAZ của Page, kho MAIN); trả mã.
func (e *env) order(cust uuid.UUID, code, status string, total int64, at time.Time) uuid.UUID {
	e.t.Helper()
	e.exec(`INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
		SELECT current_setting('app.tenant_id')::uuid, id, 'MAIN', 'Kho' FROM catalog.sellers WHERE code = 'ELIMAZ'
		ON CONFLICT DO NOTHING`)
	var id uuid.UUID
	e.scan(&id, `INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
			pending_reason, packing_status, ship_address, subtotal, shipping_fee_charged, cod_amount, grand_total, warehouse_id, ship_phone,
			ship_phone_reversed, created_at, draft_expires_at)
		SELECT current_setting('app.tenant_id')::uuid, s.id, $1, $2, 'manual', gen_random_uuid(), $3, '', 'none', '{}',
		       0, $4, $4, $4, w.id, '+84900000000', '00000000948', $5,
		       CASE WHEN $3 = 'draft' THEN $5::timestamptz + interval '2 hours' END
		FROM catalog.sellers AS s JOIN inventory.warehouses AS w ON w.operated_by_seller_id = s.id
		WHERE s.code = 'ELIMAZ' RETURNING id`, code, cust, status, total, at)
	return id
}

func (e *env) card(u identity.AuthUser, id uuid.UUID) (conversation.Card, error) {
	e.t.Helper()
	var c conversation.Card
	err := e.read(func(tx pgx.Tx) (err error) {
		c, err = conversation.CustomerCard(e.t.Context(), tx, u, id)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

// [Task 159, DH03 phần hiển thị, C3] Thẻ khách: gốc A có 8 đơn giao, 2 đơn hoàn → returnRateBP 2000, đúng bằng
// customer.Get (không công thức thứ hai); hồ sơ B gộp vào A, hội thoại gắn B → thẻ là của gốc A; recentOrders là 5 đơn
// mới nhất (trừ nháp) của cả A và B, mới nhất trước; openDrafts có đơn nháp của B; nhân viên không có customer.read →
// không có SĐT, không gợi ý gộp; có customer.read → SĐT của nhóm; nguồn tên, cờ không liên hệ của gốc.
func TestCustomerCard(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	sale := e.user("sale@elimaz.test", e.pageA, "conversation.read")
	cs := e.user("cskh@elimaz.test", e.pageA, "conversation.read", "customer.read")
	a := e.customer("Chị Hoa", "0903334444")
	b := e.customer("Hoa FB", "")
	e.merge(a, b)
	e.exec(`UPDATE customer.customers SET order_count = 10, delivered_count = 8, returned_count = 2,
		total_spent = 4500000, do_not_contact = true WHERE id = $1`, a)
	base := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	var orders []uuid.UUID // A: 0, 2, 4; B: 1, 3, 5 (đơn i lúc base + i ngày)
	for i := range 6 {
		cust := a
		if i%2 == 1 {
			cust = b
		}
		orders = append(orders, e.order(cust, fmt.Sprintf("DH%07d", i), "new", int64(100000*(i+1)),
			base.Add(time.Duration(i)*24*time.Hour)))
	}
	draft := e.order(b, "DH0000009", "draft", 250000, base.Add(10*24*time.Hour))
	id := e.conv(conv{page: e.pageA, customer: b})

	c, err := e.card(sale, id)
	if err != nil {
		t.Fatal(err)
	}
	var prof customer.Profile
	if err := e.read(func(tx pgx.Tx) (err error) {
		prof, err = customer.Get(t.Context(), tx, b)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if c.Customer.ID != a || c.Customer.RootID != a || c.Customer.Stats.ReturnRateBP != 2000 ||
		c.Customer.Stats != prof.Stats || c.Customer.Stats.DeliveredCount != 8 || c.Customer.Stats.ReturnedCount != 2 {
		t.Fatalf("hồ sơ thẻ = %+v; customer.Get = %+v", c.Customer, prof.Customer)
	}
	if !c.DoNotContact || c.NameSource != customer.NameSourceStaff {
		t.Fatalf("cờ không liên hệ %v, nguồn tên %q", c.DoNotContact, c.NameSource)
	}
	got := make([]uuid.UUID, 0, len(c.RecentOrders))
	for _, o := range c.RecentOrders {
		got = append(got, o.ID)
	}
	if want := []uuid.UUID{orders[5], orders[4], orders[3], orders[2], orders[1]}; !slices.Equal(got, want) {
		t.Fatalf("recentOrders = %v, muốn %v", got, want)
	}
	if r := c.RecentOrders[0]; r.GrandTotal != 600000 || r.Status != "new" || r.Code != "DH0000005" {
		t.Fatalf("đơn mới nhất = %+v", r)
	}
	if len(c.OpenDrafts) != 1 || c.OpenDrafts[0].ID != draft || c.OpenDrafts[0].DraftExpiresAt == nil {
		t.Fatalf("openDrafts = %+v", c.OpenDrafts)
	}
	if c.Phones != nil || c.MergeSuggestion != nil {
		t.Fatalf("không có customer.read: SĐT %v, gợi ý %v", c.Phones, c.MergeSuggestion)
	}
	c, err = e.card(cs, id)
	if err != nil || !slices.Equal(c.Phones, []logging.Phone{"+84903334444"}) {
		t.Fatalf("có customer.read: SĐT %v (lỗi %v)", c.Phones, err)
	}
}

func (e *env) orderContext(u identity.AuthUser, id uuid.UUID) (conversation.OrderContextResult, error) {
	e.t.Helper()
	var c conversation.OrderContextResult
	err := e.read(func(tx pgx.Tx) (err error) {
		c, err = conversation.OrderContext(e.t.Context(), tx, u, id)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

// [Task 159, 01 mục 2] Bối cảnh lên đơn: Page A có nguồn hộp thư, nguồn comment, khung live 11:00–14:00 → "Live ca
// trưa". Comment trên bài live bắt đầu 12:15 giờ Việt Nam → "Live ca trưa"; bài live 15:00 (ngoài khung) → nguồn
// comment; comment bài thường → nguồn comment; hộp thư → nguồn hộp thư; người bán là của Page. Page B chưa có cài đặt →
// nguồn nil; Page B có nguồn hộp thư đã tắt → nil.
func TestOrderContext(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("len-don@elimaz.test", uuid.Nil, "conversation.read")
	inbox := e.s.TrafficSource(t, e.tenant, "Hộp thư", true)
	comment := e.s.TrafficSource(t, e.tenant, "Comment", true)
	noon := e.s.TrafficSource(t, e.tenant, "Live ca trưa", true)
	off := e.s.TrafficSource(t, e.tenant, "Đã tắt", false)
	e.exec(`INSERT INTO conversation.page_settings (tenant_id, channel_account_id, inbox_traffic_source_id,
		comment_traffic_source_id, live_slots) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3,
		jsonb_build_array(jsonb_build_object('from', '11:00', 'to', '14:00', 'trafficSourceId', $4::text)))`,
		e.pageA, inbox, comment, noon)
	cust := e.customer("Khách live", "")
	liveNoon := e.post(e.pageA, "live-1", "live", new(time.Date(2026, 11, 20, 5, 15, 0, 0, time.UTC)))
	liveLate := e.post(e.pageA, "live-2", "live", new(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)))
	plain := e.post(e.pageA, "post-1", "post", new(time.Date(2026, 11, 20, 5, 15, 0, 0, time.UTC)))
	var seller uuid.UUID
	e.scan(&seller, `SELECT seller_id FROM channel.channel_accounts WHERE id = $1`, e.pageA)
	for _, c := range []struct {
		name string
		c    conv
		want *uuid.UUID
	}{
		{"comment live 12:15", conv{page: e.pageA, customer: cust, kind: "comment", post: &liveNoon}, &noon},
		{"comment live 15:00", conv{page: e.pageA, customer: cust, kind: "comment", post: &liveLate}, &comment},
		{"comment bài thường", conv{page: e.pageA, customer: cust, kind: "comment", post: &plain}, &comment},
		{"hộp thư", conv{page: e.pageA, customer: cust}, &inbox},
		{"Page B chưa cài đặt", conv{page: e.pageB, customer: cust}, nil},
	} {
		id := e.conv(c.c)
		got, err := e.orderContext(u, id)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.ConversationID != id || got.ChannelAccountID != c.c.page || got.CustomerID != cust ||
			got.SellerID != seller || !equalPtr(got.TrafficSourceID, c.want) {
			t.Errorf("%s: %+v, muốn nguồn %v", c.name, got, c.want)
		}
	}
	e.exec(`INSERT INTO conversation.page_settings (tenant_id, channel_account_id, inbox_traffic_source_id)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, e.pageB, off)
	got, err := e.orderContext(u, e.conv(conv{page: e.pageB, customer: cust}))
	if err != nil || got.TrafficSourceID != nil {
		t.Fatalf("nguồn đã tắt: %v (lỗi %v), muốn nil", got.TrafficSourceID, err)
	}
}

func equalPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// [Task 159, PQ04 cho M4] Nhân viên chỉ có conversation.read (và reply) trên Page A: danh sách chỉ có hội thoại của A
// (lọc pageId = B → rỗng); đếm chỉ có Page A; tin, thẻ khách, bối cảnh lên đơn, đánh dấu đã đọc, đổi trạng thái của
// hội thoại Page B → ErrNotFound (404); hội thoại không có → ErrNotFound.
func TestPageScope_PQ04(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	u := e.user("page-a@elimaz.test", e.pageA, "conversation.read", "conversation.reply")
	cust := e.customer("Khách hai Page", "")
	convA := e.conv(conv{page: e.pageA, customer: cust, unread: 1})
	convB := e.conv(conv{page: e.pageB, customer: cust, unread: 1})
	var msgB uuid.UUID
	e.scan(&msgB, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id,
		direction, kind, origin, body, sent_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'm-b', 'in',
		'message', 'customer', 'x', now()) RETURNING id`, convB, e.pageB)
	rows, _, err := e.list(u, conversation.ListFilter{})
	if err != nil || !slices.Equal(ids(rows), []uuid.UUID{convA}) {
		t.Fatalf("danh sách: %v (lỗi %v), muốn chỉ Page A", ids(rows), err)
	}
	if rows, _, err := e.list(u, conversation.ListFilter{PageID: &e.pageB}); err != nil || len(rows) != 0 {
		t.Fatalf("lọc Page B: %d dòng (lỗi %v)", len(rows), err)
	}
	var counts conversation.Counts
	if err := e.read(func(tx pgx.Tx) (err error) {
		counts, err = conversation.CountConversations(t.Context(), tx, u)
		return err //nolint:wrapcheck // test
	}); err != nil || len(counts.ByPage) != 1 || counts.ByPage[0].PageID != e.pageA || counts.ByPage[0].Unread != 1 ||
		counts.Unassigned != 1 {
		t.Fatalf("đếm = %+v (lỗi %v)", counts, err)
	}
	w := conversation.Writer{}
	for _, id := range []uuid.UUID{convB, uuid.New()} {
		for name, op := range map[string]func(tx pgx.Tx) error{
			"tin": func(tx pgx.Tx) error {
				_, err := conversation.ListMessages(t.Context(), tx, u, id, nil, nil, 0)
				return err //nolint:wrapcheck // test
			},
			"thẻ khách": func(tx pgx.Tx) error {
				_, err := conversation.CustomerCard(t.Context(), tx, u, id)
				return err //nolint:wrapcheck // test
			},
			"bối cảnh lên đơn": func(tx pgx.Tx) error {
				_, err := conversation.OrderContext(t.Context(), tx, u, id)
				return err //nolint:wrapcheck // test
			},
			"đã đọc": func(tx pgx.Tx) error { return w.MarkRead(t.Context(), tx, u, id, msgB) },
			"trạng thái": func(tx pgx.Tx) error {
				return w.SetStatus(t.Context(), tx, u, id, conversation.StatusDone, 1)
			},
		} {
			if err := e.read(op); !errors.Is(err, conversation.ErrNotFound) {
				t.Errorf("%s của %s: lỗi %v, muốn ErrNotFound", name, id, err)
			}
		}
	}
}
