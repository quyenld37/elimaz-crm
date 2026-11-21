package order_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// listBase là mốc giờ của đơn trong test danh sách; đơn thứ i tạo lúc listBase + i giây.
func listBase() time.Time { return time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC) }

// createAt tạo đơn M2 in (người bán e.seller) lúc at.
func (e *orderEnv) createAt(in order.CreateInput, at time.Time) order.Order {
	e.t.Helper()
	in.SellerID = e.seller
	var o order.Order
	if err := e.app(func(tx pgx.Tx) error {
		res, err := e.writer().Create(e.t.Context(), tx, in, e.actor, at)
		o = res.Order
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("Create: %v", err)
	}
	return o
}

// listFilter là bộ lọc của danh sách đơn như tham số của listOrders: phần lọc của queries.OrderFilter cộng q và khách
// lọc (order.ResolveListSearch giải).
type listFilter struct {
	Labels                 []orderview.LabelKey
	Statuses               []string
	PendingReasons         []string
	Channels               []string
	TrafficSourceIDs       []uuid.UUID
	CareBy                 *uuid.UUID
	CustomerID             *uuid.UUID
	CreatedFrom, CreatedTo *time.Time
	Q                      string
	DuplicateSuspect       *bool
}

// list đọc danh sách đơn của người bán e.seller như handler listOrders (order.ResolveListSearch rồi queries.OrderList)
// trong một transaction crm_app.
func (e *orderEnv) list(f listFilter, c order.Cursor, limit int) ([]queries.OrderListRow, order.Cursor, error) {
	e.t.Helper()
	var rows []queries.OrderListRow
	var next queries.OrderCursor
	err := e.app(func(tx pgx.Tx) error {
		s, err := order.ResolveListSearch(e.t.Context(), tx, f.Q, f.CustomerID)
		if err != nil || s.Empty {
			return err //nolint:wrapcheck // test
		}
		qf := queries.OrderFilter{
			SellerID: e.seller, Labels: f.Labels, Statuses: f.Statuses, PendingReasons: f.PendingReasons,
			Channels: f.Channels, TrafficSourceIDs: f.TrafficSourceIDs, CareBy: f.CareBy, CreatedFrom: f.CreatedFrom,
			CreatedTo: f.CreatedTo, DuplicateSuspect: f.DuplicateSuspect,
			Search: queries.OrderSearch{
				CustomerRoots: s.CustomerRoots, NameRoots: s.NameRoots, Code: s.Code, Phone: s.Phone, PhoneTail: s.PhoneTail,
			},
		}
		rows, next, err = queries.OrderList(e.t.Context(), tx, qf, queries.OrderCursor{CreatedAt: c.CreatedAt, ID: c.ID}, limit)
		return err //nolint:wrapcheck // test
	})
	return rows, order.Cursor{CreatedAt: next.CreatedAt, ID: next.ID}, err
}

func (e *orderEnv) mustList(f listFilter) []uuid.UUID {
	e.t.Helper()
	rows, next, err := e.list(f, order.Cursor{}, 100)
	if err != nil {
		e.t.Fatalf("List(%+v): %v", f, err)
	}
	if next != (order.Cursor{}) {
		e.t.Fatalf("List(%+v): còn trang sau", f)
	}
	return rowIDs(rows)
}

func rowIDs(rows []queries.OrderListRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func sameIDs(t *testing.T, what string, got []uuid.UUID, want ...uuid.UUID) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: %v, muốn %v", what, got, want)
	}
}

// customerWriter là customer.Writer của test.
func customerWriter() customer.Writer {
	return customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
}

// listFixture: khách A "Hồng Nhung" và B "Bảo Ngọc" (B gộp vào A), khách C "Trần Tèo"; nguồn S.
//   - o1: khách A, manual, SĐT người nhận đuôi 5678, người phụ trách là e.actor, lúc listBase+1s;
//   - o2: khách B, facebook, lúc +2s;
//   - o3: khách C, manual, nguồn S, giảm tay vượt ngưỡng → chờ duyệt giảm giá, lúc +3s.
type listFixture struct {
	a, b, c    customer.Customer
	source     uuid.UUID
	o1, o2, o3 order.Order
}

func newListFixture(e *orderEnv) listFixture {
	e.t.Helper()
	v := e.variant(1000000, 100)
	f := listFixture{
		a: e.newCustomer("Hồng Nhung", "0911000001"), b: e.newCustomer("Bảo Ngọc", "0911000002"),
		c: e.newCustomer("Trần Tèo", "0911000003"),
	}
	if err := e.app(func(tx pgx.Tx) error {
		s, err := e.writer().CreateSource(e.t.Context(), tx, "Nguồn thử M2-24")
		f.source = s.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	in := func(c customer.Customer) order.CreateInput {
		x := e.m2Input(0, ln(v, 1, 0))
		x.Customer = customer.OrderCustomerInput{CustomerID: &c.ID}
		return x
	}
	i1 := in(f.a)
	i1.ShipAddress.Phone = logging.Phone("0901235678")
	i1.CareBy = &e.actor.UserID
	f.o1 = e.createAt(i1, listBase().Add(time.Second))
	i2 := in(f.b)
	i2.Channel = order.ChannelFacebook
	f.o2 = e.createAt(i2, listBase().Add(2*time.Second))
	i3 := in(f.c)
	i3.TrafficSourceID, i3.OrderDiscount = &f.source, 500000
	f.o3 = e.createAt(i3, listBase().Add(3*time.Second))
	if f.o3.PendingReason != order.PendingDiscountApproval {
		e.t.Fatalf("o3 không chờ duyệt: %s %q", f.o3.Status, f.o3.PendingReason)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := customerWriter().MergeCustomers(e.t.Context(), tx, f.a.ID, f.b.ID, customer.MatchedByStaff,
			customer.Actor{ID: e.actor.UserID}, e.now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("gộp B vào A: %v", err)
	}
	return f
}

// Lọc theo nhãn, kênh, nguồn, khách gốc (cả nhóm), người phụ trách, ngày, q (mã, đuôi SĐT, SĐT đủ, tên); nhãn của
// vận đơn lọc được từ M3 (không đơn nào có vận đơn → rỗng; trước là ErrLabelNotFilterable); người bán khác không thấy
// đơn (ADR-29).
func TestList_Filters(t *testing.T) {
	e := newM2Env(t)
	f := newListFixture(e)
	o1, o2, o3 := f.o1.ID, f.o2.ID, f.o3.ID
	sameIDs(t, "mặc định (mới nhất trước)", e.mustList(listFilter{}), o3, o2, o1)
	sameIDs(t, "label awaiting_discount_approval",
		e.mustList(listFilter{Labels: []orderview.LabelKey{orderview.LabelAwaitingDiscountApproval}}), o3)
	sameIDs(t, "label confirmed", e.mustList(listFilter{Labels: []orderview.LabelKey{orderview.LabelConfirmed}}), o2, o1)
	sameIDs(t, "channel manual", e.mustList(listFilter{Channels: []string{order.ChannelManual}}), o3, o1)
	sameIDs(t, "nguồn S", e.mustList(listFilter{TrafficSourceIDs: []uuid.UUID{f.source}}), o3)
	sameIDs(t, "khách gốc A", e.mustList(listFilter{CustomerID: &f.a.ID}), o2, o1)
	sameIDs(t, "khách B (cả nhóm gốc A)", e.mustList(listFilter{CustomerID: &f.b.ID}), o2, o1)
	sameIDs(t, "người phụ trách", e.mustList(listFilter{CareBy: &e.actor.UserID}), o1)
	sameIDs(t, "trạng thái new", e.mustList(listFilter{Statuses: []string{order.StatusNew}}), o3)
	sameIDs(t, "lý do chờ", e.mustList(listFilter{PendingReasons: []string{order.PendingDiscountApproval}}), o3)
	from, to := listBase().Add(2*time.Second), listBase().Add(3*time.Second)
	sameIDs(t, "ngày [from, to)", e.mustList(listFilter{CreatedFrom: &from, CreatedTo: &to}), o2)
	no := false
	sameIDs(t, "không nghi trùng", e.mustList(listFilter{DuplicateSuspect: &no}), o3, o2, o1)
	sameIDs(t, "q mã đơn", e.mustList(listFilter{Q: f.o2.Code}), o2)
	sameIDs(t, "q mã đơn chữ thường", e.mustList(listFilter{Q: "dh" + f.o2.Code[2:]}), o2)
	sameIDs(t, "q đuôi SĐT 5678", e.mustList(listFilter{Q: "5678"}), o1)
	sameIDs(t, "q SĐT đủ", e.mustList(listFilter{Q: "0901 235 678"}), o1)
	sameIDs(t, "q tên hong nhung", e.mustList(listFilter{Q: "hong nhung"}), o2, o1)
	sameIDs(t, "q tên không ai", e.mustList(listFilter{Q: "khong ai ten nay"}))
	sameIDs(t, "q và khách", e.mustList(listFilter{Q: "5678", CustomerID: &f.c.ID}))
	sameIDs(t, "label shipping (M3)", e.mustList(listFilter{Labels: []orderview.LabelKey{orderview.LabelShipping}}))
	if _, _, err := e.list(listFilter{Q: "56"}, order.Cursor{}, 10); !errors.Is(err, customer.ErrQueryTooShort) {
		t.Errorf("q 2 chữ số: %v, muốn ErrQueryTooShort", err)
	}
	if _, _, err := e.list(listFilter{Labels: []orderview.LabelKey{"khac"}}, order.Cursor{}, 10); !errors.Is(err, queries.ErrInvalidOrderFilter) {
		t.Errorf("label lạ: %v, muốn ErrInvalidInput", err)
	}

	rows, _, err := e.list(listFilter{Q: f.o1.Code}, order.Cursor{}, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("đọc o1: %v %v", rows, err)
	}
	r := rows[0]
	if r.Code != f.o1.Code || r.Label != orderview.LabelConfirmed || r.Status != order.StatusConfirmed ||
		r.CustomerRootID != f.a.ID || r.CustomerName != "Hồng Nhung" || r.ShipPhone != "+84901235678" ||
		r.GrandTotal != f.o1.GrandTotal || r.CodAmount != f.o1.CODAmount || r.ItemCount != 1 ||
		r.Channel != order.ChannelManual || r.CareBy == nil || *r.CareBy != e.actor.UserID ||
		!r.CreatedAt.Equal(listBase().Add(time.Second)) || r.DuplicateSuspect {
		t.Errorf("dòng o1: %+v", r)
	}
	rows, _, _ = e.list(listFilter{Q: f.o2.Code}, order.Cursor{}, 10)
	if len(rows) != 1 || rows[0].CustomerRootID != f.a.ID || rows[0].CustomerName != "Hồng Nhung" {
		t.Errorf("dòng o2 phải mang khách gốc A: %+v", rows)
	}

	other := *e
	other.seller = e.partner
	if ids := other.mustList(listFilter{}); len(ids) != 0 {
		t.Errorf("người bán khác thấy %d đơn", len(ids))
	}
}

// Mỗi nhãn lọc được ở M2 chỉ trả đơn có đúng nhãn đó (lọc khớp orderview.Label của dòng), và mọi đơn có trong đúng
// một nhãn.
func TestList_LabelMatchesRowLabel(t *testing.T) {
	e := newM2Env(t)
	f := newListFixture(e)
	v := e.variant(100000, 100)
	at := listBase().Add(time.Minute)
	draft := e.m2Input(0, ln(v, 1, 0))
	draft.Intent = order.IntentDraft
	e.createAt(draft, at)
	pend := e.m2Input(0, ln(v, 1, 0))
	pend.Intent = order.IntentNew
	p := e.createAt(pend, at.Add(time.Second))
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().SetPending(e.t.Context(), tx, e.seller, p.ID, p.Version, order.PendingAwaitingCustomer, e.actor, at)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	c := e.createAt(e.m2Input(0, ln(v, 1, 0)), at.Add(2*time.Second))
	if err := e.app(func(tx pgx.Tx) error {
		return order.ApplyTransition(e.t.Context(), tx, e.writer(), &c, order.StatusCancelled, "", "", e.actor, at)
	}); err != nil {
		t.Fatal(err)
	}
	checkEachLabelOnce(t, e.mustList(listFilter{}), func(l orderview.LabelKey) []queries.OrderListRow {
		rows, _, err := e.list(listFilter{Labels: []orderview.LabelKey{l}}, order.Cursor{}, 100)
		if err != nil {
			t.Fatalf("nhãn %s: %v", l, err)
		}
		return rows
	})
	got := e.mustList(listFilter{Labels: []orderview.LabelKey{orderview.LabelAwaitingCustomer, orderview.LabelCancelled}})
	sameIDs(t, "hai nhãn", got, c.ID, p.ID)
	_ = f
}

// checkEachLabelOnce: với mọi nhãn của orderview.Labels(), byLabel(l) chỉ trả đơn có nhãn l, và mỗi đơn của all có
// trong đúng một nhãn.
func checkEachLabelOnce(t *testing.T, all []uuid.UUID, byLabel func(orderview.LabelKey) []queries.OrderListRow) {
	t.Helper()
	seen := map[uuid.UUID]int{}
	for _, l := range orderview.Labels() {
		for _, r := range byLabel(l) {
			seen[r.ID]++
			if r.Label != l {
				t.Errorf("nhãn %s trả đơn %s nhãn %s", l, r.Code, r.Label)
			}
		}
	}
	for _, id := range all {
		if seen[id] != 1 {
			t.Errorf("đơn %s có trong %d nhãn", id, seen[id])
		}
	}
}

// 250 đơn, limit 100 → ba trang 100/100/50, không trùng, không sót, đúng thứ tự (created_at, id) giảm; chèn 5 đơn mới
// giữa trang 1 và 2 → trang 2, 3 vẫn đúng phần còn lại của tập ban đầu. Con trỏ hỏng → ErrInvalidInput.
func TestList_KeysetPagination(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(10000, 1000)
	type made struct {
		id uuid.UUID
		at time.Time
	}
	initial := make([]made, 0, 250)
	for i := range 250 {
		// Hai đơn một giây: kiểm thứ tự theo id khi trùng created_at.
		at := listBase().Add(time.Duration(i/2) * time.Second)
		initial = append(initial, made{e.createAt(e.m2Input(0, ln(v, 1, 0)), at).ID, at})
	}
	rows, next, err := e.list(listFilter{}, order.Cursor{}, 100)
	if err != nil || len(rows) != 100 || next == (order.Cursor{}) {
		t.Fatalf("trang 1: %d %v %v", len(rows), next, err)
	}
	got := rowIDs(rows)
	for i := range 5 {
		e.createAt(e.m2Input(0, ln(v, 1, 0)), listBase().Add(time.Hour+time.Duration(i)*time.Second))
	}
	parsed, err := order.ParseCursor(next.String())
	if err != nil || !parsed.CreatedAt.Equal(next.CreatedAt) || parsed.ID != next.ID {
		t.Fatalf("ParseCursor(String) = %v %v, muốn %v", parsed, err, next)
	}
	rows, next, err = e.list(listFilter{}, parsed, 100)
	if err != nil || len(rows) != 100 || next == (order.Cursor{}) {
		t.Fatalf("trang 2: %d %v", len(rows), err)
	}
	got = append(got, rowIDs(rows)...)
	rows, next, err = e.list(listFilter{}, next, 100)
	if err != nil || len(rows) != 50 || next != (order.Cursor{}) {
		t.Fatalf("trang 3: %d %v %v", len(rows), next, err)
	}
	got = append(got, rowIDs(rows)...)
	// Thứ tự mong đợi: created_at giảm, cùng giây thì id giảm.
	slices.SortFunc(initial, func(x, y made) int {
		if c := y.at.Compare(x.at); c != 0 {
			return c
		}
		return bytes.Compare(y.id[:], x.id[:])
	})
	want := make([]uuid.UUID, 0, len(initial))
	for _, m := range initial {
		want = append(want, m.id)
	}
	sameIDs(t, "ba trang", got, want...)
	for _, bad := range []string{"x", "!!!", order.Cursor{}.String() + "x"} {
		if _, err := order.ParseCursor(bad); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("ParseCursor(%q): %v", bad, err)
		}
	}
}
