package order

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Quy luật của qtySplit (Task M3-23): chỉ nhận khi mỗi dòng phiếu có đúng một dòng nhận và S+D+M = số của dòng (mọi
// số ≥ 0); khi nhận, mỗi dòng ra giữ nguyên ba số và tình trạng là tên loại duy nhất khác 0, trộn thì mixed.
func TestQtySplit_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		items, lines := drawSplit(rt)
		lines, want := breakSplit(rt, lines)
		got, err := qtySplit(items, lines)
		if !errors.Is(err, want) || (want != nil) != (err != nil) {
			rt.Fatalf("lỗi = %v, muốn %v", err, want)
		}
		if err != nil {
			return
		}
		for i, g := range got {
			if g.item.ID != items[i].ID || g.ReceiveQtyLine != lines[i] {
				rt.Fatalf("dòng %d: %+v", i, g)
			}
			if c, w := g.condition(), wantCondition(g.ReceiveQtyLine); c != w {
				rt.Fatalf("dòng %d (%+v): tình trạng %s, muốn %s", i, g.ReceiveQtyLine, c, w)
			}
		}
	})
}

// drawSplit rút 1–5 dòng phiếu (1–6 cái) và một cách chia đúng cho mỗi dòng.
func drawSplit(rt *rapid.T) ([]store.LockReturnItemsRow, []ReceiveQtyLine) {
	n := rapid.IntRange(1, 5).Draw(rt, "dòng")
	items := make([]store.LockReturnItemsRow, n)
	lines := make([]ReceiveQtyLine, n)
	for i := range items {
		q := rapid.Int32Range(1, 6).Draw(rt, "số")
		s := rapid.Int32Range(0, q).Draw(rt, "tốt")
		d := rapid.Int32Range(0, q-s).Draw(rt, "hỏng")
		items[i] = store.LockReturnItemsRow{ID: uuid.New(), OrderItemID: uuid.New(), Quantity: q}
		lines[i] = ReceiveQtyLine{OrderItemID: items[i].OrderItemID, Sellable: s, Damaged: d, Missing: q - s - d}
	}
	return items, lines
}

// breakSplit làm hỏng lines theo một kiểu rút ngẫu nhiên (hoặc giữ nguyên) và trả lỗi mong đợi.
func breakSplit(rt *rapid.T, lines []ReceiveQtyLine) ([]ReceiveQtyLine, error) {
	switch rapid.IntRange(0, 5).Draw(rt, "hỏng kiểu") {
	case 1:
		return lines[1:], ErrQtyMismatch
	case 2:
		return append(lines, lines[0]), ErrQtyMismatch
	case 3:
		lines[0].OrderItemID = uuid.New()
		return lines, ErrQtyMismatch
	case 4:
		lines[0].Sellable++
		return lines, ErrQtyMismatch
	case 5:
		lines[0].Sellable, lines[0].Damaged = lines[0].Sellable+1, -1
		return lines, ErrInvalidInput
	default:
		return lines, nil
	}
}

// wantCondition: tên loại duy nhất có số khác 0, từ hai loại → mixed.
func wantCondition(l ReceiveQtyLine) string {
	kinds, name := 0, ""
	for k, v := range map[string]int32{ConditionSellable: l.Sellable, ConditionDamaged: l.Damaged, ConditionMissing: l.Missing} {
		if v > 0 {
			kinds, name = kinds+1, k
		}
	}
	if kinds > 1 {
		return ConditionMixed
	}
	return name
}
