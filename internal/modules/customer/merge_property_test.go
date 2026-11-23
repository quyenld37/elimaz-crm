package customer_test

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// applied là một lần gộp chưa hoàn tác của mô hình: ánh xạ root_id ngay trước lần gộp; exact = false khi đã có lần
// hoàn tác không theo thứ tự chạy sau nó (ánh xạ trước không còn so được nguyên vẹn).
type applied struct {
	id     uuid.UUID
	before map[uuid.UUID]uuid.UUID
	exact  bool
}

// mergeModel là một lượt rapid: các hồ sơ của lượt và chồng lần gộp chưa hoàn tác (lần gần nhất ở cuối).
type mergeModel struct {
	e     *writeEnv
	rt    *rapid.T
	ids   []uuid.UUID
	stack []applied
}

// Quy luật gộp (rapid, 200 lượt — TestMain; quy trình mục 6 câu 11): chuỗi ngẫu nhiên gộp, hoàn tác lần gần nhất, hoàn
// tác một lần bất kỳ trên 2–8 hồ sơ → sau mỗi bước (1) mọi root_id trỏ tới một hồ sơ gốc; (2) hoàn tác lần gần nhất trả
// đúng ánh xạ root_id trước lần gộp đó; (3) số dòng customers không đổi. Gộp hai hồ sơ cùng gốc → merge_same_customer,
// khác gốc → cả nhóm bị gộp sang gốc giữ lại; hoàn tác lần không phải gần nhất → thành công hoặc merge_not_latest
// (không đổi gì).
func TestMergeUndoProperty(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		m := &mergeModel{e: e, rt: rt}
		for i := range rapid.IntRange(2, 8).Draw(rt, "profiles") {
			m.ids = append(m.ids, e.create(customer.CreateInput{FullName: fmt.Sprintf("Khách %d", i)}).ID)
		}
		rows := e.count(`SELECT count(*) FROM customer.customers`)
		for range rapid.IntRange(1, 12).Draw(rt, "steps") {
			cur := e.rootsOf(m.ids)
			switch rapid.SampledFrom([]string{"merge", "merge", "undoLatest", "undoAny"}).Draw(rt, "op") {
			case "merge":
				m.merge(cur)
			case "undoLatest":
				m.undoLatest()
			case "undoAny":
				m.undoAny(cur)
			}
			if msg := rootsValid(e.rootsOf(m.ids)); msg != "" {
				rt.Fatal(msg)
			}
			if n := e.count(`SELECT count(*) FROM customer.customers`); n != rows {
				rt.Fatalf("số dòng customers: %d → %d", rows, n)
			}
		}
	})
}

// merge gộp hai hồ sơ ngẫu nhiên: cùng gốc → merge_same_customer, không đổi gì; khác gốc → mọi hồ sơ của gốc bị gộp
// sang gốc giữ lại, các hồ sơ khác giữ nguyên.
func (m *mergeModel) merge(cur map[uuid.UUID]uuid.UUID) {
	s := rapid.SampledFrom(m.ids).Draw(m.rt, "survivor")
	g := rapid.SampledFrom(m.ids).Draw(m.rt, "merged")
	got, err := m.e.merge(s, g)
	after := m.e.rootsOf(m.ids)
	if cur[s] == cur[g] {
		if !errors.Is(err, customer.ErrMergeSameCustomer) || !maps.Equal(after, cur) {
			m.rt.Fatalf("gộp cùng gốc: %v, %v → %v", err, cur, after)
		}
		return
	}
	if err != nil {
		m.rt.Fatalf("gộp %s vào %s: %v", g, s, err)
	}
	want := maps.Clone(cur)
	for id, r := range cur {
		if r == cur[g] {
			want[id] = cur[s]
		}
	}
	if !maps.Equal(after, want) {
		m.rt.Fatalf("sau gộp: muốn %v, có %v", want, after)
	}
	m.stack = append(m.stack, applied{id: got.ID, before: cur, exact: true})
}

// undoLatest hoàn tác lần gộp gần nhất: luôn được; trả đúng ánh xạ trước lần đó.
func (m *mergeModel) undoLatest() {
	if len(m.stack) == 0 {
		return
	}
	top := m.stack[len(m.stack)-1]
	if err := m.e.undo(top.id, m.e.now); err != nil {
		m.rt.Fatalf("hoàn tác lần gần nhất: %v", err)
	}
	m.stack = m.stack[:len(m.stack)-1]
	if after := m.e.rootsOf(m.ids); top.exact && !maps.Equal(after, top.before) {
		m.rt.Fatalf("hoàn tác lần gần nhất: muốn %v, có %v", top.before, after)
	}
}

// undoAny hoàn tác một lần gộp bất kỳ: được, hoặc merge_not_latest và không đổi gì.
func (m *mergeModel) undoAny(cur map[uuid.UUID]uuid.UUID) {
	if len(m.stack) == 0 {
		return
	}
	i := rapid.IntRange(0, len(m.stack)-1).Draw(m.rt, "undo")
	err := m.e.undo(m.stack[i].id, m.e.now)
	switch {
	case errors.Is(err, customer.ErrMergeNotLatest):
		if after := m.e.rootsOf(m.ids); !maps.Equal(after, cur) {
			m.rt.Fatalf("merge_not_latest mà vẫn đổi: %v → %v", cur, after)
		}
	case err != nil:
		m.rt.Fatalf("hoàn tác lần %d: %v", i, err)
	default:
		for j := i + 1; j < len(m.stack); j++ {
			m.stack[j].exact = false
		}
		m.stack = slices.Delete(m.stack, i, i+1)
	}
}
