package files

import (
	"slices"
	"sync/atomic"
)

// VariantSpec là một biến thể của bộ kích thước: chiều rộng (px), định dạng (avif, webp, jpeg) và chất lượng (0 = mặc
// định của imager).
type VariantSpec struct {
	Width   int
	Format  string
	Quality int
}

// purposePresetBase: bộ kích thước riêng của một mục đích (Purpose.Preset) đánh số từ đây, không lẫn với bộ chung
// IMAGE_PRESET_VERSION (1, 2, …).
const purposePresetBase = 1000

// PresetBannerV1 là bộ kích thước của banner trang chủ (Task 453, như website cũ): rộng 1200 và 2400 × WebP, JPEG chất
// lượng 82 (không phóng to: ảnh nhỏ hơn giữ cỡ gốc).
const PresetBannerV1 = 1001

// presets là bảng hằng các bộ kích thước (TP07): v1 = rộng 320, 640, 1080, 1600 × AVIF, WebP, JPEG (Q13, 12 biến
// thể); PresetBannerV1 là bộ riêng của mục đích banner (Task 453). Thêm cỡ là thêm một phiên bản mới vào bảng này, không sửa phiên bản đã có ảnh; worker tạo bản mới từ ảnh gốc
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
	var banner []VariantSpec
	for _, w := range []int{1200, 2400} {
		for _, f := range []string{"webp", "jpeg"} {
			banner = append(banner, VariantSpec{Width: w, Format: f, Quality: 82})
		}
	}
	var p atomic.Pointer[map[int][]VariantSpec]
	p.Store(&map[int][]VariantSpec{1: v1, PresetBannerV1: banner})
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
// danh sách và ảnh trong bài viết. Bộ riêng của một mục đích (≥ 1000, như PresetBannerV1) chỉ chọn đúng bộ đó.
func SelectPreset(vs []VariantURL, want int) []VariantURL {
	low := 1
	if want >= purposePresetBase {
		low = want // bộ riêng của mục đích (Task 453) không lùi về bộ chung
	}
	for v := want; v >= low; v-- {
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
