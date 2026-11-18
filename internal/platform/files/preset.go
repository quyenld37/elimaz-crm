package files

import "slices"

// VariantSpec là một biến thể của bộ kích thước: chiều rộng (px) và định dạng (avif, webp, jpeg).
type VariantSpec struct {
	Width  int
	Format string
}

// Preset trả bộ kích thước của phiên bản version (Q13): v1 = rộng 320, 640, 1080, 1600 × AVIF, WebP, JPEG (12 biến
// thể). Thêm cỡ là thêm phiên bản mới (TP07), không sửa phiên bản đã có ảnh.
func Preset(version int) ([]VariantSpec, bool) {
	if version != 1 {
		return nil, false
	}
	out := make([]VariantSpec, 0, 12)
	for _, w := range []int{320, 640, 1080, 1600} {
		for _, f := range []string{"avif", "webp", "jpeg"} {
			out = append(out, VariantSpec{Width: w, Format: f})
		}
	}
	return out, true
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
