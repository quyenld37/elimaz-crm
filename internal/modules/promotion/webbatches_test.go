package promotion_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// webBatch là kết quả một lần EnsureWebBatch.
type webBatch struct {
	batch   promotion.Batch
	codes   []string
	created bool
}

func (e *env) ensure(in promotion.WebBatchInput) (webBatch, error) {
	var r webBatch
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r.batch, r.codes, r.created, err = promotion.Writer{}.EnsureWebBatch(e.t.Context(), tx, in, e.actor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func birthdayInput(name string, n int) promotion.WebBatchInput {
	return promotion.WebBatchInput{Name: name, Purpose: promotion.PurposeBirthday, Quantity: n, Template: template()}
}

// [WB05] Lô sinh nhật 30 mã: 30 mã khác nhau, mỗi mã một lượt, purpose birthday, người tạo là tác nhân (khóa dịch
// vụ); gọi lại cùng tên, cùng điều khoản → cùng lô, cùng 30 mã, không thêm dòng; cùng tên, giá trị khác →
// ErrBatchConflict; lô manual cùng tên do nhân viên tạo trước đó vẫn manual, giữ nguyên mã.
func TestEnsureWebBatch_SameNameSameBatch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	manual, err := e.batch(promotion.BatchInput{Name: "Sinh nhật 12/2026", Quantity: 2, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.ensure(birthdayInput("Sinh nhật 12/2026", 30))
	if err != nil {
		t.Fatal(err)
	}
	if !first.created || first.batch.Purpose != promotion.PurposeBirthday || first.batch.ID == manual.ID ||
		first.batch.Quantity != 30 || first.batch.CreatedBy != e.user || len(first.codes) != 30 {
		t.Fatalf("lô mới: %+v, %d mã", first.batch, len(first.codes))
	}
	cs := e.batchCoupons(first.batch.ID)
	seen := map[string]bool{}
	for _, c := range cs {
		if c.maxUses != 1 || c.used != 0 || seen[c.code] {
			t.Fatalf("mã %+v", c)
		}
		seen[c.code] = true
	}
	if len(seen) != 30 || !slices.Equal(sortedCodes(cs), first.codes) {
		t.Fatalf("30 mã khác nhau, trả theo thứ tự chữ: %d, %v", len(seen), first.codes)
	}

	again, err := e.ensure(birthdayInput("Sinh nhật 12/2026", 30))
	if err != nil {
		t.Fatal(err)
	}
	if again.created || again.batch.ID != first.batch.ID || !slices.Equal(again.codes, first.codes) {
		t.Fatalf("gọi lại: %+v %v", again.batch, again.created)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons`); n != 32 {
		t.Fatalf("gọi lại thêm dòng: %d mã, mong 32", n)
	}
	other := birthdayInput("Sinh nhật 12/2026", 30)
	other.Template.Value = 150000
	if _, err := e.ensure(other); !errors.Is(err, promotion.ErrBatchConflict) {
		t.Fatalf("khác giá trị: %v", err)
	}
	if _, err := e.ensure(birthdayInput("Sinh nhật 12/2026", 31)); !errors.Is(err, promotion.ErrBatchConflict) {
		t.Fatalf("khác số lượng: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupon_batches WHERE id = $1
		AND purpose = 'manual' AND quantity = 2`, manual.ID); n != 1 || len(e.batchCoupons(manual.ID)) != 2 {
		t.Fatal("lô manual cùng tên bị đổi")
	}
}

func sortedCodes(cs []batchCoupon) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.code)
	}
	slices.Sort(out)
	return out
}

// Lô gắn SĐT: mã trả theo thứ tự SĐT gửi lên (kể cả khi gọi lại với thứ tự khác); SĐT khác → ErrBatchConflict.
func TestEnsureWebBatch_BoundPhonesOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	in := birthdayInput("Sinh nhật gắn SĐT", 3)
	in.BoundPhones = []string{"0901000001", "0901000002", "0901000003"}
	first, err := e.ensure(in)
	if err != nil {
		t.Fatal(err)
	}
	byPhone := map[string]string{}
	for _, c := range e.batchCoupons(first.batch.ID) {
		byPhone[*c.phone] = c.code
	}
	want := []string{byPhone["+84901000001"], byPhone["+84901000002"], byPhone["+84901000003"]}
	if !slices.Equal(first.codes, want) {
		t.Fatalf("thứ tự mã: %v, mong %v", first.codes, want)
	}
	in.BoundPhones = []string{"+84 901 000 003", "0901000001", "0901000002"}
	again, err := e.ensure(in)
	if err != nil {
		t.Fatal(err)
	}
	if again.created || !slices.Equal(again.codes, []string{want[2], want[0], want[1]}) {
		t.Fatalf("gọi lại khác thứ tự: %v", again.codes)
	}
	in.BoundPhones = []string{"0901000001", "0901000002", "0901000009"}
	if _, err := e.ensure(in); !errors.Is(err, promotion.ErrBatchConflict) {
		t.Fatalf("SĐT khác: %v", err)
	}
}

// Hai lần tạo lô cùng tên chạy song song (hai transaction) → cả hai thành công với cùng lô, đúng một lô, 5 mã.
func TestEnsureWebBatch_ConcurrentSameName(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var wg sync.WaitGroup
	res := make([]webBatch, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Go(func() {
			<-start
			errs[i] = tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
				var err error
				res[i].batch, res[i].codes, res[i].created, err = promotion.Writer{}.EnsureWebBatch(t.Context(), tx,
					birthdayInput("Song song", 5), e.actor())
				return err //nolint:wrapcheck // test
			}, db.TxAPI)
		})
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("lỗi: %v, %v", errs[0], errs[1])
	}
	if res[0].batch.ID != res[1].batch.ID || !slices.Equal(res[0].codes, res[1].codes) || res[0].created == res[1].created {
		t.Fatalf("hai lần: %+v %+v", res[0], res[1])
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupon_batches`); n != 1 {
		t.Fatalf("%d lô, mong 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons`); n != 5 {
		t.Fatalf("%d mã, mong 5", n)
	}
}

// Đầu vào sai: quá 5.000 mã, loại lô không phải birthday, tên rỗng → ErrInvalidInput, không ghi gì.
func TestEnsureWebBatch_Invalid(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	big := birthdayInput("Lớn", 5001)
	manual := birthdayInput("Tay", 1)
	manual.Purpose = promotion.PurposeManual
	imported := birthdayInput("Nhập", 1)
	imported.Purpose = promotion.PurposeImported
	for name, in := range map[string]promotion.WebBatchInput{
		"5001 mã": big, "manual": manual, "imported": imported, "tên rỗng": birthdayInput("  ", 1),
	} {
		if _, err := e.ensure(in); !errors.Is(err, promotion.ErrInvalidInput) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupon_batches`); n != 0 {
		t.Fatalf("%d lô", n)
	}
}

func (e *env) markGiven(code string, at time.Time) (time.Time, bool, error) {
	var m promotion.GivenMark
	err := e.app(func(tx pgx.Tx) error {
		var err error
		m, err = promotion.MarkGiven(e.t.Context(), tx, code, at)
		return err //nolint:wrapcheck // test
	})
	return m.At, m.First, err
}

// MarkGiven hai lần → given_at giữ lần đầu (lần hai changed false); mã của lô manual, mã không có → ErrNotFound.
func TestMarkGiven_KeepsFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, err := e.ensure(birthdayInput("Trao", 1))
	if err != nil {
		t.Fatal(err)
	}
	code := b.codes[0]
	t1 := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	got, changed, err := e.markGiven(code, t1)
	if err != nil || !changed || !got.Equal(t1) {
		t.Fatalf("lần 1: %v %v %v", got, changed, err)
	}
	got, changed, err = e.markGiven(code, t1.Add(time.Hour))
	if err != nil || changed || !got.Equal(t1) {
		t.Fatalf("lần 2: %v %v %v", got, changed, err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE given_at = $1`, t1); n != 1 {
		t.Fatal("given_at không giữ lần đầu")
	}
	m, err := e.batch(promotion.BatchInput{Name: "Tay", Quantity: 1, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.markGiven(e.batchCoupons(m.ID)[0].code, t1); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã lô manual: %v", err)
	}
	if _, _, err := e.markGiven("KHONGCO1", t1); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã không có: %v", err)
	}
}

func (e *env) importCoupons(items []promotion.ImportItem) (promotion.ImportReport, error) {
	var r promotion.ImportReport
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = promotion.ImportCoupons(e.t.Context(), tx, items, e.actor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func importItems(now time.Time) []promotion.ImportItem {
	given := now.Add(-48 * time.Hour).Truncate(time.Second)
	return []promotion.ImportItem{
		{
			Code: "SNCU-OK", DiscountKind: promotion.KindPercent, Value: 10, MaxDiscount: 50000, ValidFrom: now.Add(-72 * time.Hour),
			ValidTo: now.Add(30 * 24 * time.Hour), GivenAt: &given, BatchName: "Sinh nhật cũ",
		},
		{
			Code: "SNCU-DUNG", DiscountKind: promotion.KindFixed, Value: 50000, ValidFrom: now.Add(-72 * time.Hour),
			ValidTo: now.Add(30 * 24 * time.Hour), Used: true, BatchName: "Sinh nhật cũ",
		},
		{
			Code: "TRUNG1", DiscountKind: promotion.KindFixed, Value: 60000, ValidFrom: now.Add(-72 * time.Hour),
			ValidTo: now.Add(30 * 24 * time.Hour), BatchName: "Sinh nhật cũ",
		},
		{
			Code: "SNCU-HET", DiscountKind: promotion.KindFixed, Value: 50000, MinSubtotal: 100000,
			ValidFrom: now.Add(-60 * 24 * time.Hour), ValidTo: now.Add(-24 * time.Hour), BatchName: "Sinh nhật cũ",
		},
	}
}

// [WB05] Nhập 4 mã (một đã dùng, một trùng mã có sẵn khác giá trị, một hết hạn) → Inserted 3, Conflicts [TRUNG1];
// mã có sẵn không đổi; mã đã dùng → exhausted, hết hạn → expired, mã thường áp được (10% → phần vạn 1000, trần 50.000);
// mã nhập: origin web_import, kênh web, một lượt, given_at giữ, lô imported số lượng 3. Nhập lại cả bốn → Inserted 0,
// Skipped 3, Conflicts [TRUNG1], không thêm dòng.
func TestImportCoupons_Report(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	existing := sale10("TRUNG1")
	existing.ValidFrom, existing.ValidTo = time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)
	if _, err := e.create(existing); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	items := importItems(now)
	r, err := e.importCoupons(items)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 3 || r.Skipped != 0 || !slices.Equal(r.Conflicts, []string{"TRUNG1"}) {
		t.Fatalf("báo cáo: %+v", r)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'TRUNG1'
		AND discount_kind = 'percent' AND value = 1000 AND origin = 'crm'`); n != 1 {
		t.Fatal("mã có sẵn bị đổi")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons c
		JOIN promotion.coupon_batches b ON b.id = c.batch_id
		WHERE c.origin = 'web_import' AND c.channels = '{web}' AND c.max_uses = 1 AND b.purpose = 'imported'
		  AND b.name = 'Sinh nhật cũ' AND b.quantity = 3`); n != 3 {
		t.Fatalf("mã nhập: %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'SNCU-OK'
		AND value = 1000 AND max_discount = 50000 AND given_at = $1 AND used_count = 0`, *items[0].GivenAt); n != 1 {
		t.Fatal("mã SNCU-OK: phần trăm, trần, given_at")
	}
	in := e.input(e.customer(), 400000)
	in.Channel = "web"
	if c := e.check("sncu-ok", in); !c.OK || c.Discount != 40000 {
		t.Fatalf("mã thường: %+v", c)
	}
	if c := e.check("SNCU-DUNG", in); c.OK || c.Reason != promotion.ReasonExhausted {
		t.Fatalf("mã đã dùng: %+v", c)
	}
	if c := e.check("SNCU-HET", in); c.OK || c.Reason != promotion.ReasonExpired {
		t.Fatalf("mã hết hạn: %+v", c)
	}
	in.Channel = "facebook"
	if c := e.check("SNCU-OK", in); c.OK || c.Reason != promotion.ReasonChannel {
		t.Fatalf("mã nhập ngoài web: %+v", c)
	}

	again, err := e.importCoupons(items)
	if err != nil {
		t.Fatal(err)
	}
	if again.Inserted != 0 || again.Skipped != 3 || !slices.Equal(again.Conflicts, []string{"TRUNG1"}) {
		t.Fatalf("nhập lại: %+v", again)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons`); n != 4 {
		t.Fatalf("nhập lại thêm dòng: %d", n)
	}
}

// Lô trống tên → "Mã nhập từ website"; đầu vào sai (501 mục, 0 mục, mã trùng trong lần nhập không phân biệt hoa
// thường, phần trăm 101, hạn ngược, mã sai mẫu) → ErrInvalidInput, không nhập mã nào.
func TestImportCoupons_Invalid(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now()
	ok := func(code string) promotion.ImportItem {
		return promotion.ImportItem{
			Code: code, DiscountKind: promotion.KindFixed, Value: 20000, ValidFrom: now,
			ValidTo: now.Add(time.Hour),
		}
	}
	many := make([]promotion.ImportItem, 501)
	for i := range many {
		many[i] = ok("M" + uuid.NewString()[:8])
	}
	pct := ok("PCT101")
	pct.DiscountKind, pct.Value = promotion.KindPercent, 101
	back := ok("NGUOC1")
	back.ValidTo = now.Add(-time.Hour)
	for name, items := range map[string][]promotion.ImportItem{
		"501 mục": many, "0 mục": nil, "trùng": {ok("ABC123"), ok("abc123")}, "101%": {ok("X12345"), pct},
		"hạn ngược": {back}, "mẫu mã": {ok("A B")},
	} {
		if _, err := e.importCoupons(items); !errors.Is(err, promotion.ErrInvalidInput) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons`); n != 0 {
		t.Fatalf("%d mã", n)
	}
	if _, err := e.importCoupons(many[:500]); err != nil {
		t.Fatalf("500 mục: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupon_batches WHERE purpose = 'imported'
		AND name = 'Mã nhập từ website' AND quantity = 500`); n != 1 {
		t.Fatal("lô mặc định")
	}
}

// Số đếm của lô: 10 mã, trao 4, dùng 3 (Redeem thật, một mã đã trao), 2 mã hết hạn chưa dùng → given 4, used 3,
// expired 2.
func TestBatchStats(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, err := e.ensure(birthdayInput("Đếm", 10))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.codes[:4] {
		if _, _, err := e.markGiven(c, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	cust := e.customer()
	in := e.input(cust, 600000)
	in.Now = template().ValidFrom.Add(time.Hour)
	for _, c := range b.codes[3:6] {
		if _, err := e.redeem(c, e.order(cust), in); err != nil {
			t.Fatal(err)
		}
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET valid_from = now() - interval '2 day',
			valid_to = now() - interval '1 day' WHERE code = ANY($1)`, b.codes[8:10])
		return err //nolint:wrapcheck // test
	})
	var st promotion.BatchStats
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		st, err = promotion.GetBatchStats(t.Context(), tx, b.batch.ID, time.Now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if st != (promotion.BatchStats{Given: 4, Used: 3, Expired: 2}) {
		t.Fatalf("số đếm: %+v", st)
	}
}

// ListBatches lọc purpose, mới nhất trước, phân trang bằng con trỏ (limit 2 → trang sau có con trỏ, trang cuối không).
func TestListBatches_PurposeAndCursor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, err := e.ensure(birthdayInput("Đếm", 1))
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.batch(promotion.BatchInput{Name: "Tay", Quantity: 1, Template: template()})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := e.ensure(birthdayInput("Đếm 2", 1))
	if err != nil {
		t.Fatal(err)
	}
	list := func(purpose string, after *uuid.UUID, limit int) ([]uuid.UUID, *uuid.UUID) {
		var ids []uuid.UUID
		var next *uuid.UUID
		if err := e.app(func(tx pgx.Tx) error {
			bs, n, err := promotion.ListBatches(t.Context(), tx, purpose, after, limit)
			for _, x := range bs {
				ids = append(ids, x.ID)
			}
			next = n
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return ids, next
	}
	if ids, next := list(promotion.PurposeBirthday, nil, 50); !slices.Equal(ids, []uuid.UUID{b2.batch.ID, b.batch.ID}) ||
		next != nil {
		t.Fatalf("birthday: %v %v", ids, next)
	}
	ids, next := list("", nil, 2)
	if !slices.Equal(ids, []uuid.UUID{b2.batch.ID, m.ID}) || next == nil {
		t.Fatalf("trang 1: %v %v", ids, next)
	}
	if ids, next = list("", next, 2); !slices.Equal(ids, []uuid.UUID{b.batch.ID}) || next != nil {
		t.Fatalf("trang 2: %v %v", ids, next)
	}
}

// Migration 0505 trên database đã có lô và mã của M2 → không lỗi, lô cũ purpose manual, mã cũ origin crm, chưa trao.
func TestMigration0505_ExistingM2Rows(t *testing.T) {
	t.Parallel()
	_, ownerURL, _ := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0505")
	owner := testkit.OpenDB(t, ownerURL)
	ctx := t.Context()
	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(ctx, "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		var batch uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO promotion.coupon_batches (tenant_id, name, quantity, template, created_by)
			VALUES ($1, 'Đợt M2', 1, '{}', $2) RETURNING id`, tenant, uuid.New()).Scan(&batch); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO promotion.coupons (tenant_id, code, batch_id, name, discount_kind, value,
			max_uses, valid_from, valid_to, created_by) VALUES ($1, 'M2CODE', $2, 'Mã M2', 'fixed', 10000, 1, now(),
			now() + interval '1 day', $3)`, tenant, batch, uuid.New())
		return err //nolint:wrapcheck // test
	})
	if _, err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("migrate 0505: %v", err)
	}
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		var purpose, origin string
		var given *time.Time
		if err := tx.QueryRow(ctx, `SELECT b.purpose, c.origin, c.given_at FROM promotion.coupons c
			JOIN promotion.coupon_batches b ON b.id = c.batch_id`).Scan(&purpose, &origin, &given); err != nil {
			return err //nolint:wrapcheck // test
		}
		if purpose != promotion.PurposeManual || origin != "crm" || given != nil {
			t.Fatalf("sau 0505: %s %s %v", purpose, origin, given)
		}
		return nil
	})
}
