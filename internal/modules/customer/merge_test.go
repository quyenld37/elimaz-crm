package customer_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// merge gộp nhóm của merged vào nhóm của survivor trong tenant A (giờ e.now).
func (e *writeEnv) merge(survivor, merged uuid.UUID) (customer.Merge, error) {
	return e.mergeIn(e.tenant, survivor, merged, customer.MatchedByStaff)
}

func (e *writeEnv) mergeIn(tenant, survivor, merged uuid.UUID, matchedBy string) (customer.Merge, error) {
	var m customer.Merge
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		m, err = e.w.MergeCustomers(e.t.Context(), tx, survivor, merged, matchedBy, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return m, err
}

func (e *writeEnv) mustMerge(survivor, merged uuid.UUID) customer.Merge {
	e.t.Helper()
	m, err := e.merge(survivor, merged)
	if err != nil {
		e.t.Fatalf("gộp %s vào %s: %v", merged, survivor, err)
	}
	return m
}

func (e *writeEnv) undo(mergeID uuid.UUID, now time.Time) error {
	return e.try(e.tenant, func(tx pgx.Tx) error {
		return e.w.UndoMerge(e.t.Context(), tx, mergeID, e.actor, now)
	})
}

func (e *writeEnv) getMerge(id uuid.UUID) (customer.Merge, error) {
	var m customer.Merge
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		m, err = customer.GetMerge(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return m, err
}

// people tạo mỗi tên một khách (không SĐT) và trả mã theo thứ tự tạo (uuidv7: mã tăng dần).
func (e *writeEnv) people(names ...string) []uuid.UUID {
	e.t.Helper()
	out := make([]uuid.UUID, 0, len(names))
	for _, n := range names {
		out = append(out, e.create(customer.CreateInput{FullName: n}).ID)
	}
	return out
}

// abc tạo ba khách A, B, C (không SĐT), mã tăng dần.
func (e *writeEnv) abc() (a, b, c uuid.UUID) {
	e.t.Helper()
	ids := e.people("A", "B", "C")
	return ids[0], ids[1], ids[2]
}

// roots đọc root_id của mọi hồ sơ trong tenant A.
func (e *writeEnv) roots() map[uuid.UUID]uuid.UUID {
	e.t.Helper()
	return e.rootsWhere("true")
}

// rootsOf đọc root_id của các hồ sơ ids.
func (e *writeEnv) rootsOf(ids []uuid.UUID) map[uuid.UUID]uuid.UUID {
	e.t.Helper()
	return e.rootsWhere("id = ANY($1)", ids)
}

func (e *writeEnv) rootsWhere(cond string, args ...any) map[uuid.UUID]uuid.UUID {
	e.t.Helper()
	out := map[uuid.UUID]uuid.UUID{}
	e.inTx(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT id, root_id FROM customer.customers WHERE "+cond, args...)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var id, root uuid.UUID
			if err := rows.Scan(&id, &root); err != nil {
				return err //nolint:wrapcheck // test
			}
			out[id] = root
		}
		return rows.Err()
	})
	return out
}

// rootsValid: mọi root_id trỏ tới một hồ sơ gốc (root_id = id), không vòng; trả mô tả lỗi đầu tiên.
func rootsValid(roots map[uuid.UUID]uuid.UUID) string {
	for id, r := range roots {
		if roots[r] != r {
			return fmt.Sprintf("hồ sơ %s trỏ gốc %s không phải gốc (gốc của nó là %s)", id, r, roots[r])
		}
	}
	return ""
}

// insertOrder chèn thẳng một đơn của khách (fixture SQL, không qua module order) và trả mã đơn.
func (e *writeEnv) insertOrder(customerID uuid.UUID, shipPhone string, at time.Time) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		ctx := e.t.Context()
		// Người bán và kho thử (tạo một lần mỗi tenant).
		if _, err := tx.Exec(ctx, `INSERT INTO catalog.sellers (tenant_id, code, name, kind)
			SELECT $1, 'T68', 'Người bán thử', 'own' WHERE NOT EXISTS (SELECT 1 FROM catalog.sellers WHERE code = 'T68')`,
			e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			SELECT $1, s.id, 'KT68', 'Kho thử' FROM catalog.sellers s
			WHERE s.code = 'T68' AND NOT EXISTS (SELECT 1 FROM inventory.warehouses WHERE code = 'KT68')`,
			e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO ord.orders (id, tenant_id, seller_id, code, customer_id, channel,
			idempotency_key, status, ship_address, subtotal, cod_amount, grand_total, warehouse_id, ship_phone, created_at)
			SELECT $1, $2, s.id, 'DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'), $3, 'manual', $1, 'confirmed',
			'{}', 0, 0, 0, w.id, $4, $5
			FROM catalog.sellers s JOIN inventory.warehouses w ON w.code = 'KT68' WHERE s.code = 'T68'`,
			id, e.tenant, customerID, shipPhone, at)
		return err //nolint:wrapcheck // test
	})
	return id
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	return out
}

// DH14: gộp B vào A chỉ đổi root_id (đơn, địa chỉ, ghi chú của B giữ customer_id = B); Get(A) có cả hai hồ sơ và địa
// chỉ của B; dòng gộp moved_ids = [B], sự kiện customer.merged, audit, báo hiệu hai gốc. Hoàn tác → B về gốc B, sự kiện
// customer.merge_undone (Seq 2), undone_at, undone_by; hoàn tác lần hai → version_conflict.
func TestMergeDH14(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị A", Phone: "0911000001"})
	b := e.create(customer.CreateInput{FullName: "Chị B", Phone: "0911000002"})
	e.insertOrder(a.ID, "+84911000001", e.now)
	o2 := e.insertOrder(b.ID, "+84911000002", e.now)
	addr, err := e.addAddress(b.ID, addrIn("T01001"), true)
	if err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_notes (tenant_id, customer_id, body, author_id)
			VALUES ($1, $2, 'ghi chú của B', $3)`, e.tenant, b.ID, e.actor.ID)
		return err //nolint:wrapcheck // test
	})
	signalsBefore := len(e.bus.Signals())

	m := e.mustMerge(a.ID, b.ID)
	if m.SurvivorID != a.ID || m.MergedID != b.ID || !slices.Equal(m.MovedIDs, []uuid.UUID{b.ID}) ||
		m.MatchedBy != customer.MatchedByStaff || m.ActorID != e.actor.ID || m.UndoneAt != nil || m.ID == uuid.Nil {
		t.Fatalf("lần gộp: %+v", m)
	}
	p := e.get(a.ID)
	if !slices.Equal(p.MemberIDs, sortedIDs(a.ID, b.ID)) || p.ID != a.ID ||
		!slices.ContainsFunc(p.Addresses, func(x customer.Address) bool { return x.ID == addr && x.CustomerID == b.ID }) {
		t.Fatalf("Get(A) sau gộp: thành viên %v, địa chỉ %+v", p.MemberIDs, p.Addresses)
	}
	e.expectCounts(t, map[string][]any{
		"đơn O2 giữ customer_id = B":  {`SELECT count(*) FROM ord.orders WHERE id = $1 AND customer_id = $2`, o2, b.ID},
		"ghi chú giữ customer_id = B": {`SELECT count(*) FROM customer.customer_notes WHERE customer_id = $1`, b.ID},
		"dòng customer_merges": {
			`SELECT count(*) FROM customer.customer_merges WHERE id = $1 AND survivor_id = $2
			AND merged_id = $3 AND moved_ids = $4 AND matched_by = 'staff' AND actor_id = $5`,
			m.ID, a.ID, b.ID,
			[]uuid.UUID{b.ID},
			e.actor.ID,
		},
		"sự kiện customer.merged": {`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.merged' AND seq = 1
			AND aggregate_type = 'customer_merge' AND aggregate_id = $1 AND payload->>'mergeId' = $1
			AND payload->>'survivorRootId' = $2 AND payload->>'mergedRootId' = $3`, m.ID.String(), a.ID.String(), b.ID.String()},
		"audit customer.merge": {`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.merge'
			AND entity_type = 'customer_merge' AND entity_id = $1 AND actor_id = $2`, m.ID.String(), e.actor.ID.String()},
	})
	var sigRoots []string
	for _, s := range e.bus.Signals()[signalsBefore:] {
		sigRoots = append(sigRoots, s.Entity.ID)
	}
	if !slices.Contains(sigRoots, a.ID.String()) || !slices.Contains(sigRoots, b.ID.String()) {
		t.Errorf("báo hiệu hai gốc: %v", sigRoots)
	}
	if got, err := e.getMerge(m.ID); err != nil || got.ID != m.ID || !slices.Equal(got.MovedIDs, m.MovedIDs) {
		t.Fatalf("GetMerge: %+v %v", got, err)
	}
	e.checkUndoDH14(t, m, a.ID, b.ID)
}

// checkUndoDH14: hoàn tác lần gộp B→A của DH14.
func (e *writeEnv) checkUndoDH14(t *testing.T, m customer.Merge, a, b uuid.UUID) {
	t.Helper()
	undoAt := e.now.Add(time.Hour)
	if err := e.undo(m.ID, undoAt); err != nil {
		t.Fatalf("hoàn tác: %v", err)
	}
	if p := e.get(a); !slices.Equal(p.MemberIDs, []uuid.UUID{a}) {
		t.Fatalf("Get(A) sau hoàn tác còn B: %v", p.MemberIDs)
	}
	if r := e.roots(); r[b] != b {
		t.Fatalf("root_id(B) = %s", r[b])
	}
	e.expectCounts(t, map[string][]any{
		"sự kiện customer.merge_undone": {`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.merge_undone'
			AND seq = 2 AND aggregate_type = 'customer_merge' AND aggregate_id = $1`, m.ID.String()},
		"undone_at, undone_by": {`SELECT count(*) FROM customer.customer_merges WHERE id = $1 AND undone_at = $2
			AND undone_by = $3`, m.ID, undoAt, e.actor.ID},
		"audit customer.merge_undo": {`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.merge_undo'
			AND entity_type = 'customer_merge' AND entity_id = $1`, m.ID.String()},
	})
	// Hoàn tác lần nữa: lần gộp đã hoàn tác → version_conflict (Quyết định thay chủ shop QĐ1).
	if err := e.undo(m.ID, undoAt); !errors.Is(err, customer.ErrVersionConflict) {
		t.Fatalf("hoàn tác lần hai: %v", err)
	}
}

// expectCounts: mỗi câu đếm (câu, tham số…) phải ra đúng 1.
func (e *writeEnv) expectCounts(t *testing.T, checks map[string][]any) {
	t.Helper()
	for what, q := range checks {
		if n := e.count(q[0].(string), q[1:]...); n != 1 { //nolint:forcetypeassert // test
			t.Errorf("%s: %d", what, n)
		}
	}
}

// Chuỗi: C đã gộp vào B; gộp B vào A (qua mã của hồ sơ con C) → moved_ids = [B, C] theo mã; hoàn tác → B và C về gốc B.
func TestMergeChain(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b, c := e.abc()
	e.mustMerge(b, c)
	m := e.mustMerge(a, c)
	if m.MergedID != b || !slices.Equal(m.MovedIDs, sortedIDs(b, c)) {
		t.Fatalf("lần gộp chuỗi: %+v", m)
	}
	if r := e.roots(); r[b] != a || r[c] != a {
		t.Fatalf("gốc sau gộp: %v", r)
	}
	if err := e.undo(m.ID, e.now); err != nil {
		t.Fatal(err)
	}
	if r := e.roots(); r[b] != b || r[c] != b || r[a] != a {
		t.Fatalf("gốc sau hoàn tác: %v", r)
	}
}

// Gộp đè: B vào A, rồi A vào D; hoàn tác B→A → merge_not_latest, BlockingMergeID = lần A→D, không đổi gì; hoàn tác A→D
// rồi B→A → được. Lần gộp độc lập vào cùng gốc (C vào A) không chặn hoàn tác B→A.
func TestUndoMergeNotLatest(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	ids := e.people("A", "B", "D", "C")
	a, b, d, c := ids[0], ids[1], ids[2], ids[3]
	ba := e.mustMerge(a, b)
	ad := e.mustMerge(d, a)
	err := e.undo(ba.ID, e.now)
	var nl *customer.MergeNotLatestError
	if !errors.As(err, &nl) || nl.BlockingMergeID != ad.ID || !errors.Is(err, customer.ErrMergeNotLatest) {
		t.Fatalf("hoàn tác B→A khi còn A→D: %v", err)
	}
	if r := e.roots(); r[b] != d || r[a] != d {
		t.Fatalf("lỗi không được đổi gì: %v", r)
	}
	for _, id := range []uuid.UUID{ad.ID, ba.ID} {
		if err := e.undo(id, e.now); err != nil {
			t.Fatal(err)
		}
	}
	if r := e.roots(); r[a] != a || r[b] != b || r[d] != d {
		t.Fatalf("sau hai lần hoàn tác: %v", r)
	}
	ba2 := e.mustMerge(a, b)
	e.mustMerge(a, c)
	if err := e.undo(ba2.ID, e.now); err != nil {
		t.Fatalf("lần gộp độc lập chặn hoàn tác: %v", err)
	}
	if r := e.roots(); r[b] != b || r[c] != a {
		t.Fatalf("hoàn tác B→A khi có C→A: %v", r)
	}
}

// Hạn hoàn tác: 30 ngày 1 giây → merge_window_expired, không đổi gì; đúng 30 ngày → được.
func TestUndoMergeWindow(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	ids := e.people("A", "B", "C")
	m := e.mustMerge(ids[0], ids[1])
	if err := e.undo(m.ID, m.CreatedAt.Add(30*24*time.Hour+time.Second)); !errors.Is(err, customer.ErrMergeWindowExpired) {
		t.Fatalf("quá hạn: %v", err)
	}
	if r := e.roots(); r[ids[1]] != ids[0] {
		t.Fatalf("quá hạn không được đổi gì: %v", r)
	}
	m2 := e.mustMerge(ids[0], ids[2])
	if err := e.undo(m2.ID, m2.CreatedAt.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("đúng 30 ngày: %v", err)
	}
}

// Gộp A vào A, B (đã thuộc A) vào A, A vào B (cùng gốc) → merge_same_customer; matchedBy lạ → invalid_input; khách không
// có hay của tenant khác → not_found; hoàn tác, đọc lần gộp không có → not_found.
func TestMergeErrors(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b, c := e.abc()
	if _, err := e.merge(a, a); !errors.Is(err, customer.ErrMergeSameCustomer) {
		t.Fatalf("A vào A: %v", err)
	}
	e.mustMerge(a, b)
	for _, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		if _, err := e.merge(pair[0], pair[1]); !errors.Is(err, customer.ErrMergeSameCustomer) {
			t.Fatalf("cùng gốc %v: %v", pair, err)
		}
	}
	if _, err := e.mergeIn(e.tenant, a, c, "auto"); !errors.Is(err, customer.ErrInvalidInput) {
		t.Fatalf("matchedBy lạ: %v", err)
	}
	if _, err := e.merge(a, uuid.New()); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("khách không có: %v", err)
	}
	if _, err := e.mergeIn(e.other, a, c, customer.MatchedByPhone); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}
	if err := e.undo(uuid.New(), e.now); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("hoàn tác lần gộp không có: %v", err)
	}
	if _, err := e.getMerge(uuid.New()); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("GetMerge không có: %v", err)
	}
}

// Đồng thời: gộp B→A và A→B cùng lúc, 100 vòng → mỗi vòng đúng một bên thành công, bên kia merge_same_customer; không
// vòng; không deadlock.
func TestMergeConcurrentOpposite(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	before := e.deadlocks()
	for i := range 100 {
		ids := e.people("A", "B")
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, errs[0] = e.merge(ids[0], ids[1]) })
		wg.Go(func() { _, errs[1] = e.merge(ids[1], ids[0]) })
		wg.Wait()
		ok, same := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, customer.ErrMergeSameCustomer):
				same++
			default:
				t.Fatalf("vòng %d: lỗi lạ %v", i, err)
			}
		}
		if ok != 1 || same != 1 {
			t.Fatalf("vòng %d: %d thành công, %d same: %v", i, ok, same, errs)
		}
	}
	if msg := rootsValid(e.roots()); msg != "" {
		t.Fatal(msg)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_merges`); n != 100 {
		t.Fatalf("số lần gộp: %d", n)
	}
	if after := e.deadlocks(); after != before {
		t.Fatalf("deadlock: %d → %d", before, after)
	}
}

// Gộp nhóm (thành viên có mã nhỏ hơn gốc) song song với tìm-hoặc-tạo theo mã thành viên (lockPair khóa thành viên rồi
// gốc theo mã tăng dần), 20 vòng: không deadlock, gốc luôn hợp lệ.
func TestMergeVsForOrderNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	before := e.deadlocks()
	for i := range 20 {
		ids := e.people("X", "Y", "Z") // X < Y < Z
		x, y, z := ids[0], ids[1], ids[2]
		e.mustMerge(y, x) // nhóm Y = {X, Y}, thành viên X nhỏ hơn gốc Y
		var mergeErr, orderErr error
		var wg sync.WaitGroup
		wg.Go(func() { _, mergeErr = e.merge(z, y) })
		wg.Go(func() {
			_, orderErr = e.forOrder(customer.OrderCustomerInput{
				CustomerID: &x, Phone: fmt.Sprintf("09030%05d", i), PhoneIsCustomers: true,
			})
		})
		wg.Wait()
		if mergeErr != nil || orderErr != nil {
			t.Fatalf("vòng %d: gộp %v, đơn %v", i, mergeErr, orderErr)
		}
	}
	if msg := rootsValid(e.roots()); msg != "" {
		t.Fatal(msg)
	}
	if after := e.deadlocks(); after != before {
		t.Fatalf("deadlock: %d → %d", before, after)
	}
}

func (e *writeEnv) deadlocks() int {
	e.staff.Count(e.t, "SELECT 0 FROM pg_stat_force_next_flush()")
	return e.staff.Count(e.t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
}
