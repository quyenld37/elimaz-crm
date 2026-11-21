package order_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// dupEnv là orderEnv M2 (cửa sổ đơn trùng mặc định 24 giờ) có sổ sự kiện đăng ký consumer order_duplicates: đơn tạo,
// sửa bằng dupEnv.w() ghi việc event.order_duplicates; drain chạy worker tới khi hết việc.
type dupEnv struct {
	*orderEnv
	reg *events.Registry
}

func newDupEnv(t *testing.T) *dupEnv {
	t.Helper()
	d := &dupEnv{orderEnv: newM2Env(t), reg: events.NewRegistry()}
	if err := d.reg.Register(order.DuplicatesConsumer{Now: time.Now}); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d *dupEnv) w() order.Writer {
	return order.Writer{Events: d.reg, Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
}

// at là giờ h (giờ thứ h tính từ 00:00 ngày 19/11/2026 UTC).
func at(h int) time.Time {
	return time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
}

// placeFor tạo đơn confirmed lúc when cho khách cust (nil: khách mới "Chị Lan"), SĐT người nhận phone, mỗi biến thể
// trong vs một chiếc.
func (d *dupEnv) placeFor(cust *uuid.UUID, phone string, when time.Time, vs ...uuid.UUID) order.Order {
	d.t.Helper()
	o, err := d.tryPlace(cust, phone, when, vs...)
	if err != nil {
		d.t.Fatalf("tạo đơn: %v", err)
	}
	return o
}

// tryPlace như placeFor nhưng trả lỗi (gọi được từ goroutine).
func (d *dupEnv) tryPlace(cust *uuid.UUID, phone string, when time.Time, vs ...uuid.UUID) (order.Order, error) {
	lines := make([]order.LineInput, 0, len(vs))
	for _, v := range vs {
		lines = append(lines, ln(v, 1, 0))
	}
	in := d.m2Input(0, lines...)
	if cust != nil {
		in.Customer = customer.OrderCustomerInput{CustomerID: cust}
	}
	addr := *fullAddress()
	addr.Phone = logging.Phone(phone)
	in.ShipAddress = &addr
	var res order.CreateResult
	err := d.app(func(tx pgx.Tx) error {
		var err error
		res, err = d.w().Create(d.t.Context(), tx, in, d.actor, when)
		return err //nolint:wrapcheck // test
	})
	return res.Order, err
}

func (d *dupEnv) place(phone string, when time.Time, vs ...uuid.UUID) order.Order {
	d.t.Helper()
	return d.placeFor(nil, phone, when, vs...)
}

// drain chạy worker (consumer order_duplicates) tới khi không còn việc.
func (d *dupEnv) drain() {
	d.t.Helper()
	w := queue.NewWorker(d.staff.App, d.reg.JobHandlers(), queue.WorkerOptions{WorkerID: "dup-" + uuid.NewString()})
	testkit.DrainDue(d.t, d.staff.Owner, w)
}

// links đếm dòng liên kết giữa a và b (hai chiều), matchedBy rỗng là mọi cách khớp.
func (d *dupEnv) links(a, b uuid.UUID, matchedBy string) int {
	d.t.Helper()
	return d.count(`SELECT count(*) FROM ord.order_duplicate_links WHERE order_id = least($1::uuid, $2::uuid)
		AND other_order_id = greatest($1::uuid, $2::uuid) AND ($3 = '' OR matched_by = $3)`, a, b, matchedBy)
}

func (d *dupEnv) linksOf(o uuid.UUID) int {
	d.t.Helper()
	return d.count(`SELECT count(*) FROM ord.order_duplicate_links WHERE order_id = $1 OR other_order_id = $1`, o)
}

func (d *dupEnv) suspect(o uuid.UUID) bool {
	d.t.Helper()
	return d.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND duplicate_suspect`, o) == 1
}

// resolve xử lý cặp (o, other) bằng action; hủy một đơn thì gửi version hiện tại của đơn bị hủy (đơn không có: không
// gửi version).
func (d *dupEnv) resolve(o, other uuid.UUID, action string) error {
	var version *int64
	target := map[string]uuid.UUID{order.DuplicateCancelOther: other, order.DuplicateCancelThis: o}[action]
	if target != uuid.Nil && d.count(`SELECT count(*) FROM ord.orders WHERE id = $1`, target) == 1 {
		v := d.get(target).Version
		version = &v
	}
	return d.resolveV(o, other, action, version)
}

// resolveV như resolve, version gửi đúng như đưa vào (nil: không gửi).
func (d *dupEnv) resolveV(o, other uuid.UUID, action string, version *int64) error {
	return d.app(func(tx pgx.Tx) error {
		return d.w().ResolveDuplicate(d.t.Context(), tx, d.seller, o, other, action, version, d.actor, at(20))
	})
}

func (d *dupEnv) list() []order.Duplicate {
	d.t.Helper()
	var out []order.Duplicate
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		out, _, err = order.ListDuplicates(d.t.Context(), tx, d.seller, nil, 50)
		return err //nolint:wrapcheck // test
	}); err != nil {
		d.t.Fatal(err)
	}
	return out
}

func (d *dupEnv) check(cust *uuid.UUID, phone string, now time.Time, vs ...uuid.UUID) []order.Summary {
	d.t.Helper()
	var out []order.Summary
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		out, err = order.CheckDuplicates(d.t.Context(), tx, d.seller, cust, phone, vs, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		d.t.Fatal(err)
	}
	return out
}

// pair tạo O1 lúc 10:00, O2 lúc 13:00 (cùng SĐT phone, cùng biến thể v) rồi chạy worker.
func (d *dupEnv) pair(phone string, v uuid.UUID) (order.Order, order.Order) {
	d.t.Helper()
	o1 := d.place(phone, at(10), v)
	o2 := d.place(phone, at(13), v)
	d.drain()
	return o1, o2
}

func ids(s []order.Summary) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(s))
	for _, o := range s {
		out[o.ID] = true
	}
	return out
}

// DH09 (cửa sổ 24 giờ): O1, O2 cùng SĐT, cùng V1 lúc 10:00 và 13:00 → sau một lượt worker cả hai nghi trùng, một dòng
// matched_by phone, có trong ListDuplicates, không đơn nào bị hủy; O3 (chỉ V3) không liên kết; O4 lúc 11:00 hôm sau
// (25 giờ sau O1, 22 giờ sau O2) liên kết với O2, không với O1. checkOrderDuplicates lúc 14:00 (SĐT X, V1) → O1, O2.
func TestDuplicates_DH09(t *testing.T) {
	d := newDupEnv(t)
	v1, v3 := d.variant(250000, 20), d.variant(180000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	if !d.suspect(o1.ID) || !d.suspect(o2.ID) {
		t.Fatalf("cờ nghi trùng O1 %v, O2 %v", d.suspect(o1.ID), d.suspect(o2.ID))
	}
	if n := d.links(o1.ID, o2.ID, order.MatchedByPhone); n != 1 {
		t.Fatalf("liên kết O1–O2 phone: %d", n)
	}
	if got := d.get(o1.ID); got.Version != o1.Version || got.Status != order.StatusConfirmed {
		t.Errorf("O1 sau khi đánh dấu: version %d (trước %d), %s", got.Version, o1.Version, got.Status)
	}
	if n := d.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'order.updated'`); n != 0 {
		t.Errorf("đánh dấu nghi trùng phát %d order.updated", n)
	}
	if n := d.count(`SELECT count(*) FROM ord.orders WHERE status = 'cancelled'`); n != 0 {
		t.Errorf("%d đơn bị hủy", n)
	}
	l := d.list()
	if len(l) != 1 || l[0].MatchedBy != order.MatchedByPhone || l[0].Resolution != nil ||
		!ids([]order.Summary{l[0].Order, l[0].OtherOrder})[o1.ID] ||
		!ids([]order.Summary{l[0].Order, l[0].OtherOrder})[o2.ID] {
		t.Fatalf("ListDuplicates: %+v", l)
	}
	if s := l[0].Order; s.Code == "" || s.ItemCount != 1 || s.GrandTotal != 250000 || !s.DuplicateSuspect ||
		s.Label != "confirmed" || s.CustomerName != "Chị Lan" || string(s.ShipPhone) != shipPhoneNorm {
		t.Errorf("tóm tắt đơn: %+v", s)
	}

	got := d.check(nil, shipPhone, at(14), v1)
	if len(got) != 2 || !ids(got)[o1.ID] || !ids(got)[o2.ID] {
		t.Errorf("CheckDuplicates 14:00: %+v", got)
	}
	if got := d.check(nil, shipPhone, at(14), v3); len(got) != 0 {
		t.Errorf("CheckDuplicates V3: %+v", got)
	}

	o3 := d.place(shipPhone, at(14), v3)
	o4 := d.place(shipPhone, at(35), v1)
	d.drain()
	if n := d.linksOf(o3.ID); n != 0 || d.suspect(o3.ID) {
		t.Errorf("O3 (chỉ V3): %d liên kết, cờ %v", n, d.suspect(o3.ID))
	}
	if d.links(o2.ID, o4.ID, order.MatchedByPhone) != 1 || d.links(o1.ID, o4.ID, "") != 0 || !d.suspect(o4.ID) {
		t.Errorf("O4: O2–O4 %d, O1–O4 %d", d.links(o2.ID, o4.ID, ""), d.links(o1.ID, o4.ID, ""))
	}
	if n := len(d.list()); n != 2 {
		t.Errorf("ListDuplicates: %d cặp, mong 2", n)
	}
}

// Cùng khách gốc, SĐT người nhận khác → matched_by customer. Đơn đã hủy không liên kết.
func TestDuplicates_SameCustomer(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	c := d.newCustomer("Chị Hoa", momPhone)
	a := d.placeFor(&c.ID, shipPhone, at(10), v1)
	b := d.placeFor(&c.ID, "0987654321", at(12), v1)
	d.drain()
	if n := d.links(a.ID, b.ID, order.MatchedByCustomer); n != 1 {
		t.Fatalf("liên kết customer: %d", n)
	}
	if got := d.check(&c.ID, "", at(13), v1); len(got) != 2 {
		t.Errorf("CheckDuplicates theo khách: %+v", got)
	}

	gone := d.place("0933000111", at(10), v1)
	if _, err := d.cancel(d.get(gone.ID)); err != nil {
		t.Fatal(err)
	}
	fresh := d.place("0933000111", at(11), v1)
	d.drain()
	if d.links(gone.ID, fresh.ID, "") != 0 || d.suspect(fresh.ID) {
		t.Errorf("đơn đã hủy được liên kết")
	}
}

// Cửa sổ 0 (tắt) → không liên kết, CheckDuplicates rỗng.
func TestDuplicates_WindowZero(t *testing.T) {
	d := newDupEnv(t)
	d.staff.Exec(t, d.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.order_settings SET duplicate_window_minutes = 0`)
		return err //nolint:wrapcheck // test
	})
	v1 := d.variant(250000, 20)
	a := d.place(shipPhone, at(10), v1)
	b := d.place(shipPhone, at(10), v1)
	d.drain()
	if d.links(a.ID, b.ID, "") != 0 || d.suspect(a.ID) {
		t.Errorf("cửa sổ 0 vẫn liên kết")
	}
	if got := d.check(nil, shipPhone, at(10), v1); len(got) != 0 {
		t.Errorf("CheckDuplicates cửa sổ 0: %+v", got)
	}
}

// not_duplicate → hai đơn hết cờ, cặp ghi resolution; order.updated của O2 sau đó (gắn vận đơn) → không liên kết lại.
// Đơn còn cặp mở khác giữ cờ.
func TestDuplicates_NotDuplicate(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	if err := d.resolve(o2.ID, o1.ID, order.DuplicateNotDuplicate); err != nil {
		t.Fatalf("not_duplicate: %v", err)
	}
	if d.suspect(o1.ID) || d.suspect(o2.ID) {
		t.Errorf("còn cờ sau not_duplicate")
	}
	if n := d.count(`SELECT count(*) FROM ord.order_duplicate_links WHERE resolution = 'not_duplicate'
		AND resolved_at IS NOT NULL AND resolved_by = $1`, d.actor.UserID); n != 1 {
		t.Errorf("dòng đã xử lý: %d", n)
	}
	d.attachShipment(o2.ID)
	if n := d.events(o2.ID, order.TopicOrderUpdated, o2.Version+1); n != 1 {
		t.Fatalf("order.updated của O2: %d", n)
	}
	d.drain()
	if d.links(o1.ID, o2.ID, "") != 1 || d.suspect(o1.ID) || d.suspect(o2.ID) || len(d.list()) != 0 {
		t.Errorf("liên kết lại sau order.updated")
	}
	if err := d.resolve(o1.ID, o2.ID, order.DuplicateCancelOther); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("xử lý lại cặp đã xong: %v", err)
	}

	// Ba đơn: xử lý một cặp, hai đơn vẫn còn cặp với đơn thứ ba → giữ cờ.
	a := d.place(momPhone, at(10), v1)
	b := d.place(momPhone, at(11), v1)
	c := d.place(momPhone, at(12), v1)
	d.drain()
	if err := d.resolve(a.ID, b.ID, order.DuplicateNotDuplicate); err != nil {
		t.Fatal(err)
	}
	if !d.suspect(a.ID) || !d.suspect(b.ID) || !d.suspect(c.ID) {
		t.Errorf("đơn còn cặp mở bị gỡ cờ")
	}
}

// cancel_other từ O1 → O2 cancelled lý do duplicate, hàng nhả, cặp resolution cancelled, hai đơn hết cờ;
// cancel_this hủy chính đơn trên đường dẫn. Cặp không có → ErrNotFound; hành động lạ, hai mã trùng → ErrInvalidInput.
func TestDuplicates_Cancel(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	if err := d.resolve(o1.ID, o2.ID, "merge"); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("hành động lạ: %v", err)
	}
	if err := d.resolve(o1.ID, o1.ID, order.DuplicateNotDuplicate); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("hai mã trùng: %v", err)
	}
	if err := d.resolve(o1.ID, uuid.New(), order.DuplicateNotDuplicate); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("cặp không có: %v", err)
	}
	if err := d.resolve(o1.ID, o2.ID, order.DuplicateCancelOther); err != nil {
		t.Fatalf("cancel_other: %v", err)
	}
	got := d.get(o2.ID)
	if got.Status != order.StatusCancelled || got.CancelReason == nil || *got.CancelReason != order.CancelDuplicate {
		t.Fatalf("O2 sau cancel_other: %s %v", got.Status, got.CancelReason)
	}
	if d.get(o1.ID).Status != order.StatusConfirmed {
		t.Errorf("O1 bị đổi")
	}
	if n := d.heldOf(o2.ID, inventory.StateHeld); n != 0 {
		t.Errorf("O2 còn giữ %d", n)
	}
	if b := d.balance(v1); b.Held != 1 {
		t.Errorf("V1 held %d, mong 1 (chỉ O1)", b.Held)
	}
	if n := d.count(`SELECT count(*) FROM ord.order_duplicate_links WHERE resolution = 'cancelled'`); n != 1 {
		t.Errorf("cặp cancelled: %d", n)
	}
	if d.suspect(o1.ID) || d.suspect(o2.ID) {
		t.Errorf("còn cờ sau hủy")
	}

	a := d.place(momPhone, at(10), v1)
	b := d.place(momPhone, at(11), v1)
	d.drain()
	if err := d.resolve(b.ID, a.ID, order.DuplicateCancelThis); err != nil {
		t.Fatalf("cancel_this: %v", err)
	}
	if d.get(b.ID).Status != order.StatusCancelled || d.get(a.ID).Status != order.StatusConfirmed {
		t.Errorf("cancel_this hủy sai đơn")
	}
	var dup order.Duplicate
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		dup, err = order.GetDuplicate(t.Context(), tx, d.seller, b.ID, a.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if dup.Order.ID != b.ID || dup.OtherOrder.ID != a.ID || dup.Resolution == nil || *dup.Resolution != "cancelled" ||
		dup.ResolvedAt == nil {
		t.Errorf("GetDuplicate: %+v", dup)
	}
}

// O1, O2 tạo cùng lúc (hai transaction song song) → sau worker đúng một dòng liên kết, cả hai có cờ. Bốn đơn tạo song
// song (consumer của bốn đơn chạy song song, mỗi việc khóa nhiều đơn theo id tăng dần) → đủ 6 cặp, worker không lỗi
// (không deadlock).
func TestDuplicates_ConcurrentCreate(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	parallel := func(n int, phone string) []order.Order {
		var wg sync.WaitGroup
		got, errs := make([]order.Order, n), make([]error, n)
		for i := range n {
			wg.Go(func() { got[i], errs[i] = d.tryPlace(nil, phone, at(10), v1) })
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatal(err)
		}
		d.drain()
		return got
	}
	got := parallel(2, shipPhone)
	if n := d.links(got[0].ID, got[1].ID, ""); n != 1 {
		t.Fatalf("liên kết: %d", n)
	}
	if n := d.count(`SELECT count(*) FROM ord.order_duplicate_links`); n != 1 {
		t.Errorf("tổng liên kết: %d", n)
	}
	if !d.suspect(got[0].ID) || !d.suspect(got[1].ID) {
		t.Errorf("thiếu cờ")
	}
	four := parallel(4, momPhone)
	for i := range four {
		for j := i + 1; j < len(four); j++ {
			if n := d.links(four[i].ID, four[j].ID, order.MatchedByPhone); n != 1 {
				t.Errorf("cặp %d–%d: %d dòng", i, j, n)
			}
		}
	}
}

// Consumer nhận lại cùng sự kiện bốn lần song song sau khi cặp đã xử lý → không thêm dòng, không bật lại cờ; consumer
// tên order_duplicates nghe order.created, order.updated, order.cancelled; payload hỏng → ErrPermanent.
func TestDuplicates_ConsumerReplay(t *testing.T) {
	d := newDupEnv(t)
	c := order.DuplicatesConsumer{Now: time.Now}
	if c.Name() != "order_duplicates" ||
		!slices.Equal(c.Topics(), []string{order.TopicOrderCreated, order.TopicOrderUpdated, "order.cancelled"}) {
		t.Fatalf("consumer: %s %v", c.Name(), c.Topics())
	}
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	ev := events.StoredEvent{
		ID: uuid.New(), TenantID: d.tenant, AggregateType: "order", AggregateID: o2.ID.String(), Seq: 1,
		Topic:   order.TopicOrderCreated,
		Payload: []byte(`{"orderId":"` + o2.ID.String() + `","sellerId":"` + d.seller.String() + `","status":"confirmed"}`),
	}
	if err := d.resolve(o1.ID, o2.ID, order.DuplicateNotDuplicate); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range 4 {
		wg.Go(func() { errs[i] = d.app(func(tx pgx.Tx) error { return c.Handle(t.Context(), ev, tx) }) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if d.linksOf(o2.ID) != 1 || d.suspect(o1.ID) || d.suspect(o2.ID) {
		t.Errorf("giao lại đổi cặp đã xử lý")
	}
	bad := ev
	bad.Payload = []byte(`{}`)
	if err := d.app(func(tx pgx.Tx) error { return c.Handle(t.Context(), bad, tx) }); !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("payload hỏng: %v", err)
	}
}

// ListDuplicates phân trang: ba cặp, limit 2 → trang 2 cặp kèm con trỏ, trang sau 1 cặp không con trỏ, không trùng;
// con trỏ qua String/ParseDuplicateCursor giữ nguyên; chuỗi hỏng → ErrInvalidInput.
func TestDuplicates_ListPages(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	for _, p := range []string{shipPhone, momPhone, "0933000111"} {
		d.pair(p, v1)
	}
	page := func(after *order.DuplicateCursor) ([]order.Duplicate, *order.DuplicateCursor) {
		var out []order.Duplicate
		var next *order.DuplicateCursor
		if err := d.app(func(tx pgx.Tx) error {
			var err error
			out, next, err = order.ListDuplicates(t.Context(), tx, d.seller, after, 2)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return out, next
	}
	first, next := page(nil)
	if len(first) != 2 || next == nil {
		t.Fatalf("trang 1: %d cặp, con trỏ %v", len(first), next)
	}
	back, err := order.ParseDuplicateCursor(next.String())
	if err != nil || !back.At.Equal(next.At) || back.OrderID != next.OrderID || back.OtherOrderID != next.OtherOrderID {
		t.Fatalf("con trỏ qua chuỗi: %+v %v", back, err)
	}
	second, last := page(&back)
	if len(second) != 1 || last != nil {
		t.Fatalf("trang 2: %d cặp, con trỏ %v", len(second), last)
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range append(first, second...) {
		if seen[p.Order.ID] {
			t.Errorf("cặp lặp")
		}
		seen[p.Order.ID] = true
	}
	for _, s := range []string{"xx", "", next.String() + "A"} {
		if _, err := order.ParseDuplicateCursor(s); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("con trỏ %q: %v", s, err)
		}
	}
}

// B-05 (Task 436): consumer lấy giờ từ đồng hồ được giao (background.Deps.Now), không gọi time.Now trong module: mốc của
// cặp nghi trùng là đúng giờ của đồng hồ đó; thiếu đồng hồ → lỗi, không ghi cặp.
func TestDuplicates_ConsumerClock(t *testing.T) {
	d := newDupEnv(t)
	v := d.variant(250000, 20)
	o1 := d.place(shipPhone, at(10), v)
	o2 := d.place(shipPhone, at(13), v)
	ev := events.StoredEvent{
		ID: uuid.New(), TenantID: d.tenant, AggregateType: "order", AggregateID: o2.ID.String(), Seq: 1,
		Topic:   order.TopicOrderCreated,
		Payload: []byte(`{"orderId":"` + o2.ID.String() + `","sellerId":"` + d.seller.String() + `"}`),
	}
	if err := d.app(func(tx pgx.Tx) error {
		return order.DuplicatesConsumer{}.Handle(t.Context(), ev, tx)
	}); err == nil || d.links(o1.ID, o2.ID, "") != 0 {
		t.Fatalf("thiếu đồng hồ: %v, %d cặp", err, d.links(o1.ID, o2.ID, ""))
	}
	clock := at(14).Add(1234 * time.Microsecond)
	if err := d.app(func(tx pgx.Tx) error {
		return order.DuplicatesConsumer{Now: func() time.Time { return clock }}.Handle(t.Context(), ev, tx)
	}); err != nil {
		t.Fatal(err)
	}
	var got order.Duplicate
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		got, err = order.GetDuplicate(t.Context(), tx, d.seller, o1.ID, o2.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(clock) {
		t.Errorf("mốc cặp %v, muốn %v (đồng hồ của consumer)", got.CreatedAt, clock)
	}
}
