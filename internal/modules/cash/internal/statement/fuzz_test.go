package statement

import (
	"strconv"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// FuzzParseAmount: chuỗi bất kỳ không làm sập; số đọc được nằm trong ±2^53 và viết lại dạng liền đọc lại đúng số đó.
func FuzzParseAmount(f *testing.F) {
	for _, s := range []string{"1.234.567", "1,234,567", "1234567", "1234567.0", "12,5", "-25.000", "9007199254740993", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := ParseAmount(s)
		if err != nil {
			return
		}
		if n > maxAmount || n < -maxAmount {
			t.Fatalf("ParseAmount(%q) = %d ngoài ±2^53", s, n)
		}
		if back, err := ParseAmount(strconv.FormatInt(n, 10)); err != nil || back != n {
			t.Fatalf("ParseAmount(%q) = %d, viết lại đọc ra %d, %v", s, n, back, err)
		}
	})
}

// FuzzParseRow: dòng dữ liệu bất kỳ (ô ngăn bằng \x1f, công thức ở cột chỉ số formula) theo cột của elimaz_v1 không
// làm sập; dòng đọc được có mã vận đơn, COD và phí không âm, mọi số trong ±2^53, thực nhận đúng ô thực nhận; lỗi luôn
// là *ParseError có mã và đúng số dòng.
func FuzzParseRow(f *testing.F) {
	f.Add("FK0001\x1f630.000\x1f32.000\x1f0\x1f598.000\x1f05/11/2026", -1, true)
	f.Add("\x1f630000\x1f0\x1f\x1f1", 1, false)
	f.Add("X\x1f12,5\x1f0\x1f0\x1f0\x1f46331", 5, true)
	reg, err := formats.NewRegistry(nil)
	if err != nil {
		f.Fatal(err)
	}
	fm, _ := reg.Get(formats.ElimazV1Key)
	cols := map[fulfillment.StatementField]int{
		fulfillment.FieldTrackingCode: 0, fulfillment.FieldCOD: 1, fulfillment.FieldFee: 2, fulfillment.FieldOtherFee: 3,
		fulfillment.FieldNet: 4, fulfillment.FieldPaidOn: 5,
	}
	lim := DefaultLimits()
	f.Fuzz(func(t *testing.T, joined string, formula int, isXlsx bool) {
		r := rawRow{number: 17, cells: strings.Split(joined, "\x1f")}
		if formula >= 0 {
			r.formulas = []int{formula}
		}
		l, err := parseRow(r, cols, fm, lim, isXlsx)
		if err != nil {
			pe, ok := err.(*ParseError) //nolint:errorlint // parseRow trả thẳng *ParseError
			if !ok || pe.Code == "" || pe.Line != 17 {
				t.Fatalf("lỗi %v", err)
			}
			return
		}
		in := func(n int64) bool { return n >= -maxAmount && n <= maxAmount }
		if l.TrackingCode == "" || l.COD < 0 || l.Fee < 0 || !in(l.COD) || !in(l.Fee) || !in(l.OtherFee) || !in(l.Net) ||
			l.LineNo != 17 {
			t.Fatalf("dòng %+v từ %q", l, joined)
		}
		if want, _ := ParseAmount(r.cells[4]); l.Net != want {
			t.Fatalf("thực nhận %d, ô %q", l.Net, r.cells[4])
		}
	})
}
