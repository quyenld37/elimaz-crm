package inventory_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Đếm cộng dồn nhiều người (kế hoạch M7, Task 263 = M7-18): IncrementLine cộng/trừ theo mã quét (SKU hoặc barcode)
// vào phiên mở sổ.

func (e *invEnv) increment(id uuid.UUID, code string, by int32) (uuid.UUID, int32, error) {
	var v uuid.UUID
	var n int32
	err := e.app(func(tx pgx.Tx) error {
		var err error
		v, n, err = inventory.IncrementLine(e.t.Context(), tx, id, code, by, staffActor(), time.Now())
		return err //nolint:wrapcheck // test
	})
	return v, n, err
}

// countedOpening dựng biến thể mẫu, mở phiên mở sổ ở kho mặc định và đếm mọi dòng bằng 0.
func (e *invEnv) countedOpening() ([]uuid.UUID, inventory.Stocktake) {
	e.t.Helper()
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	e.countAll(s.ID, vs, make([]int32, len(vs)))
	return vs, s
}

func (e *invEnv) countedOf(id, v uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(counted), -1)::int FROM inventory.stocktake_lines
		WHERE stocktake_id = $1 AND variant_id = $2`, id, v)
}

// 3 người, mỗi người 100 lần +1 song song cho cùng một biến thể (người thứ nhất quét SKU viết thường, hai người kia quét
// barcode) → counted = 300.
func TestIncrementLine_ConcurrentCounters(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	var wg sync.WaitGroup
	errs := make(chan error, 300)
	for p := range 3 {
		code := "MS1-1"
		if p == 0 {
			code = "ms1-1"
		}
		wg.Go(func() {
			for range 100 {
				if _, _, err := e.increment(s.ID, code, 1); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("IncrementLine song song: %v", err)
	}
	if got := e.countedOf(s.ID, vs[0]); got != 300 {
		t.Fatalf("counted = %d, mong 300", got)
	}
}

// Barcode → đúng biến thể; mã lạ → ErrUnknownCode; barcode trùng giữa hai người bán → *AmbiguousCodeError kèm hai ứng
// viên; trừ quá số đã đếm (2 − 3) → ErrNegativeCount, số đếm giữ 2; trừ dòng chưa có → ErrNegativeCount; by = 0 hay ngoài
// [-1000, 1000] → ErrInvalidInput.
func TestIncrementLine_CodesAndBounds(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	dupA, dupB, sellers := e.duplicateSKU("TRUNG-1")
	s := e.mustCreateOpening(e.warehouse)

	if v, n, err := e.increment(s.ID, " MS2-3 ", 2); err != nil || v != vs[6] || n != 2 {
		t.Fatalf("quét barcode MS2-3: %s %d %v, mong %s 2", v, n, err, vs[6])
	}
	if _, _, err := e.increment(s.ID, "KHONGCO", 1); !errors.Is(err, inventory.ErrUnknownCode) {
		t.Fatalf("mã lạ: %v", err)
	}
	checkAmbiguous(t, e, s.ID, dupA, dupB, sellers)
	if _, _, err := e.increment(s.ID, "MS2-3", -3); !errors.Is(err, inventory.ErrNegativeCount) {
		t.Fatalf("2 − 3: %v", err)
	}
	if n := e.countedOf(s.ID, vs[6]); n != 2 {
		t.Fatalf("sau khi trừ quá: %d, mong 2", n)
	}
	if _, n, err := e.increment(s.ID, "MS2-3", -1); err != nil || n != 1 {
		t.Fatalf("2 − 1: %d %v", n, err)
	}
	if _, _, err := e.increment(s.ID, "MS2-4", -1); !errors.Is(err, inventory.ErrNegativeCount) {
		t.Fatalf("trừ dòng chưa có: %v", err)
	}
	if e.countedOf(s.ID, vs[7]) != -1 {
		t.Fatal("trừ dòng chưa có không được tạo dòng")
	}
	for _, by := range []int32{0, 1001, -1001} {
		if _, _, err := e.increment(s.ID, "MS2-3", by); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Fatalf("by = %d: %v", by, err)
		}
	}
}

// duplicateSKU tạo hai biến thể cùng SKU sku ở hai người bán đầu; trả hai biến thể và danh sách người bán.
func (e *invEnv) duplicateSKU(sku string) (uuid.UUID, uuid.UUID, []uuid.UUID) {
	e.t.Helper()
	sellers := e.sellers()
	var dupA, dupB uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		pa, err := e.fixtureProduct(tx, sellers[0], "TRUNGA", false)
		if err != nil {
			return err
		}
		pb, err := e.fixtureProduct(tx, sellers[1], "TRUNGB", false)
		if err != nil {
			return err
		}
		if dupA, err = e.fixtureVariant(tx, sellers[0], pa, sku, "active"); err != nil {
			return err
		}
		dupB, err = e.fixtureVariant(tx, sellers[1], pb, sku, "active")
		return err
	})
	return dupA, dupB, sellers
}

// checkAmbiguous: cộng mã TRUNG-1 vào phiên id → *AmbiguousCodeError kèm đúng hai ứng viên dupA, dupB.
func checkAmbiguous(t *testing.T, e *invEnv, id, dupA, dupB uuid.UUID, sellers []uuid.UUID) {
	t.Helper()
	_, _, err := e.increment(id, "TRUNG-1", 1)
	ae, ok := errors.AsType[*inventory.AmbiguousCodeError](err)
	if !ok || !errors.Is(err, inventory.ErrAmbiguousCode) || len(ae.Candidates) != 2 {
		t.Fatalf("mã trùng hai người bán: %v", err)
	}
	got := map[uuid.UUID]uuid.UUID{ae.Candidates[0].VariantID: ae.Candidates[0].SellerID, ae.Candidates[1].VariantID: ae.Candidates[1].SellerID}
	if got[dupA] != sellers[0] || got[dupB] != sellers[1] || ae.Candidates[0].SKU != "TRUNG-1" {
		t.Fatalf("ứng viên: %+v", ae.Candidates)
	}
}

// Phiên kiểm thường (M1) không nhận đếm cộng dồn: không tăng version phiên nên phá "áp dụng đúng bản đã xem trước".
func TestIncrementLine_RegularStocktakeRejected(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	e.openingFixture()
	reg := e.mustStart()
	if _, _, err := e.increment(reg.ID, "MS1-1", 1); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("phiên thường: %v", err)
	}
	if _, _, err := e.increment(uuid.New(), "MS1-1", 1); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("phiên không có: %v", err)
	}
}

// Phiên đã đăng → ErrStocktakeNotOpen; +1 chạy song song với PostOpening → mỗi lần cộng hoặc thành công trước khi đăng
// (số đếm vào tồn mở sổ) hoặc nhận ErrStocktakeNotOpen; không dòng nào đổi sau khi phiên applied.
func TestIncrementLine_RacesPost(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs, s := e.countedOpening()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	start := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			<-start
			for range 25 {
				_, _, err := e.increment(s.ID, "MS1-2", 1)
				switch {
				case err == nil:
					mu.Lock()
					ok++
					mu.Unlock()
				case errors.Is(err, inventory.ErrStocktakeNotOpen):
				default:
					t.Errorf("cộng song song với đăng: %v", err)
				}
			}
		})
	}
	var postErr error
	wg.Go(func() {
		<-start
		time.Sleep(5 * time.Millisecond)
		_, postErr = e.postOpening(s.ID)
	})
	close(start)
	wg.Wait()
	if postErr != nil {
		t.Fatalf("PostOpening: %v", postErr)
	}
	// Số đếm cuối = số lần cộng thành công = tồn mở sổ: mọi lần cộng thành công đều vào trước khi đăng, không dòng nào
	// đổi sau khi phiên applied (dòng đổi sau sẽ làm counted khác on_hand). Không so counted_at với applied_at: hai mốc
	// lấy từ đồng hồ Go lúc bắt đầu mỗi lệnh, PostOpening lấy giờ trước khi chờ khóa nên lần cộng hợp lệ có thể mang giờ
	// muộn hơn (bẫy "một đồng hồ").
	if c, oh := e.countedOf(s.ID, vs[1]), e.onHandAt(vs[1], e.warehouse); c != ok || oh != ok {
		t.Fatalf("counted %d, on_hand %d, mong cả hai = %d lần cộng thành công", c, oh, ok)
	}
	if _, _, err := e.increment(s.ID, "MS1-2", 1); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		t.Fatalf("sau khi đăng: %v", err)
	}
}

// Quy luật (rapid): chuỗi cộng/trừ ngẫu nhiên trên vài biến thể, so với mô hình → số đếm sau mỗi lần = tổng các lần
// được nhận, không bao giờ âm, và lần bị từ chối đúng là lần sẽ làm âm. Đồng thời: TestIncrementLine_ConcurrentCounters.
func TestIncrementLine_Property(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	e.openingFixture()
	codes := []string{"MS1-1", "MS1-2", "MS3-1"}
	rapid.Check(t, func(rt *rapid.T) {
		s := e.mustCreateOpeningFresh(rt)
		model := map[string]int32{}
		ops := rapid.SliceOfN(rapid.Custom(func(rt *rapid.T) [2]int32 {
			return [2]int32{rapid.Int32Range(0, 2).Draw(rt, "code"), rapid.Int32Range(-5, 5).Filter(func(n int32) bool { return n != 0 }).Draw(rt, "by")}
		}), 1, 30).Draw(rt, "ops")
		for _, op := range ops {
			code := codes[op[0]]
			_, n, err := e.increment(s, code, op[1])
			want := model[code] + op[1]
			switch {
			case want < 0 && !errors.Is(err, inventory.ErrNegativeCount):
				rt.Fatalf("%s %+d từ %d: mong ErrNegativeCount, có %v", code, op[1], model[code], err)
			case want < 0:
			case err != nil || n != want:
				rt.Fatalf("%s %+d từ %d: %d %v", code, op[1], model[code], n, err)
			default:
				model[code] = want
			}
		}
		if err := e.cancelCurrent(s); err != nil {
			rt.Fatalf("hủy phiên: %v", err)
		}
	})
}

// mustCreateOpeningFresh mở một phiên mở sổ mới trên kho chính (lượt trước đã hủy phiên của nó).
func (e *invEnv) mustCreateOpeningFresh(rt *rapid.T) uuid.UUID {
	s, _, err := e.createOpening(e.warehouse)
	if err != nil {
		rt.Fatalf("CreateOpening: %v", err)
	}
	return s.ID
}

// cancelCurrent hủy phiên id theo version hiện tại.
func (e *invEnv) cancelCurrent(id uuid.UUID) error {
	var v int64
	if err := e.app(func(tx pgx.Tx) error {
		s, _, err := inventory.GetStocktake(e.t.Context(), tx, id)
		v = s.Version
		return err //nolint:wrapcheck // test
	}); err != nil {
		return err
	}
	return e.cancelStocktake(id, v)
}
