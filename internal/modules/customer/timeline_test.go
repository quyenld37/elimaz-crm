package customer_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func (e *writeEnv) timeline(tenant, id uuid.UUID, after *customer.TimelineCursor, limit int,
) ([]customer.TimelineItem, *customer.TimelineCursor, error) {
	var items []customer.TimelineItem
	var next *customer.TimelineCursor
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		items, next, err = customer.Timeline(e.t.Context(), tx, id, after, limit)
		return err //nolint:wrapcheck // test
	})
	return items, next, err
}

func (e *writeEnv) mustTimeline(id uuid.UUID, after *customer.TimelineCursor, limit int,
) ([]customer.TimelineItem, *customer.TimelineCursor) {
	e.t.Helper()
	items, next, err := e.timeline(e.tenant, id, after, limit)
	if err != nil {
		e.t.Fatal(err)
	}
	return items, next
}

func refs(items []customer.TimelineItem) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		out = append(out, it.RefID)
	}
	return out
}

func kinds(items []customer.TimelineItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Kind)
	}
	return out
}

// Dòng thời gian của cả nhóm: 3 đơn (của A và của B), 2 ghi chú, 1 lần gộp B vào A → 6 mục giảm dần theo lúc; đọc
// qua A hay qua B như nhau. Hai trang 3 + 3 không trùng, không sót; thêm một ghi chú giữa hai lần đọc thì trang sau
// không đổi, ghi chú mới chỉ ở đầu trang đầu khi đọc lại.
func TestTimelineGroupKeyset(t *testing.T) {
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "A"})
	b := e.create(customer.CreateInput{FullName: "B"})
	h := func(n int) time.Time { return e.now.Add(time.Duration(n) * time.Hour) }
	o1 := e.insertOrder(a.ID, "", h(-5))
	o2 := e.insertOrder(b.ID, "", h(-4))
	o3 := e.insertOrder(a.ID, "", h(-3))
	n1 := e.mustNote(a.ID, "Ghi chú một", h(-2))
	n2 := e.mustNote(b.ID, "Ghi chú hai", h(-1))
	m := e.mustMerge(a.ID, b.ID) // lúc e.now

	want := []uuid.UUID{m.ID, n2.ID, n1.ID, o3, o2, o1}
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		all, next := e.mustTimeline(id, nil, 50)
		if !slices.Equal(refs(all), want) || next != nil {
			t.Fatalf("dòng thời gian qua %s: %v %v (next %v)", id, kinds(all), refs(all), next)
		}
	}
	all, _ := e.mustTimeline(a.ID, nil, 50)
	if !slices.Equal(kinds(all), []string{"merge", "note", "note", "order_created", "order_created", "order_created"}) {
		t.Fatalf("loại: %v", kinds(all))
	}
	if all[1].Note != "Ghi chú hai" || all[1].ActorID == nil || *all[1].ActorID != e.actor.ID || !all[1].At.Equal(h(-1)) {
		t.Fatalf("mục ghi chú: %+v", all[1])
	}
	if all[0].Summary["survivor_id"] != a.ID.String() || all[0].Summary["merged_id"] != b.ID.String() ||
		all[0].Summary["matched_by"] != customer.MatchedByStaff {
		t.Fatalf("mục gộp: %+v", all[0].Summary)
	}
	if all[3].Summary["status"] != "confirmed" || all[3].Summary["code"] == "" || all[3].Note != "" {
		t.Fatalf("mục đơn: %+v", all[3])
	}

	p1, c1 := e.mustTimeline(a.ID, nil, 3)
	if !slices.Equal(refs(p1), want[:3]) || c1 == nil {
		t.Fatalf("trang 1: %v", refs(p1))
	}
	n3 := e.mustNote(b.ID, "Ghi chú mới", h(1))
	parsed, err := customer.ParseTimelineCursor(c1.String())
	if err != nil || !parsed.At.Equal(c1.At) || parsed.Kind != c1.Kind || parsed.RefID != c1.RefID {
		t.Fatalf("con trỏ qua chuỗi: %v %s %s (%v)", parsed.At, parsed.Kind, parsed.RefID, err)
	}
	p2, c2 := e.mustTimeline(a.ID, &parsed, 3)
	if !slices.Equal(refs(p2), want[3:]) || c2 != nil {
		t.Fatalf("trang 2: %v (next %v)", refs(p2), c2)
	}
	again, _ := e.mustTimeline(a.ID, nil, 3)
	if !slices.Equal(refs(again), []uuid.UUID{n3.ID, m.ID, n2.ID}) {
		t.Fatalf("đọc lại trang 1: %v", refs(again))
	}
}

// Các loại còn lại: đổi trạng thái đơn (bỏ dòng tạo đơn from_status rỗng), nhãn đang gắn của nhóm (một mục mỗi nhãn),
// hoàn tác gộp (mục merge_undone lúc hoàn tác, hiện ở cả hai nhóm).
// Tóm tắt không có dữ liệu cá nhân. Khách tenant khác → ErrNotFound; con trỏ hỏng → ErrInvalidInput.
func TestTimelineKinds(t *testing.T) {
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Nguyễn Văn A", Phone: "0912000111"})
	b := e.create(customer.CreateInput{FullName: "B"})
	h := func(n int) time.Time { return e.now.Add(time.Duration(n) * time.Hour) }
	o := e.insertOrder(a.ID, "+84912000111", h(-10))
	var created, cancelled uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		ctx := t.Context()
		if err := tx.QueryRow(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, from_status, to_status,
			actor_type, occurred_at) VALUES ($1, $2, NULL, 'confirmed', 'system', $3) RETURNING id`,
			e.tenant, o, h(-10)).Scan(&created); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, from_status, to_status,
			actor_type, actor_id, reason, occurred_at) VALUES ($1, $2, 'confirmed', 'cancelled', 'staff', $3,
			'customer_request', $4) RETURNING id`, e.tenant, o, e.actor.ID, h(-9)).Scan(&cancelled)
	})
	vip := e.mustTag("VIP", "red")
	if err := e.addTag(e.tenant, a.ID, vip.ID, customer.TagSourceStaff); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		// Cùng nhãn ở hồ sơ con (gắn sau) → vẫn một mục, lúc gắn sớm nhất.
		_, err := tx.Exec(t.Context(), `UPDATE customer.customer_tag_links SET created_at = $1 WHERE tag_id = $2`, h(-8), vip.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source,
			created_at) VALUES ($1, $2, $3, 'rule', $4)`, e.tenant, b.ID, vip.ID, h(-7))
		return err //nolint:wrapcheck // test
	})
	m := e.mustMerge(a.ID, b.ID)
	if err := e.undo(m.ID, h(2)); err != nil {
		t.Fatal(err)
	}

	items, _ := e.mustTimeline(a.ID, nil, 50)
	wantKinds := []string{"merge_undone", "merge", "tag", "order_status", "order_created"}
	if !slices.Equal(kinds(items), wantKinds) || !slices.Equal(refs(items), []uuid.UUID{m.ID, m.ID, vip.ID, cancelled, o}) {
		t.Fatalf("dòng thời gian A: %v %v", kinds(items), refs(items))
	}
	if !items[0].At.Equal(h(2)) || items[0].ActorID == nil || *items[0].ActorID != e.actor.ID {
		t.Fatalf("mục hoàn tác: %+v", items[0])
	}
	if s := items[2].Summary; s["name"] != "VIP" || s["color"] != "red" || !items[2].At.Equal(h(-8)) {
		t.Fatalf("mục nhãn: %+v at %v", s, items[2].At)
	}
	if s := items[3].Summary; s["from_status"] != "confirmed" || s["to_status"] != "cancelled" ||
		s["order_id"] != o.String() || s["code"] == "" || s["reason"] != "customer_request" {
		t.Fatalf("mục đổi trạng thái: %+v", s)
	}
	noPersonalData(t, items, "Nguyễn Văn A", "+84912000111", "0912000111")
	// Sau hoàn tác, B là gốc riêng: thấy lần gộp, hoàn tác và nhãn của mình.
	bItems, _ := e.mustTimeline(b.ID, nil, 50)
	if !slices.Equal(kinds(bItems), []string{"merge_undone", "merge", "tag"}) || !bItems[2].At.Equal(h(-7)) {
		t.Fatalf("dòng thời gian B: %v", kinds(bItems))
	}

	if _, _, err := e.timeline(e.other, a.ID, nil, 50); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}
	if _, err := customer.ParseTimelineCursor("không-phải-con-trỏ"); !errors.Is(err, customer.ErrInvalidInput) {
		t.Fatalf("con trỏ hỏng: %v", err)
	}
}

// noPersonalData: không giá trị nào của tóm tắt trùng một giá trị cá nhân trong values.
func noPersonalData(t *testing.T, items []customer.TimelineItem, values ...string) {
	t.Helper()
	for _, it := range items {
		for k, v := range it.Summary {
			if slices.Contains(values, v) {
				t.Fatalf("tóm tắt %s.%s có dữ liệu cá nhân", it.Kind, k)
			}
		}
	}
}

// Mục cùng lúc: xếp theo kind giảm dần (note trước merge), rồi ref_id giảm dần; trang 1 + 2 nối lại đúng thứ tự đó.
func TestTimelineTiedAt(t *testing.T) {
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "C"})
	x, y := e.mustNote(c.ID, "x", e.now), e.mustNote(c.ID, "y", e.now)
	d := e.create(customer.CreateInput{FullName: "D"})
	md := e.mustMerge(c.ID, d.ID)
	tied, _ := e.mustTimeline(c.ID, nil, 50)
	hi, lo := x.ID, y.ID
	if slices.Compare(hi[:], lo[:]) < 0 {
		hi, lo = lo, hi
	}
	if !slices.Equal(refs(tied), []uuid.UUID{hi, lo, md.ID}) {
		t.Fatalf("cùng lúc: %v %v", kinds(tied), refs(tied))
	}
	p1, c1 := e.mustTimeline(c.ID, nil, 1)
	p2, _ := e.mustTimeline(c.ID, c1, 2)
	if !slices.Equal(append(refs(p1), refs(p2)...), refs(tied)) {
		t.Fatalf("trang khi cùng lúc: %v + %v", refs(p1), refs(p2))
	}
}
