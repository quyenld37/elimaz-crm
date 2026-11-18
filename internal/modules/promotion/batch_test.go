package promotion_test

import (
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

func template() promotion.CouponTemplate {
	in := sale10("")
	return promotion.CouponTemplate{
		Name: "Quà sinh nhật", DiscountKind: promotion.KindFixed, Value: 100000, MinSubtotal: 500000,
		Channels: []string{"manual"}, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo,
	}
}

func (e *env) batch(in promotion.BatchInput) (promotion.Batch, error) {
	return e.batchWith(promotion.Writer{}, in)
}

func (e *env) batchWith(w promotion.Writer, in promotion.BatchInput) (promotion.Batch, error) {
	var b promotion.Batch
	err := e.app(func(tx pgx.Tx) error {
		var err error
		b, err = w.CreateBatch(e.t.Context(), tx, e.actor(), in)
		return err //nolint:wrapcheck // test
	})
	return b, err
}

type batchCoupon struct {
	code, name, kind string
	value, minSub    int64
	maxUses, used    int32
	phone            *string
	channels         []string
}

func (e *env) batchCoupons(batchID uuid.UUID) []batchCoupon {
	e.t.Helper()
	var out []batchCoupon
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT code::text, name, discount_kind, value, min_subtotal, max_uses,
			used_count, bound_phone, channels FROM promotion.coupons WHERE batch_id = $1 ORDER BY code`, batchID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (batchCoupon, error) {
			var c batchCoupon
			err := r.Scan(&c.code, &c.name, &c.kind, &c.value, &c.minSub, &c.maxUses, &c.used, &c.phone, &c.channels)
			return c, err //nolint:wrapcheck // test
		})
		return err //nolint:wrapcheck // test
	})
	return out
}

func TestCreateBatch_Quantity(t *testing.T) {
	e := newEnv(t)
	b, err := e.batch(promotion.BatchInput{Name: "Đợt 500", Quantity: 500, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	if b.Quantity != 500 || b.Name != "Đợt 500" || b.Prefix != "" || b.Template.Value != 100000 ||
		b.Template.Name != "Quà sinh nhật" || !b.Template.ValidTo.Equal(template().ValidTo) || b.CreatedBy != e.user {
		t.Fatalf("đợt: %+v", b)
	}
	cs := e.batchCoupons(b.ID)
	if len(cs) != 500 {
		t.Fatalf("500 mã: %d", len(cs))
	}
	seen := map[string]bool{}
	for _, c := range cs {
		if !generatedCode.MatchString(c.code) || seen[c.code] {
			t.Fatalf("mã %q sai dạng hay trùng", c.code)
		}
		seen[c.code] = true
		if c.maxUses != 1 || c.used != 0 || c.phone != nil || c.kind != promotion.KindFixed || c.value != 100000 ||
			c.minSub != 500000 || c.name != "Quà sinh nhật" || !slices.Equal(c.channels, []string{"manual"}) {
			t.Fatalf("điều khoản theo mẫu, một lượt: %+v", c)
		}
	}
	var got promotion.Batch
	if err := e.app(func(tx pgx.Tx) error {
		got, err = promotion.GetBatch(t.Context(), tx, b.ID)
		return err //nolint:wrapcheck // test
	}); err != nil || got.ID != b.ID || got.Quantity != 500 || got.Template.MinSubtotal != 500000 {
		t.Fatalf("GetBatch: %v %+v", err, got)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.GetBatch(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("đợt không có: %v", err)
	}
}

// Đợt vượt một lô 1.000 dòng, có tiền tố.
func TestCreateBatch_ChunksAndPrefix(t *testing.T) {
	e := newEnv(t)
	b, err := e.batch(promotion.BatchInput{Name: "Đợt lớn", Prefix: "TET", Quantity: 2001, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	cs := e.batchCoupons(b.ID)
	re := regexp.MustCompile(`^TET-[A-HJ-NP-Z2-9]{8}$`)
	if len(cs) != 2001 || b.Prefix != "TET" {
		t.Fatalf("2001 mã: %d", len(cs))
	}
	for _, c := range cs {
		if !re.MatchString(c.code) {
			t.Fatalf("mã có tiền tố: %q", c.code)
		}
	}
}

func TestCreateBatch_BoundPhones(t *testing.T) {
	e := newEnv(t)
	phones := []string{"0901000001", "+84901000002", "84 901 000 003"}
	b, err := e.batch(promotion.BatchInput{Name: "Sinh nhật", Quantity: 3, Template: template(), BoundPhones: phones})
	if err != nil {
		t.Fatal(err)
	}
	cs := e.batchCoupons(b.ID)
	var got []string
	for _, c := range cs {
		if c.phone == nil || c.maxUses != 1 {
			t.Fatalf("mã gắn SĐT, một lượt: %+v", c)
		}
		got = append(got, *c.phone)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"+84901000001", "+84901000002", "+84901000003"}) {
		t.Fatalf("mỗi SĐT đúng một mã: %v", got)
	}
}

func TestCreateBatch_Invalid(t *testing.T) {
	e := newEnv(t)
	tpl := template()
	bad := tpl
	bad.Value = 0
	cases := []struct {
		name string
		in   promotion.BatchInput
		want error
	}{
		{"số lượng 0", promotion.BatchInput{Name: "x", Quantity: 0, Template: tpl}, promotion.ErrInvalidInput},
		{"quá 100.000", promotion.BatchInput{Name: "x", Quantity: 100001, Template: tpl}, promotion.ErrInvalidInput},
		{"tên rỗng", promotion.BatchInput{Name: " ", Quantity: 1, Template: tpl}, promotion.ErrInvalidInput},
		{"tiền tố thường", promotion.BatchInput{Name: "x", Prefix: "tet", Quantity: 1, Template: tpl}, promotion.ErrInvalidInput},
		{"mẫu sai", promotion.BatchInput{Name: "x", Quantity: 1, Template: bad}, promotion.ErrInvalidInput},
		{"số lượng khác số SĐT", promotion.BatchInput{
			Name: "x", Quantity: 2, Template: tpl,
			BoundPhones: []string{"0901000001"},
		}, promotion.ErrInvalidInput},
		{"SĐT trùng sau chuẩn hóa", promotion.BatchInput{
			Name: "x", Quantity: 2, Template: tpl,
			BoundPhones: []string{"0901000001", "+84901000001"},
		}, promotion.ErrInvalidInput},
		{
			"SĐT sai",
			promotion.BatchInput{Name: "x", Quantity: 1, Template: tpl, BoundPhones: []string{"123"}},
			customer.ErrInvalidPhone,
		},
	}
	for _, c := range cases {
		if _, err := e.batch(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupon_batches`); n != 0 {
		t.Fatalf("đợt sai không được ghi: %d", n)
	}
}

// Mã sinh trùng mã có sẵn: chỉ mã trùng được sinh lại; đủ số lượng.
func TestCreateBatch_RegeneratesTaken(t *testing.T) {
	e := newEnv(t)
	if _, err := e.create(sale10("TAKEN234")); err != nil {
		t.Fatal(err)
	}
	next := []string{"TAKEN234", "AAAA2222", "TAKEN234", "BBBB3333"}
	calls := 0
	w := promotion.WithCodeGen(promotion.Writer{}, func(string) (string, error) {
		c := next[calls%len(next)]
		calls++
		return c, nil
	})
	b, err := e.batchWith(w, promotion.BatchInput{Name: "x", Quantity: 2, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	cs := e.batchCoupons(b.ID)
	if len(cs) != 2 || cs[0].code != "AAAA2222" || cs[1].code != "BBBB3333" {
		t.Fatalf("mã của đợt: %+v", cs)
	}
	always := promotion.WithCodeGen(promotion.Writer{}, func(string) (string, error) { return "TAKEN234", nil })
	if _, err := e.batchWith(always, promotion.BatchInput{Name: "x", Quantity: 1, Template: template()}); err == nil {
		t.Fatal("luôn trùng: phải lỗi")
	}
}
