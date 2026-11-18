package files

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
