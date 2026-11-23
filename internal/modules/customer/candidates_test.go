package customer_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func (e *writeEnv) candidates(tenant, id uuid.UUID) ([]customer.Candidate, error) {
	var out []customer.Candidate
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		out, err = customer.MergeCandidates(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *writeEnv) mustCandidates(id uuid.UUID) []customer.Candidate {
	e.t.Helper()
	out, err := e.candidates(e.tenant, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func candidateRoots(cs []customer.Candidate) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.RootID)
	}
	return out
}

// Gợi ý: địa chỉ của A có SĐT 0987654321 là danh tính của B → MergeCandidates(A) có B (mã, tên, chỉ số của gốc B),
// MatchedPhones chứa SĐT đó; sau khi gộp thì không còn. SĐT danh tính của chính nhóm A không gợi ý A.
func TestMergeCandidatesByAddressPhone(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị A", Phone: "0911222333"})
	b := e.create(customer.CreateInput{FullName: "Chị B", Phone: "0987654321"})
	e.create(customer.CreateInput{FullName: "Người lạ", Phone: "0900000009"})
	in := addrIn("T01001")
	in.Phone = "0987 654 321"
	if _, err := e.addAddress(a.ID, in, true); err != nil {
		t.Fatal(err)
	}
	// Chỉ số đọc ở gốc B.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE customer.customers SET order_count = 3, total_spent = 450000 WHERE id = $1", b.ID)
		return err //nolint:wrapcheck // test
	})
	got := e.mustCandidates(a.ID)
	if len(got) != 1 || got[0].RootID != b.ID || got[0].Code != b.Code || string(got[0].FullName) != "Chị B" ||
		!slices.Equal(got[0].MatchedPhones, []logging.Phone{"+84987654321"}) || got[0].Stats.OrderCount != 3 ||
		got[0].Stats.TotalSpent != 450000 {
		t.Fatalf("gợi ý: %+v", got)
	}
	// Tenant khác: không thấy A → ErrNotFound.
	if _, err := e.candidates(e.other, a.ID); err == nil {
		t.Fatal("tenant khác phải không thấy khách")
	}
	e.mustMerge(a.ID, b.ID)
	if got := e.mustCandidates(a.ID); len(got) != 0 {
		t.Fatalf("sau gộp vẫn gợi ý: %+v", got)
	}
}

// Gợi ý theo SĐT người nhận của 50 đơn gần nhất của cả nhóm: đơn của hồ sơ con cũng tính; đơn thứ 51 trở về trước
// không tính; khách khớp hai SĐT có MatchedPhones đủ hai (theo thứ tự); SĐT danh tính của hồ sơ con thuộc nhóm (gộp
// rồi) khớp nhau thì gom về một gốc.
func TestMergeCandidatesByRecentOrders(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "A"})
	a2 := e.create(customer.CreateInput{FullName: "A2"})
	e.mustMerge(a.ID, a2.ID)
	c := e.create(customer.CreateInput{FullName: "C", Phone: "0933000001"})
	c2 := e.create(customer.CreateInput{FullName: "C2", Phone: "0933000002"})
	e.mustMerge(c.ID, c2.ID)
	old := e.create(customer.CreateInput{FullName: "Cũ", Phone: "0944000001"})
	base := e.now.Add(-100 * time.Hour)
	// Đơn cũ nhất mang SĐT của "Cũ"; sau đó 50 đơn mới hơn → đơn cũ ra khỏi cửa sổ 50.
	e.insertOrder(a.ID, "+84944000001", base)
	for i := range 48 {
		e.insertOrder(a.ID, fmt.Sprintf("+8495500%04d", i), base.Add(time.Duration(i+1)*time.Hour))
	}
	e.insertOrder(a2.ID, "+84933000002", base.Add(60*time.Hour))
	e.insertOrder(a.ID, "+84933000001", base.Add(61*time.Hour))
	got := e.mustCandidates(a2.ID)
	if !slices.Equal(candidateRoots(got), []uuid.UUID{c.ID}) {
		t.Fatalf("gợi ý: %v (cũ %s)", candidateRoots(got), old.ID)
	}
	if !slices.Equal(got[0].MatchedPhones, []logging.Phone{"+84933000001", "+84933000002"}) {
		t.Fatalf("SĐT khớp: %v", got[0].MatchedPhones)
	}
}
