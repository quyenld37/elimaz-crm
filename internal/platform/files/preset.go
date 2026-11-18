package files

import (
	"slices"
	"sync/atomic"
)

// VariantSpec là một biến thể của bộ kích thước: chiều rộng (px) và định dạng (avif, webp, jpeg).
type VariantSpec struct {
	Width  int
	Format string
}

// presets là bảng hằng các bộ kích thước (TP07): v1 = rộng 320, 640, 1080, 1600 × AVIF, WebP, JPEG (Q13, 12 biến
// thể). Thêm cỡ là thêm một phiên bản mới vào bảng này, không sửa phiên bản đã có ảnh; worker tạo bản mới từ ảnh gốc
// bằng lệnh file.regenerate. Mã chạy thật chỉ đọc; con trỏ nguyên tử để test thay cả bảng (SetPresetsForTest) mà
// không đua với goroutine của worker.
//
//nolint:gochecknoglobals // bảng hằng, chỉ test thay
var presets = func() *atomic.Pointer[map[int][]VariantSpec] {
	v1 := make([]VariantSpec, 0, 12)
	for _, w := range []int{320, 640, 1080, 1600} {
		for _, f := range []string{"avif", "webp", "jpeg"} {
			v1 = append(v1, VariantSpec{Width: w, Format: f})
		}
	}
	var p atomic.Pointer[map[int][]VariantSpec]
	p.Store(&map[int][]VariantSpec{1: v1})
	return &p
}()

// Preset trả bộ kích thước của phiên bản version (bản sao, bên gọi sửa thoải mái); chưa có → false.
func Preset(version int) ([]VariantSpec, bool) {
	specs, ok := (*presets.Load())[version]
	if !ok {
		return nil, false
	}
	return slices.Clone(specs), true
}

// SelectPreset chọn biến thể của một bộ kích thước trong vs (biến thể của một tệp, như PublicVariants trả): bản want
// nếu đã đủ mọi biến thể của Preset(want), không thì lùi về bản nhỏ hơn gần nhất đã đủ (IMAGE_PRESET_VERSION mới mà
// ảnh chưa tạo lại xong vẫn hiện bản cũ, TP07). Không bản nào ≤ want đủ → nil. Luật chung của getProduct, ảnh bìa
// danh sách và ảnh trong bài viết.
func SelectPreset(vs []VariantURL, want int) []VariantURL {
	for v := want; v >= 1; v-- {
		specs, ok := Preset(v)
		if !ok {
			continue
		}
		var out []VariantURL
		for _, x := range vs {
			if x.Preset == v && slices.ContainsFunc(specs, func(s VariantSpec) bool { return s.Width == x.Width && s.Format == x.Format }) {
				out = append(out, x)
			}
		}
		if complete(out, specs) {
			return out
		}
	}
	return nil
}

// complete: out có đủ mọi biến thể của specs (mỗi biến thể ít nhất một lần).
func complete(out []VariantURL, specs []VariantSpec) bool {
	for _, s := range specs {
		if !slices.ContainsFunc(out, func(x VariantURL) bool { return x.Width == s.Width && x.Format == s.Format }) {
			return false
		}
	}
	return true
}
