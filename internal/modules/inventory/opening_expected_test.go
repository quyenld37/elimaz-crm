package inventory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Tồn dự kiến (kế hoạch M7, Task 541 = M7-41): ghi số Nhanh theo (kho, biến thể), cột lệch = đếm − dự kiến; chưa đếm
// thì lệch trống; chụp lại thì thay số; số dự kiến KHÔNG phải tồn (không dòng sổ, không số dư) và không làm kho "không
// trống" của phiên mở sổ.
func (e *invEnv) setExpected(wh uuid.UUID, at time.Time, rows []inventory.ExpectedStock) (int, error) {
	var n int
	err := e.app(func(tx pgx.Tx) error {
		var err error
		n, err = inventory.SetOpeningExpected(e.t.Context(), tx, e.tenant, wh, at, rows)
		return err //nolint:wrapcheck // test
	})
	return n, err
}

func (e *invEnv) openingDiff(id uuid.UUID) ([]inventory.OpeningDiffRow, error) {
	var rows []inventory.OpeningDiffRow
	err := e.app(func(tx pgx.Tx) error {
		var err error
		rows, err = inventory.OpeningDiff(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return rows, err
}

func TestOpeningExpected_DiffColumnAndRewrite(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	taken := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	exp := []inventory.ExpectedStock{
		{VariantID: vs[0], SellerID: e.sellers()[0], Expected: 7},
		{VariantID: vs[1], SellerID: e.sellers()[0], Expected: 0},
		{VariantID: vs[2], SellerID: e.sellers()[0], Expected: 5},
	}
	if n, err := e.setExpected(e.warehouse, taken, exp); err != nil || n != 3 {
		t.Fatalf("SetOpeningExpected = %d, %v", n, err)
	}
	s := e.mustCreateOpening(e.warehouse)
	// Không đổi tồn: kho vẫn trống nên mở sổ và đăng được như thường (số dự kiến không phải sổ).
	if c := e.count("SELECT count(*) FROM inventory.stock_ledger"); c != 0 {
		t.Fatalf("có %d dòng sổ sau khi ghi số dự kiến", c)
	}
	rows, err := e.openingDiff(s.ID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("OpeningDiff = %d dòng, %v", len(rows), err)
	}
	for _, r := range rows {
		if r.Counted != nil || r.Diff != nil {
			t.Errorf("%s chưa đếm nhưng có đếm/lệch: %+v", r.SKU, r)
		}
	}
	e.countAll(s.ID, vs[:2], []int32{4, 0}) // vs[0]: 4 so với 7 → −3; vs[1]: 0 so với 0 → 0; vs[2] chưa đếm
	rows, err = e.openingDiff(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	bySKU := map[string]inventory.OpeningDiffRow{}
	for _, r := range rows {
		bySKU[r.SKU] = r
	}
	if r := bySKU["MS1-1"]; r.Expected != 7 || r.Counted == nil || *r.Counted != 4 || r.Diff == nil || *r.Diff != -3 {
		t.Errorf("MS1-1 = %+v, muốn dự kiến 7, đếm 4, lệch −3", r)
	}
	if r := bySKU["MS1-2"]; r.Diff == nil || *r.Diff != 0 {
		t.Errorf("MS1-2 = %+v, muốn lệch 0", r)
	}
	if r := bySKU["MS1-3"]; r.Counted != nil || r.Diff != nil || r.Expected != 5 {
		t.Errorf("MS1-3 = %+v, muốn chưa đếm", r)
	}
	// Chụp lại: thay số (không thêm dòng).
	exp[0].Expected = 9
	if n, err := e.setExpected(e.warehouse, taken.Add(time.Hour), exp[:1]); err != nil || n != 1 {
		t.Fatalf("ghi lại = %d, %v", n, err)
	}
	if c := e.count("SELECT count(*) FROM inventory.opening_expected"); c != 3 {
		t.Errorf("sau khi ghi lại có %d dòng, muốn 3", c)
	}
	if c := e.count("SELECT count(*) FROM inventory.opening_expected WHERE expected = 9"); c != 1 {
		t.Errorf("số dự kiến mới không thay số cũ")
	}
	// Đăng phiên mở sổ vẫn qua đường thường; tồn mở sổ là số đếm.
	e.countAll(s.ID, vs[2:], append(make([]int32, 0, 10), 2, 0, 0, 0, 0, 0, 0, 0, 0, 0))
	if _, err := e.postOpening(s.ID); err != nil {
		t.Fatalf("PostOpening: %v", err)
	}
	if b := e.balance(vs[0]); b.OnHand != 4 {
		t.Errorf("tồn mở sổ vs[0] = %d, muốn 4 (số đếm, không phải số dự kiến 9)", b.OnHand)
	}
}

func TestOpeningExpected_Refused(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	seller := e.sellers()[0]
	at := time.Now()
	for name, c := range map[string]struct {
		rows []inventory.ExpectedStock
		at   time.Time
		want error
	}{
		"âm":          {[]inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller, Expected: -1}}, at, inventory.ErrInvalidInput},
		"quá lớn":     {[]inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller, Expected: 1_000_001}}, at, inventory.ErrInvalidInput},
		"lặp":         {[]inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller}, {VariantID: vs[0], SellerID: seller}}, at, inventory.ErrInvalidInput},
		"thiếu giờ":   {[]inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller}}, time.Time{}, inventory.ErrInvalidInput},
		"biến thể lạ": {[]inventory.ExpectedStock{{VariantID: uuid.New(), SellerID: seller}}, at, inventory.ErrNotFound},
	} {
		if _, err := e.setExpected(e.warehouse, c.at, c.rows); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, muốn %v", name, err, c.want)
		}
	}
	if _, err := e.setExpected(uuid.New(), at, []inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller}}); !errors.Is(err, inventory.ErrNotFound) {
		t.Errorf("kho lạ: err = %v, muốn ErrNotFound", err)
	}
	if _, err := e.openingDiff(uuid.New()); !errors.Is(err, inventory.ErrNotFound) {
		t.Errorf("phiên lạ: err = %v, muốn ErrNotFound", err)
	}
	if c := e.count("SELECT count(*) FROM inventory.opening_expected"); c != 0 {
		t.Errorf("lỗi vẫn để lại %d dòng", c)
	}
}

// Tenant khác không thấy, không ghi được vào kho của tenant này (RLS).
func TestOpeningExpected_TenantIsolated(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	seller := e.sellers()[0]
	if _, err := e.setExpected(e.warehouse, time.Now(), []inventory.ExpectedStock{{VariantID: vs[0], SellerID: seller, Expected: 3}}); err != nil {
		t.Fatal(err)
	}
	other := e.staff.Tenant(t, "B")
	if c := e.staff.CountIn(t, other, "SELECT count(*) FROM inventory.opening_expected"); c != 0 {
		t.Errorf("tenant khác thấy %d dòng", c)
	}
}
