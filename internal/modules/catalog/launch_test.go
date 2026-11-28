package catalog_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// launchEnv là catalogEnv có phân loại kệ (shoptest.SeedShelf) để dựng mẫu nháp đủ / thiếu điều kiện lên kệ.
type launchEnv struct {
	*catalogEnv

	shelf shoptest.Shelf
	now   time.Time
}

func newLaunchEnv(t *testing.T) *launchEnv {
	t.Helper()
	e := &launchEnv{catalogEnv: newCatalogEnv(t), now: time.Date(2026, 12, 1, 20, 0, 0, 0, time.UTC)}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { e.shelf = shoptest.SeedShelf(t, tx, e.tenant); return nil })
	return e
}

// draft tạo mẫu nháp code của người bán seller với phần thiếu lack.
func (e *launchEnv) draft(seller uuid.UUID, code string, lack shoptest.Lack) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id, _ = shoptest.DraftProduct(e.t, tx, e.tenant, seller, e.shelf, code, lack)
		return nil
	})
	return id
}

// fiveDrafts tạo A (đủ), B (không ảnh), C (ảnh đang xử lý), D (giá 0), E (thiếu chat_lieu) của người bán mặc định.
func (e *launchEnv) fiveDrafts() (a, b, c, d, ee uuid.UUID) {
	return e.draft(e.seller, "A1", shoptest.Lack{}), e.draft(e.seller, "B1", shoptest.Lack{NoImage: true}),
		e.draft(e.seller, "C1", shoptest.Lack{ProcessingImage: true}), e.draft(e.seller, "D1", shoptest.Lack{ZeroPrice: true}),
		e.draft(e.seller, "E1", shoptest.Lack{NoAttr: true})
}

func (e *launchEnv) readiness(ids []uuid.UUID) map[uuid.UUID]catalog.ProductReadiness {
	e.t.Helper()
	var rs []catalog.ProductReadiness
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rs, err = catalog.Readiness(e.t.Context(), tx, ids)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("Readiness: %v", err)
	}
	m := make(map[uuid.UUID]catalog.ProductReadiness, len(rs))
	for _, r := range rs {
		m[r.ProductID] = r
	}
	return m
}

func (e *launchEnv) publish(in catalog.PublishInput) (catalog.PublishResult, error) {
	var out catalog.PublishResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().Publish(e.t.Context(), tx, in, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *launchEnv) mustPublish(in catalog.PublishInput) catalog.PublishResult {
	e.t.Helper()
	out, err := e.publish(in)
	if err != nil {
		e.t.Fatalf("Publish: %v", err)
	}
	return out
}

func (e *launchEnv) unpublish(ids []uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var out []uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().Unpublish(e.t.Context(), tx, ids, e.actor, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("Unpublish: %v", err)
	}
	return out
}

// state đọc trạng thái, published_at, version, seller_id của mẫu id (chủ bảng).
func (e *launchEnv) state(id uuid.UUID) (status string, publishedAt *time.Time, version int64, seller uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, published_at, version, seller_id FROM catalog.products WHERE id = $1`, id).
			Scan(&status, &publishedAt, &version, &seller)
	})
	return status, publishedAt, version, seller
}

func (e *launchEnv) events(id uuid.UUID) int {
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1`, id.String())
}

func (e *launchEnv) audits(action string, id uuid.UUID) int {
	return e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = $1 AND entity_id = $2`, action, id.String())
}

func reasonsOf(m map[uuid.UUID]catalog.ProductReadiness, id uuid.UUID) []string {
	r, ok := m[id]
	if !ok {
		return []string{"<không có dòng>"}
	}
	return r.Reasons
}

// [MO01] Readiness(nil): mọi mẫu nháp kèm lý do theo thứ tự cố định; A đủ; mỗi mẫu thiếu đúng một thứ có đúng lý do
// đó; mẫu thiếu nhiều thứ có đủ lý do; mẫu đang bán chỉ có khi gửi mã.
func TestLaunch_Readiness(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a, b, c, d, ee := e.fiveDrafts()
	f := e.draft(e.seller, "F1", shoptest.Lack{NoVariant: true})
	g := e.draft(e.seller, "G1", shoptest.Lack{NoCategory: true, NoTag: true})
	h := e.draft(e.seller, "H1", shoptest.Lack{NoVariant: true, NoImage: true, NoCategory: true, NoAttr: true, NoTag: true})
	got := e.readiness(nil)
	want := map[uuid.UUID][]string{
		a: {}, b: {"no_image"}, c: {"image_processing"}, d: {"no_price"}, ee: {"missing_attr:chat_lieu"},
		f: {"no_variant"}, g: {"no_category", "no_tag"},
		h: {"no_variant", "no_image", "no_category", "missing_attr:chat_lieu", "no_tag"},
	}
	if len(got) != len(want) {
		t.Fatalf("Readiness(nil): %d dòng, mong %d", len(got), len(want))
	}
	for id, w := range want {
		r := got[id]
		if !slices.Equal(reasonsOf(got, id), w) || r.Ready != (len(w) == 0) || r.Status != catalog.StatusDraft {
			t.Errorf("mẫu %s: %+v, mong lý do %v", id, r, w)
		}
	}
	if got[a].Code != "A1" || got[a].Name != "Đầm A1" {
		t.Errorf("mã, tên của A: %+v", got[a])
	}
	e.mustPublish(catalog.PublishInput{IDs: []uuid.UUID{a}})
	if _, ok := e.readiness(nil)[a]; ok {
		t.Errorf("Readiness(nil) còn mẫu đang bán")
	}
	if r := e.readiness([]uuid.UUID{a, uuid.New()}); len(r) != 1 || r[a].Status != catalog.StatusActive || !r[a].Ready {
		t.Errorf("Readiness([A, mã lạ]): %+v", r)
	}
}

// [MO01] Publish(allReady): chỉ A hiện (active, published_at, version + 1, một product.updated Seq = version, một
// audit); bốn mẫu còn lại bỏ qua kèm lý do, vẫn nháp; gọi lại → published rỗng, không thêm sự kiện.
func TestLaunch_PublishAllReady(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a, b, c, d, ee := e.fiveDrafts()
	_, _, v0, _ := e.state(a)
	out := e.mustPublish(catalog.PublishInput{AllReady: true})
	if !slices.Equal(out.Published, []uuid.UUID{a}) || len(out.Skipped) != 4 {
		t.Fatalf("Publish(allReady): %+v", out)
	}
	skipped := map[uuid.UUID][]string{}
	for _, s := range out.Skipped {
		skipped[s.ProductID] = s.Reasons
	}
	for id, w := range map[uuid.UUID]string{b: "no_image", c: "image_processing", d: "no_price", ee: "missing_attr:chat_lieu"} {
		if !slices.Equal(skipped[id], []string{w}) {
			t.Errorf("skipped %s: %v, mong [%s]", id, skipped[id], w)
		}
		if st, _, _, _ := e.state(id); st != "draft" {
			t.Errorf("mẫu bỏ qua %s: %s", id, st)
		}
	}
	st, pub, v, _ := e.state(a)
	if st != "active" || pub == nil || !pub.Equal(e.now) || v != v0+1 {
		t.Fatalf("A sau khi hiện: %s %v v%d (trước v%d)", st, pub, v, v0)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1 AND seq = $2`,
		a.String(), v); n != 1 {
		t.Errorf("product.updated của A (seq %d): %d", v, n)
	}
	if n := e.audits("catalog.product.publish", a); n != 1 {
		t.Errorf("audit hiện A: %d", n)
	}
	again := e.mustPublish(catalog.PublishInput{AllReady: true})
	if len(again.Published) != 0 || len(again.Skipped) != 4 || e.events(a) != 1 {
		t.Errorf("gọi lại: %+v, %d sự kiện", again, e.events(a))
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = ANY($1)`,
		[]string{b.String(), c.String(), d.String(), ee.String()}); n != 0 {
		t.Errorf("mẫu bỏ qua có %d sự kiện", n)
	}
}

// Publish theo mã [A, B] → A hiện, B bỏ qua no_image; mã đã active, mã lạ không ở danh sách nào; đầu vào sai →
// ErrInvalidInput (thiếu cả hai, có cả hai, quá MaxLaunchIDs).
func TestLaunch_PublishIDs(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a, b, _, _, _ := e.fiveDrafts()
	out := e.mustPublish(catalog.PublishInput{IDs: []uuid.UUID{b, a}})
	if !slices.Equal(out.Published, []uuid.UUID{a}) || len(out.Skipped) != 1 || out.Skipped[0].ProductID != b ||
		!slices.Equal(out.Skipped[0].Reasons, []string{"no_image"}) {
		t.Fatalf("Publish([A, B]): %+v", out)
	}
	if again := e.mustPublish(catalog.PublishInput{IDs: []uuid.UUID{a, uuid.New()}}); len(again.Published) != 0 || len(again.Skipped) != 0 {
		t.Errorf("A đã active, mã lạ: %+v", again)
	}
	tooMany := make([]uuid.UUID, catalog.MaxLaunchIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	for name, in := range map[string]catalog.PublishInput{
		"thiếu cả hai": {}, "có cả hai": {IDs: []uuid.UUID{a}, AllReady: true}, "quá nhiều mã": {IDs: tooMany},
	} {
		if _, err := e.publish(in); !errors.Is(err, catalog.ErrInvalidInput) {
			t.Errorf("%s: %v, mong ErrInvalidInput", name, err)
		}
	}
}

// Điều kiện đánh giá lại dưới khóa: trong cùng transaction, Readiness thấy B không ảnh; transaction khác gắn ảnh ready
// cho B và commit; Publish([B]) sau đó (khóa B rồi mới đọc điều kiện) → B hiện.
func TestLaunch_ReevaluatedUnderLock(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	b := e.draft(e.seller, "B1", shoptest.Lack{NoImage: true})
	var out catalog.PublishResult
	if err := e.app(func(tx pgx.Tx) error {
		rs, err := catalog.Readiness(e.t.Context(), tx, []uuid.UUID{b})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if len(rs) != 1 || rs[0].Ready {
			t.Fatalf("trước khi gắn ảnh: %+v", rs)
		}
		e.staff.Exec(t, e.tenant, func(other pgx.Tx) error {
			shoptest.AttachReadyImage(t, other, e.tenant, b)
			return nil
		})
		out, err = e.writer().Publish(e.t.Context(), tx, catalog.PublishInput{IDs: []uuid.UUID{b}}, e.actor, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Published, []uuid.UUID{b}) || len(out.Skipped) != 0 {
		t.Fatalf("Publish([B]) sau khi gắn ảnh: %+v", out)
	}
}

// Hai transaction cùng lúc Publish(allReady) và Publish([A]) → A hiện đúng một lần (một bên published rỗng), một sự
// kiện, một audit.
func TestLaunch_ConcurrentPublish(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a, _, _, _, _ := e.fiveDrafts()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		hits int
	)
	for _, in := range []catalog.PublishInput{{AllReady: true}, {IDs: []uuid.UUID{a}}} {
		wg.Go(func() {
			out, err := e.publish(in)
			if err != nil {
				t.Errorf("Publish %+v: %v", in, err)
				return
			}
			mu.Lock()
			hits += len(out.Published)
			mu.Unlock()
		})
	}
	wg.Wait()
	if hits != 1 || e.events(a) != 1 || e.audits("catalog.product.publish", a) != 1 {
		t.Fatalf("A hiện %d lần, %d sự kiện, %d audit", hits, e.events(a), e.audits("catalog.product.publish", a))
	}
}

// Unpublish([A, B]): chỉ A (đang bán) về nháp, published_at NULL, có product.updated và audit; giá biến thể không đổi;
// gọi lại → rỗng.
func TestLaunch_Unpublish(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a, b, _, _, _ := e.fiveDrafts()
	e.mustPublish(catalog.PublishInput{IDs: []uuid.UUID{a}})
	priceOf := func() (p int64, v int64) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT price, version FROM catalog.variants WHERE product_id = $1`, a).Scan(&p, &v)
		})
		return p, v
	}
	p0, pv0 := priceOf()
	if got := e.unpublish([]uuid.UUID{b, a}); !slices.Equal(got, []uuid.UUID{a}) {
		t.Fatalf("Unpublish([B, A]): %v", got)
	}
	st, pub, v, _ := e.state(a)
	if st != "draft" || pub != nil || e.events(a) != 2 || e.audits("catalog.product.unpublish", a) != 1 {
		t.Fatalf("A sau khi ẩn: %s %v v%d, %d sự kiện", st, pub, v, e.events(a))
	}
	if p, pv := priceOf(); p != p0 || pv != pv0 {
		t.Errorf("biến thể đổi khi ẩn: %d v%d → %d v%d", p0, pv0, p, pv)
	}
	if got := e.unpublish([]uuid.UUID{a}); len(got) != 0 {
		t.Errorf("ẩn lại mẫu nháp: %v", got)
	}
}

// [KH14] Hai người bán: mẫu đủ điều kiện của người bán thứ hai cũng hiện bằng allReady; seller_id không đổi.
func TestLaunch_TwoSellers(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	a := e.draft(e.seller, "A1", shoptest.Lack{})
	p := e.draft(e.partner, "P1", shoptest.Lack{})
	out := e.mustPublish(catalog.PublishInput{AllReady: true})
	want := []uuid.UUID{a, p}
	slices.SortFunc(want, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	if !slices.Equal(out.Published, want) {
		t.Fatalf("Publish(allReady) hai người bán: %v, mong %v", out.Published, want)
	}
	if st, _, _, s := e.state(p); st != "active" || s != e.partner {
		t.Errorf("mẫu người bán thứ hai: %s, người bán %s", st, s)
	}
	if got := e.unpublish([]uuid.UUID{p}); !slices.Equal(got, []uuid.UUID{p}) {
		t.Errorf("ẩn mẫu người bán thứ hai: %v", got)
	}
}

// 200 mẫu đủ điều kiện, một lần gọi → hiện cả 200, mỗi mẫu một sự kiện (thời gian ghi log; ngưỡng 2 s ở
// launch_perf_test.go, nhãn perf).
func TestLaunch_Publish200(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	e.seed200()
	start := time.Now()
	out := e.mustPublish(catalog.PublishInput{AllReady: true})
	t.Logf("Publish 200 mẫu: %v", time.Since(start))
	if len(out.Published) != 200 || len(out.Skipped) != 0 {
		t.Fatalf("Publish 200: %d hiện, %d bỏ qua", len(out.Published), len(out.Skipped))
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated'`); n != 200 {
		t.Errorf("sự kiện: %d", n)
	}
}

func (e *launchEnv) seed200() {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		for i := range 200 {
			shoptest.DraftProduct(e.t, tx, e.tenant, e.seller, e.shelf, fmt.Sprintf("N%03d", i), shoptest.Lack{})
		}
		return nil
	})
}

// ReadinessPage: lọc ready / not_ready / mọi mẫu nháp, phân trang theo mã, đếm cả shop không theo trang.
func TestLaunch_ReadinessPage(t *testing.T) {
	t.Parallel()
	e := newLaunchEnv(t)
	e.fiveDrafts()
	e.draft(e.partner, "P1", shoptest.Lack{})
	page := func(f catalog.ReadinessFilter, after uuid.UUID, limit int) catalog.ReadinessPageResult {
		var out catalog.ReadinessPageResult
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = catalog.ReadinessPage(t.Context(), tx, f, after, limit)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var all []uuid.UUID
	for after, n := uuid.Nil, 0; n < 10; n++ {
		p := page(catalog.ReadinessAll, after, 2)
		if p.ReadyCount != 2 || p.NotReadyCount != 4 {
			t.Fatalf("đếm: %+v", p)
		}
		for _, it := range p.Items {
			all = append(all, it.ProductID)
		}
		if p.Next == uuid.Nil {
			break
		}
		after = p.Next
	}
	if len(all) != 6 || !slices.IsSortedFunc(all, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) }) {
		t.Fatalf("mọi trang: %v", all)
	}
	if r := page(catalog.ReadinessReady, uuid.Nil, 50); len(r.Items) != 2 || !r.Items[0].Ready || r.Next != uuid.Nil {
		t.Errorf("ready: %+v", r)
	}
	if r := page(catalog.ReadinessNotReady, uuid.Nil, 50); len(r.Items) != 4 || r.Items[0].Ready {
		t.Errorf("not_ready: %+v", r)
	}
	for _, bad := range []int{0, catalog.MaxReadinessPage + 1} {
		if err := e.app(func(tx pgx.Tx) error {
			_, err := catalog.ReadinessPage(t.Context(), tx, catalog.ReadinessAll, uuid.Nil, bad)
			return err //nolint:wrapcheck // test
		}); !errors.Is(err, catalog.ErrInvalidInput) {
			t.Errorf("limit %d: %v", bad, err)
		}
	}
}
