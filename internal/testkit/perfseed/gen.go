package perfseed

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Phần sinh dữ liệu của seedperf (thuần, không database): SĐT giả, tên, mã, mã uuidv7 theo giờ cho trước, đơn và dòng
// đơn. Mọi lựa chọn đi qua một rand.Rand gieo từ Options.Seed: cùng hạt giống là cùng dữ liệu.

// Hằng của dải SĐT giả: phone(k) = "+8490" + 7 chữ số của (k·phoneMul + phoneAdd) mod 10⁷. phoneMul nguyên tố cùng
// nhau với 10⁷ nên k ↦ SĐT là song ánh trên [0, 10⁷): k khác nhau luôn ra SĐT khác nhau, đuôi số rải đều. k6
// (loadtest/k6/m2_common.js) tính lại đúng công thức này để tìm theo SĐT, đuôi số và tạo khách mới ngoài dải đã nạp.
const (
	phonePrefix = "+8490"
	PhoneSpace  = 10_000_000
	phoneMul    = 7_919
	phoneAdd    = 12_345
)

// fakePhone là SĐT giả thứ k (0 ≤ k < PhoneSpace), dạng chuẩn +84… như customer.NormalizePhone.
func fakePhone(k int) string {
	return fmt.Sprintf("%s%07d", phonePrefix, (k*phoneMul+phoneAdd)%PhoneSpace)
}

// reversedDigits là value_reversed của SĐT p (cùng hàm mã thật dùng khi ghi danh tính, để câu tìm đuôi số khớp).
func reversedDigits(p string) string { return customer.ReverseDigits(logging.Phone(p)) }

// foldName là name_search của tên (cùng hàm mã thật dùng khi ghi khách).
func foldName(s string) string { return textnorm.Fold(s) }

// Họ, đệm, tên (tên tiếng Việt có dấu; shop thời trang nữ nên tên nữ nhiều hơn). Họ theo tỷ lệ gần đúng ngoài đời: Nguyễn
// khoảng 38%, để câu tìm tên một từ phổ biến ("nguyen") đo đúng ca nặng nhất.
// nameParts trả họ, trọng số họ, đệm, tên (bảng hằng; hàm thay biến cấp gói).
func nameParts() (family []string, weights []int, middle, given []string) {
	return familyNames(), familyWeights(), middleNames(), givenNames()
}

func familyNames() []string {
	return []string{"Nguyễn", "Trần", "Lê", "Phạm", "Hoàng", "Huỳnh", "Phan", "Vũ", "Võ", "Đặng", "Bùi", "Đỗ", "Hồ", "Ngô", "Dương", "Lý"}
}

func familyWeights() []int {
	return []int{38, 11, 9, 7, 5, 5, 4, 4, 4, 2, 2, 2, 2, 2, 2, 1}
}

func middleNames() []string {
	return []string{"Thị", "Văn", "Ngọc", "Minh", "Thanh", "Thu", "Hoàng", "Đức", "Kim", "Bảo", "Phương", "Khánh", "Mỹ", "Hải", "Quỳnh"}
}

func givenNames() []string {
	return []string{
		"Anh", "Linh", "Trang", "Hương", "Hà", "Mai", "Lan", "Hằng", "Nhung", "Thảo", "Ngọc", "Phương",
		"Hoa", "Yến", "Vy", "Tâm", "Hạnh", "Dung", "Huyền", "Quỳnh", "Nga", "Giang", "Chi", "Thủy", "Hiền", "Oanh", "Loan",
		"Tuyết", "Nhi", "My", "Uyên", "Trinh", "Diệp", "Khánh", "Hùng", "Tuấn", "Nam", "Long", "Dũng", "Sơn",
	}
}

// pickWeighted chọn chỉ số theo trọng số w (tổng > 0).
func pickWeighted(r *rand.Rand, w []int) int {
	total := 0
	for _, x := range w {
		total += x
	}
	n := r.IntN(total)
	for i, x := range w {
		if n < x {
			return i
		}
		n -= x
	}
	return len(w) - 1
}

// fakeName là một họ tên ba chữ ngẫu nhiên.
func fakeName(r *rand.Rand) string {
	family, weights, middle, given := nameParts()
	return family[pickWeighted(r, weights)] + " " + middle[r.IntN(len(middle))] + " " + given[r.IntN(len(given))]
}

// customerCode là mã khách thứ i (0…): "KH9" + 8 chữ số. Dài hơn mọi mã do sequence của module customer sinh (KH + 6–7
// chữ số) nên không bao giờ trùng mã khách tạo qua API trong bài tải.
func customerCode(i int) string { return fmt.Sprintf("KH9%08d", i+1) }

// orderCode là mã đơn thứ j: "DH9" + 12 chữ số, cùng lý do (sequence của đơn sinh DH + 7 chữ số; chỉ queries/ord/code.sql
// gọi sequence, TestCodeSequenceSingleSource).
func orderCode(j int) string { return fmt.Sprintf("DH9%012d", j+1) }

// uuidV7At là một uuid phiên bản 7 mang mốc thời gian t (48 bit mili giây) và phần ngẫu nhiên từ r: như uuidv7() của
// PostgreSQL lúc t, để thứ tự mã theo thời gian giống dữ liệu thật.
func uuidV7At(t time.Time, r *rand.Rand) uuid.UUID {
	var u uuid.UUID
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(max(t.UnixMilli(), 0)))
	binary.BigEndian.PutUint64(u[8:], r.Uint64())
	binary.BigEndian.PutUint64(u[:8], r.Uint64())
	copy(u[0:6], ts[2:8])   // 48 bit mili giây
	u[6] = u[6]&0x0f | 0x70 // phiên bản 7
	u[8] = u[8]&0x3f | 0x80 // biến thể RFC 9562
	return u
}

// Trạng thái kết thúc của đơn nạp sẵn và tỷ lệ (phần nghìn): giao xong 85%, hoàn 7%, hủy 8% (QĐ-88-3).
const (
	permilleDelivered = 850
	permilleReturned  = 70
	shippingFee       = 30_000
)

// variantRef là một biến thể có thật của tenant đo (đơn nạp sẵn tham chiếu nó).
type variantRef struct {
	ID    uuid.UUID
	SKU   string
	Price int64
}

// genOrder là một đơn nạp sẵn và các dòng của nó.
type genOrder struct {
	ID, IdemKey uuid.UUID
	Code        string
	Customer    int // chỉ số khách trong tenant
	Status      string
	CreatedAt   time.Time
	Subtotal    int64
	GrandTotal  int64
	Items       []genItem
}

type genItem struct {
	ID       uuid.UUID
	Variant  variantRef
	Position int32
}

// timeline là khung thời gian của dữ liệu nạp sẵn: đơn rải đều trong [from, to); khách thứ c (trên n khách) tạo lúc
// from − lead + (c/n)·(to − from + lead): khách chỉ số nhỏ là khách cũ (có từ trước khung, như khách nhập từ hệ cũ).
type timeline struct {
	from, to time.Time
	lead     time.Duration
	n        int
}

// customerCreated là lúc tạo khách c.
func (tl timeline) customerCreated(c int) time.Time {
	span := tl.to.Sub(tl.from) + tl.lead
	return tl.from.Add(-tl.lead + time.Duration(float64(span)*float64(c)/float64(tl.n)))
}

// customersBy là số khách đã tạo trước lúc at (ít nhất 1).
func (tl timeline) customersBy(at time.Time) int {
	span := tl.to.Sub(tl.from) + tl.lead
	k := int(float64(tl.n) * float64(at.Sub(tl.from)+tl.lead) / float64(span))
	return min(max(k, 1), tl.n)
}

// profile là phần cố định của khách c (mã, tên, lúc tạo), sinh từ hạt giống riêng của khách: tính lại ở mọi lượt (đơn,
// danh tính, khách) mà không cần giữ trong bộ nhớ.
type profile struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// randFor là bộ sinh ngẫu nhiên riêng của một phần tử (hạt giống, chỉ số): tính lại được ở mọi lượt.
func randFor(seed, i uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, i)) //nolint:gosec // dữ liệu thử, không phải mật mã
}

func customerProfile(seed uint64, salt uint64, tl timeline, c int) profile {
	r := randFor(seed^salt, uint64(c)) //nolint:gosec // c ≥ 0
	at := tl.customerCreated(c)
	return profile{ID: uuidV7At(at, r), Name: fakeName(r), CreatedAt: at}
}

// orderGen sinh n đơn theo thứ tự thời gian, rải đều trong khung: khách chọn trong số khách đã tạo, lệch về khách cũ
// (mua nhiều lần hơn), 40% đơn một dòng, 60% hai dòng (8 dòng mỗi 5 đơn), hai biến thể khác nhau trong một đơn. Hai
// orderGen cùng hạt giống sinh cùng dãy đơn (lượt đơn và lượt dòng đơn chạy song song, mỗi lượt một bộ sinh).
type orderGen struct {
	r        *rand.Rand
	n        int
	tl       timeline
	step     time.Duration
	variants []variantRef
	j        int
}

func newOrderGen(seed uint64, n int, tl timeline, variants []variantRef) *orderGen {
	step := time.Duration(0)
	if n > 0 {
		step = tl.to.Sub(tl.from) / time.Duration(n)
	}
	r := randFor(seed, 0x6f72646572)
	return &orderGen{r: r, n: n, tl: tl, step: step, variants: variants}
}

// next trả đơn kế tiếp; false khi đã đủ n đơn.
func (g *orderGen) next() (genOrder, bool) {
	if g.j >= g.n {
		return genOrder{}, false
	}
	j := g.j
	g.j++
	at := g.tl.from.Add(time.Duration(j)*g.step + time.Duration(g.r.Int64N(int64(max(g.step, 1)))))
	u := g.r.Float64()
	c := min(int(float64(g.tl.customersBy(at))*u*u), g.tl.n-1)
	o := genOrder{ID: uuidV7At(at, g.r), IdemKey: uuidV7At(at, g.r), Code: orderCode(j), Customer: c, CreatedAt: at}
	switch p := g.r.IntN(1000); {
	case p < permilleDelivered:
		o.Status = "delivered"
	case p < permilleDelivered+permilleReturned:
		o.Status = "returned"
	default:
		o.Status = "cancelled"
	}
	lines := 1
	if j%5 >= 2 && len(g.variants) > 1 { // 3 trên 5 đơn có hai dòng
		lines = 2
	}
	first := g.r.IntN(len(g.variants))
	for k := range lines {
		v := g.variants[(first+k)%len(g.variants)]
		o.Items = append(o.Items, genItem{ID: uuidV7At(at, g.r), Variant: v, Position: int32(k + 1)})
		o.Subtotal += v.Price
	}
	o.GrandTotal = o.Subtotal + shippingFee
	return o, true
}

// custStats là chỉ số mua hàng của một khách, tính như customer ComputeGroupStats từ các đơn nạp sẵn.
type custStats struct {
	orders, delivered, returned int32
	spent                       int64
	last                        time.Time
}

func (s *custStats) add(o genOrder) {
	if o.Status == "cancelled" {
		return
	}
	s.orders++
	if o.CreatedAt.After(s.last) {
		s.last = o.CreatedAt
	}
	switch o.Status {
	case "delivered":
		s.delivered++
		s.spent += o.GrandTotal
	case "returned":
		s.returned++
	}
}
