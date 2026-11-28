package catalog_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// expectedReasons là lý do mà view phải trả cho mẫu nháp thiếu lack (thứ tự cố định của catalog.product_readiness_v).
func expectedReasons(l shoptest.Lack) []string {
	out := []string{}
	add := func(cond bool, r string) {
		if cond {
			out = append(out, r)
		}
	}
	add(l.NoVariant, "no_variant")
	add(l.ZeroPrice && !l.NoVariant, "no_price")     // giá 0 chỉ tính trên biến thể active
	add(l.NoImage && !l.ProcessingImage, "no_image") // DraftProduct: ảnh đang xử lý thắng NoImage
	add(l.ProcessingImage, "image_processing")
	add(l.NoCategory, "no_category")
	add(l.NoAttr, "missing_attr:"+shoptest.ShelfAttrCode)
	add(l.NoTag, "no_tag")
	return out
}

// Quy luật (rapid, 50 lượt): tập mẫu nháp ngẫu nhiên với phần thiếu ngẫu nhiên → view trả đúng lý do; sau
// Publish(allReady) mọi mẫu mới active đều không có lý do và mọi mẫu có lý do vẫn nháp; skipped của lượt đúng bằng mọi
// mẫu nháp còn lại (mẫu của lượt trước đã hiện thì không lặp lại).
func TestLaunch_PublishProperty(t *testing.T) {
	// Không t.Parallel: propertyChecks đổi cờ rapid.checks của cả tiến trình.
	propertyChecks(t, 50)
	e := newLaunchEnv(t)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		lacks := e.drawDrafts(rt, run)
		ids := make([]uuid.UUID, 0, len(lacks))
		for id := range lacks {
			ids = append(ids, id)
		}
		before := e.readiness(ids)
		for id, l := range lacks {
			if got, want := reasonsOf(before, id), expectedReasons(l); !slices.Equal(got, want) {
				rt.Fatalf("lý do của %+v: %v, mong %v", l, got, want)
			}
		}
		out := e.mustPublish(catalog.PublishInput{AllReady: true})
		checkPublished(rt, out, e.readiness(ids), lacks)
		if drafts := len(e.readiness(nil)); len(out.Skipped) != drafts {
			rt.Fatalf("skipped %d, còn %d mẫu nháp", len(out.Skipped), drafts)
		}
	})
}

// drawDrafts tạo 1–8 mẫu nháp (người bán ngẫu nhiên) với phần thiếu ngẫu nhiên (mỗi thứ 15 %) và trả phần thiếu theo mã.
func (e *launchEnv) drawDrafts(rt *rapid.T, run int) map[uuid.UUID]shoptest.Lack {
	miss := func(label string) bool { return rapid.Float64Range(0, 1).Draw(rt, label) < 0.15 }
	lacks := map[uuid.UUID]shoptest.Lack{}
	for i := range rapid.IntRange(1, 8).Draw(rt, "n") {
		l := shoptest.Lack{
			NoVariant: miss("nv"), ZeroPrice: miss("zp"), NoImage: miss("ni"), ProcessingImage: miss("pi"),
			NoCategory: miss("nc"), NoAttr: miss("na"), NoTag: miss("nt"),
		}
		seller := e.seller
		if rapid.Bool().Draw(rt, "partner") {
			seller = e.partner
		}
		lacks[e.draft(seller, fmt.Sprintf("R%dX%d", run, i), l)] = l
	}
	return lacks
}

// checkPublished: skipped chỉ có mẫu còn lý do; mẫu đủ điều kiện đã active và ở published; mẫu có lý do vẫn nháp.
func checkPublished(rt *rapid.T, out catalog.PublishResult, after map[uuid.UUID]catalog.ProductReadiness,
	lacks map[uuid.UUID]shoptest.Lack,
) {
	for _, s := range out.Skipped {
		if len(s.Reasons) == 0 || s.Ready {
			rt.Fatalf("bỏ qua mẫu đủ điều kiện: %+v", s)
		}
	}
	for id, l := range lacks {
		r, ready, published := after[id], len(expectedReasons(l)) == 0, slices.Contains(out.Published, id)
		if ready && (r.Status != catalog.StatusActive || !published) {
			rt.Fatalf("mẫu đủ điều kiện %s không hiện: %+v", id, r)
		}
		if !ready && (r.Status != catalog.StatusDraft || published) {
			rt.Fatalf("mẫu có lý do %v bị hiện: %+v", expectedReasons(l), r)
		}
	}
}
