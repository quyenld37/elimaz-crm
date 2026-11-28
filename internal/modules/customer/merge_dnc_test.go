package customer_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Task 521: gộp không được làm mất cờ "không liên hệ" của hồ sơ nào trong hai hồ sơ, dù ai là gốc giữ lại.
func TestMergeDoNotContactKeepsFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		flagA, flagB  bool
		want          bool
		auditExpected int
	}{
		{"gốc giữ lại có cờ", true, false, true, 0},
		{"gốc bị gộp có cờ", false, true, true, 1},
		{"cả hai có cờ", true, true, true, 0},
		{"không ai có cờ", false, false, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkMergeKeepsFlag(t, c.flagA, c.flagB, c.want, c.auditExpected)
		})
	}
}

func checkMergeKeepsFlag(t *testing.T, flagA, flagB, want bool, auditExpected int) {
	t.Helper()
	e := newWriteEnv(t)
	a, b := e.pair("Chị Lan Gốc", "Chị Lan Phụ", "")
	for id, on := range map[uuid.UUID]bool{a.ID: flagA, b.ID: flagB} {
		if on {
			if err := e.setDNC(e.tenant, id, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	m := e.mustMerge(a.ID, b.ID) // A giữ lại, B bị gộp
	if e.isDNC(a.ID) != want || e.isDNC(b.ID) != want {
		t.Errorf("sau gộp: A %v, B %v, muốn %v", e.isDNC(a.ID), e.isDNC(b.ID), want)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.do_not_contact'
		AND entity_id = $1 AND row_to_json(audit_logs)::text LIKE '%' || $2 || '%'
		AND row_to_json(audit_logs)::text NOT LIKE '%Lan%'`, a.ID.String(), m.ID.String()); n != auditExpected {
		t.Errorf("audit chép cờ khi gộp: %d, muốn %d", n, auditExpected)
	}
	// Gốc bị gộp không bị xóa cờ: hoàn tác trả đúng trạng thái của nó.
	if err := e.undo(m.ID, e.now); err != nil {
		t.Fatal(err)
	}
	if e.isDNC(b.ID) != flagB || (flagB && !e.isDNC(a.ID)) {
		t.Errorf("sau hoàn tác: A %v, B %v (B muốn %v, A phải giữ cờ nếu B có)", e.isDNC(a.ID), e.isDNC(b.ID), flagB)
	}
}

// Đảo thứ tự: B có cờ và là gốc giữ lại; A có cờ và bị gộp.
func TestMergeDoNotContactReverseOrder(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	ids := e.people("Gốc cũ", "Gốc mới")
	a, b := customerRef{ID: ids[0]}, customerRef{ID: ids[1]}
	if err := e.setDNC(e.tenant, ids[0], true); err != nil {
		t.Fatal(err)
	}
	e.mustMerge(b.ID, a.ID) // B giữ lại, A (có cờ) bị gộp
	if !e.isDNC(b.ID) || !e.isDNC(a.ID) {
		t.Fatalf("B giữ lại phải mang cờ của A: A %v, B %v", e.isDNC(a.ID), e.isDNC(b.ID))
	}
	// Giữ mốc lúc khách yêu cầu, không đặt lại bằng giờ gộp.
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND do_not_contact
		AND do_not_contact_at = $2`, b.ID, e.now); n != 1 {
		t.Errorf("mốc do_not_contact_at của gốc giữ lại: %d", n)
	}
}

// Gộp cả nhóm: gốc bị gộp đang có hồ sơ con, cờ vẫn chép; chuỗi gộp A←B←C giữ cờ của C.
func TestMergeDoNotContactChain(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b, c := e.abc()
	if err := e.setDNC(e.tenant, c, true); err != nil {
		t.Fatal(err)
	}
	e.mustMerge(b, c)
	e.mustMerge(a, b)
	for _, id := range []uuid.UUID{a, b, c} {
		if !e.isDNC(id) {
			t.Errorf("%s mất cờ sau chuỗi gộp", id)
		}
	}
}

// Hai gộp ngược chiều song song, một bên có cờ: dù bên nào thắng, gốc còn lại mang cờ.
func TestMergeDoNotContactConcurrent(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	for i := range 30 {
		ids := e.people("A", "B")
		if err := e.setDNC(e.tenant, ids[i%2], true); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = e.merge(ids[0], ids[1]) })
		wg.Go(func() { _, _ = e.merge(ids[1], ids[0]) })
		wg.Wait()
		if !e.isDNC(ids[0]) || !e.isDNC(ids[1]) {
			t.Fatalf("vòng %d: mất cờ: %v %v", i, e.isDNC(ids[0]), e.isDNC(ids[1]))
		}
	}
}

type customerRef struct{ ID uuid.UUID }
