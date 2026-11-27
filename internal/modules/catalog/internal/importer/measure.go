package importer

import "math"

// MeasureMmFromCm đổi số đo cm (số thực của JSON: API, data.json của website) sang mm số nguyên (Task 517); ok false
// khi cm có hơn một chữ số thập phân, ngoài [MinMeasureMm, MaxMeasureMm] mm, hay không phải số hữu hạn. Sai số biểu
// diễn số thực (72.49999999999 của 72,5) dưới 1e-6 mm được bỏ qua.
func MeasureMmFromCm(cm float64) (int, bool) {
	x := cm * 10
	mm := math.Round(x)
	if !(math.Abs(x-mm) < 1e-6) || mm < MinMeasureMm || mm > MaxMeasureMm { // NaN, ±Inf: so sánh sai
		return 0, false
	}
	return int(mm), true
}
