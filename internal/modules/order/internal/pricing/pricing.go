// Package pricing tính tiền đơn: tiền dòng, giảm dòng, giảm cấp đơn và mã giảm giá chia về từng dòng, phí giao, tổng
// đơn, tỷ lệ giảm tay và tiền hoàn từng phần. Thuần Go, không database; là chỗ duy nhất tính tổng đơn (Global
// Constraints của kế hoạch M2). Tiền là int64 đồng trong [0, MaxMoney]; mọi phép chia làm tròn xuống; phần lẻ khi chia
// giảm cấp đơn dồn vào dòng có Base lớn nhất (Q4). Màn hình (Task M2-27) làm cùng thuật toán, kiểm bằng
// testdata/vectors.json.
package pricing

import (
	"cmp"
	"errors"
	"math"
	"math/bits"
	"slices"
)

// MaxMoney là số tiền lớn nhất (2^53 − 1 đồng, số nguyên lớn nhất JavaScript đọc đúng); bằng catalog.MaxMoney.
const MaxMoney int64 = 1<<53 - 1

// Lỗi của Compute. Gói order bọc thành sentinel của mình ở order/errors.go (ErrMoneyOverflow → ErrTotalOverflow).
var (
	// ErrLineDiscountExceeds: giảm dòng lớn hơn tiền dòng (Gross).
	ErrLineDiscountExceeds = errors.New("pricing: giảm dòng lớn hơn tiền dòng")
	// ErrOrderDiscountExceeds: giảm cấp đơn cộng mã giảm giá lớn hơn tổng tiền dòng sau giảm dòng (Σ Base).
	ErrOrderDiscountExceeds = errors.New("pricing: giảm cấp đơn lớn hơn tiền hàng sau giảm dòng")
	// ErrNegative: số lượng, đơn giá, một khoản giảm hay phí giao âm.
	ErrNegative = errors.New("pricing: số âm")
	// ErrMoneyOverflow: một số đầu vào, tích số lượng × đơn giá, hay một tổng vượt MaxMoney.
	ErrMoneyOverflow = errors.New("pricing: tiền vượt giới hạn")
)

// Line là một dòng đơn. Key do bên gọi đặt để ghép kết quả với dòng (trả lại nguyên trong LineTotals).
type Line struct {
	Key                     int
	Qty                     int64
	UnitPrice, LineDiscount int64
}

// Input là đầu vào của Compute. OrderDiscount là giảm tay cấp đơn; CouponDiscount là tiền của mã giảm giá.
type Input struct {
	Lines                                      []Line
	OrderDiscount, CouponDiscount, ShippingFee int64
}

// LineTotals là tiền của một dòng: Gross = Qty × UnitPrice, Base = Gross − LineDiscount, Allocated là phần giảm cấp
// đơn và mã chia về dòng, Net = Base − Allocated (doanh thu dòng, căn cứ tiền hoàn).
type LineTotals struct {
	Key                         int
	Gross, Base, Allocated, Net int64
}

// Totals là tiền của đơn. Subtotal = Σ Gross; DiscountTotal = LineDiscountTotal + OrderDiscount + CouponDiscount;
// GrandTotal = Subtotal − DiscountTotal + ShippingFee = Σ Net + ShippingFee. Lines cùng thứ tự với Input.Lines.
type Totals struct {
	Subtotal, LineDiscountTotal, OrderDiscount, CouponDiscount, DiscountTotal, ShippingFee, GrandTotal int64
	Lines                                                                                              []LineTotals
}

// Compute tính tiền đơn. Thứ tự kiểm: số âm (ErrNegative), số vượt MaxMoney (ErrMoneyOverflow), rồi từng dòng theo
// thứ tự (tích tràn, giảm dòng lớn hơn Gross, tạm tính cộng tới dòng đó tràn), rồi giảm cấp đơn cộng mã tràn, giảm cấp
// đơn quá Σ Base, tổng đơn tràn. Lỗi thì Totals rỗng.
func Compute(in Input) (Totals, error) {
	if err := validate(in); err != nil {
		return Totals{}, err
	}
	t := Totals{
		OrderDiscount: in.OrderDiscount, CouponDiscount: in.CouponDiscount, ShippingFee: in.ShippingFee,
		Lines: make([]LineTotals, len(in.Lines)),
	}
	bases := make([]int64, len(in.Lines))
	for i, l := range in.Lines {
		gross, ok := mulMoney(l.Qty, l.UnitPrice)
		if !ok {
			return Totals{}, ErrMoneyOverflow
		}
		if l.LineDiscount > gross {
			return Totals{}, ErrLineDiscountExceeds
		}
		if t.Subtotal, ok = addMoney(t.Subtotal, gross); !ok {
			return Totals{}, ErrMoneyOverflow
		}
		t.LineDiscountTotal += l.LineDiscount // ≤ Subtotal ≤ MaxMoney
		bases[i] = gross - l.LineDiscount
		t.Lines[i] = LineTotals{Key: l.Key, Gross: gross, Base: bases[i]}
	}
	orderLevel, ok := addMoney(in.OrderDiscount, in.CouponDiscount)
	if !ok {
		return Totals{}, ErrMoneyOverflow
	}
	if orderLevel > t.Subtotal-t.LineDiscountTotal {
		return Totals{}, ErrOrderDiscountExceeds
	}
	for i, a := range Allocate(bases, orderLevel) {
		t.Lines[i].Allocated = a
		t.Lines[i].Net = bases[i] - a
	}
	t.DiscountTotal = t.LineDiscountTotal + orderLevel // ≤ Subtotal
	if t.GrandTotal, ok = addMoney(t.Subtotal-t.DiscountTotal, in.ShippingFee); !ok {
		return Totals{}, ErrMoneyOverflow
	}
	return t, nil
}

func validate(in Input) error {
	nums := make([]int64, 0, 3*len(in.Lines)+3)
	for _, l := range in.Lines {
		nums = append(nums, l.Qty, l.UnitPrice, l.LineDiscount)
	}
	nums = append(nums, in.OrderDiscount, in.CouponDiscount, in.ShippingFee)
	if slices.ContainsFunc(nums, func(x int64) bool { return x < 0 }) {
		return ErrNegative
	}
	if slices.ContainsFunc(nums, func(x int64) bool { return x > MaxMoney }) {
		return ErrMoneyOverflow
	}
	return nil
}

// Allocate chia amount về các dòng theo tỷ lệ bases: dòng i được floor(bases[i] × amount / Σ bases) (tích tính 128
// bit, không tràn). Phần dư cộng vào dòng có base lớn nhất (bằng nhau thì chỉ số nhỏ nhất), nhưng không quá base của
// dòng; phần còn lại (chỉ xảy ra khi base rất nhỏ so với số dòng) sang dòng lớn kế tiếp theo cùng thứ tự (Q-T64-1).
// Kết quả: Σ = amount và 0 ≤ kết quả[i] ≤ bases[i]. Σ bases = 0 → toàn 0.
//
// Điều kiện của bên gọi (Compute bảo đảm): mọi base ≥ 0, 0 ≤ amount ≤ Σ bases ≤ MaxMoney. Sai thì panic (lỗi lập
// trình, không phải lỗi dữ liệu).
func Allocate(bases []int64, amount int64) []int64 {
	out := make([]int64, len(bases))
	var sum int64
	for _, b := range bases {
		if b < 0 || b > MaxMoney-sum {
			panic("pricing.Allocate: base âm hoặc Σ base vượt MaxMoney")
		}
		sum += b
	}
	if amount < 0 || amount > sum {
		panic("pricing.Allocate: amount ngoài [0, Σ base]")
	}
	if amount == 0 {
		return out
	}
	rest := amount
	for i, b := range bases {
		out[i] = mulDiv(b, amount, sum) // b ≤ sum nên thương ≤ amount
		rest -= out[i]
	}
	order := make([]int, len(bases))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(bases[j], bases[i]) })
	for _, i := range order {
		if rest == 0 {
			break
		}
		add := min(rest, bases[i]-out[i])
		out[i] += add
		rest -= add
	}
	return out
}

// ManualDiscountBP là tỷ lệ giảm tay (giảm dòng + giảm cấp đơn, không tính mã giảm giá; Q3) trên tạm tính, đơn vị
// 1/10.000, làm tròn lên: ceil((LineDiscountTotal + OrderDiscount) × 10000 / Subtotal). Không giảm tay (kể cả
// Subtotal = 0) → 0. t lấy từ Compute nên kết quả trong [0, 10000]. Totals không thể có từ Compute (số âm, có giảm mà
// Subtotal = 0, tỷ lệ vượt int64) trả math.MaxInt64: luôn vượt ngưỡng duyệt, không bao giờ lọt qua duyệt.
func ManualDiscountBP(t Totals) int64 {
	if t.Subtotal < 0 || t.LineDiscountTotal < 0 || t.OrderDiscount < 0 {
		return math.MaxInt64
	}
	manual := uint64(t.LineDiscountTotal) + uint64(t.OrderDiscount) // hai số < 2^63 nên tổng < 2^64
	if manual == 0 {
		return 0
	}
	if t.Subtotal == 0 {
		return math.MaxInt64
	}
	hi, lo := bits.Mul64(manual, 10000)
	if hi >= uint64(t.Subtotal) {
		return math.MaxInt64
	}
	q, r := bits.Div64(hi, lo, uint64(t.Subtotal))
	if q >= math.MaxInt64 {
		return math.MaxInt64
	}
	if r > 0 {
		q++
	}
	return int64(q)
}

// mulDiv trả floor(a × b / c), tích tính 128 bit. Điều kiện (bên gọi bảo đảm): a, b ≥ 0, c > 0 và a ≤ c hoặc b ≤ c,
// nên thương không vượt số còn lại, vừa int64 và bits.Div64 không panic.
func mulDiv(a, b, c int64) int64 {
	hi, lo := bits.Mul64(uint64(a), uint64(b)) //nolint:gosec // G115: a, b ≥ 0 theo điều kiện của hàm
	q, _ := bits.Div64(hi, lo, uint64(c))      //nolint:gosec // G115: c > 0 theo điều kiện của hàm
	return int64(q)                            //nolint:gosec // G115: q ≤ max(a, b) ≤ MaxInt64
}

// mulMoney nhân hai số không âm; tích vượt MaxMoney → false.
func mulMoney(a, b int64) (int64, bool) {
	hi, lo := bits.Mul64(uint64(a), uint64(b)) //nolint:gosec // G115: Compute đã kiểm a, b ≥ 0 (validate)
	if hi != 0 || lo > uint64(MaxMoney) {
		return 0, false
	}
	return int64(lo), true
}

// addMoney cộng hai số trong [0, MaxMoney]; tổng vượt MaxMoney → false.
func addMoney(a, b int64) (int64, bool) {
	if a > MaxMoney-b {
		return 0, false
	}
	return a + b, true
}
