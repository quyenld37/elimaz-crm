package content_test

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/content"
)

// Quy luật (6.11): mọi danh sách ValidateLooks nhận đều có ≤ 500 bộ, ≤ 6 bộ trên trang chủ, mỗi bộ 1–3 món khác nhau,
// chấm trong 0–100, ảnh là ảnh của một món trong bộ. Danh sách sinh ngẫu nhiên quanh các biên (số bộ, số trang chủ,
// số món, món trùng, chấm ngoài ảnh, ảnh ngoài bộ).
func TestValidateLooks_Property(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	pool := []uuid.UUID{r.pA, r.pB, r.pOut, r.pStop}
	photos := []uuid.UUID{r.photoA, r.photoB, r.photoPending, r.photoOther}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.SampledFrom([]int{0, 1, 5, 6, 7, 12, 499, 500, 501}).Draw(rt, "looks")
		homeP := rapid.IntRange(0, 100).Draw(rt, "home_pct")
		list := make([]content.Look, n)
		for i := range list {
			l := content.Look{ID: uuid.New(), Name: "Dịp", OnHome: rapid.IntRange(0, 99).Draw(rt, "home") < homeP}
			for range rapid.IntRange(0, 4).Draw(rt, "items") {
				l.Items = append(l.Items, content.LookItem{
					ProductID: rapid.SampledFrom(pool).Draw(rt, "product"),
					X:         rapid.Float64Range(-10, 110).Draw(rt, "x"),
					Y:         rapid.Float64Range(-10, 110).Draw(rt, "y"),
				})
			}
			l.PhotoFileID = rapid.SampledFrom(photos).Draw(rt, "photo")
			list[i] = l
		}
		norm := content.NormalizeLooks(list)
		if content.ValidateLooks(norm, r.ref, nil) != nil {
			return
		}
		if len(norm) > content.MaxLooks {
			rt.Fatalf("nhận %d bộ", len(norm))
		}
		home := 0
		for _, l := range norm {
			if l.OnHome {
				home++
			}
			checkSavable(rt, l, r.ref)
		}
		if home > content.MaxHomeLooks {
			rt.Fatalf("nhận %d bộ trang chủ", home)
		}
	})
}

// checkSavable: bộ l mà ValidateLooks nhận có 1–3 món khác nhau, chấm trong 0–100, ảnh là ảnh của một món.
func checkSavable(rt *rapid.T, l content.Look, ref content.LookRefData) {
	seen := map[uuid.UUID]bool{}
	if len(l.Items) < 1 || len(l.Items) > content.MaxLookItems {
		rt.Fatalf("nhận bộ %d món", len(l.Items))
	}
	for _, it := range l.Items {
		if seen[it.ProductID] {
			rt.Fatalf("nhận món trùng %s", it.ProductID)
		}
		seen[it.ProductID] = true
		if it.X < 0 || it.X > 100 || it.Y < 0 || it.Y > 100 {
			rt.Fatalf("nhận chấm (%v, %v)", it.X, it.Y)
		}
	}
	for _, im := range ref.Images {
		if im.FileID == l.PhotoFileID && seen[im.ProductID] {
			return
		}
	}
	rt.Fatalf("nhận ảnh %s không thuộc món của bộ", l.PhotoFileID)
}
