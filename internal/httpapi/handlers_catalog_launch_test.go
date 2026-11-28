package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

const (
	launchOwner  = "mo-ban@elimaz.test"
	launchViewer = "xem-ke@elimaz.test"
	launchPath   = "/api/v1/catalog/launch/"
)

type launchAPIEnv struct {
	t          *testing.T
	staff      *testkit.Staff
	keys       *cfaccesstest.AccessKeys
	srv        http.Handler
	tenant     uuid.UUID
	a, b, c, d uuid.UUID
	e          uuid.UUID
}

// newLaunchAPIEnv: tenant có người bán mặc định, phân loại kệ và năm mẫu nháp A (đủ), B (không ảnh), C (ảnh đang xử
// lý), D (giá 0), E (thiếu chat_lieu); người có catalog.launch.publish + catalog.read và người chỉ có catalog.read.
func newLaunchAPIEnv(t *testing.T) *launchAPIEnv {
	t.Helper()
	e := &launchAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		seller, err := catalog.EnsureDefaultSeller(t.Context(), tx, e.tenant)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s := shoptest.SeedShelf(t, tx, e.tenant)
		for _, p := range []struct {
			dst  *uuid.UUID
			code string
			lack shoptest.Lack
		}{
			{&e.a, "A1", shoptest.Lack{}},
			{&e.b, "B1", shoptest.Lack{NoImage: true}},
			{&e.c, "C1", shoptest.Lack{ProcessingImage: true}},
			{&e.d, "D1", shoptest.Lack{ZeroPrice: true}},
			{&e.e, "E1", shoptest.Lack{NoAttr: true}},
		} {
			*p.dst, _ = shoptest.DraftProduct(t, tx, e.tenant, seller, s, p.code, p.lack)
		}
		return nil
	})
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, launchOwner, "active"),
		[]string{"catalog.launch.publish", "catalog.read"}, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, launchViewer, "active"), []string{"catalog.read"}, "all", "")
	e.srv = testServer(t, e.staff, e.keys, 0)
	return e
}

func (e *launchAPIEnv) post(op, key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, http.MethodPost, launchPath+op, launchOwner, key, body)
}

type launchResult struct {
	Published   []uuid.UUID `json:"published"`
	Unpublished []uuid.UUID `json:"unpublished"`
	Skipped     []struct {
		ProductID uuid.UUID `json:"productId"`
		Reasons   []string  `json:"reasons"`
		Ready     bool      `json:"ready"`
		Status    string    `json:"status"`
	} `json:"skipped"`
}

func (e *launchAPIEnv) result(rec *httptest.ResponseRecorder) launchResult {
	e.t.Helper()
	var r launchResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &r) != nil {
		e.t.Fatalf("mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	return r
}

func (e *launchAPIEnv) events(id uuid.UUID) int {
	return e.staff.CountIn(e.t, e.tenant,
		`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1`, id.String())
}

func (e *launchAPIEnv) status(id uuid.UUID) string {
	var s string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status FROM catalog.products WHERE id = $1`, id).Scan(&s)
	})
	return s
}

// [MO01] publishProducts {allReady}: published [A], skipped 4 kèm lý do, A active, một product.updated; gửi lại cùng
// khóa → cùng thân, Idempotent-Replayed: true, không thêm sự kiện. Thiếu cả hai → 400 invalid_input; người chỉ có
// catalog.read → 403.
func TestLaunchAPI_PublishAllReady(t *testing.T) {
	t.Parallel()
	e := newLaunchAPIEnv(t)
	rec := e.post("publish", "p1", `{"allReady":true}`)
	r := e.result(rec)
	reasons := map[uuid.UUID][]string{}
	for _, s := range r.Skipped {
		reasons[s.ProductID] = s.Reasons
	}
	if !slices.Equal(r.Published, []uuid.UUID{e.a}) || len(r.Skipped) != 4 ||
		!slices.Equal(reasons[e.b], []string{"no_image"}) || !slices.Equal(reasons[e.c], []string{"image_processing"}) ||
		!slices.Equal(reasons[e.d], []string{"no_price"}) || !slices.Equal(reasons[e.e], []string{"missing_attr:chat_lieu"}) {
		t.Fatalf("publish allReady: %s", rec.Body.String())
	}
	if e.status(e.a) != "active" || e.events(e.a) != 1 {
		t.Fatalf("A: %s, %d sự kiện", e.status(e.a), e.events(e.a))
	}
	again := e.post("publish", "p1", `{"allReady":true}`)
	if !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) || again.Header().Get("Idempotent-Replayed") != "true" || e.events(e.a) != 1 {
		t.Fatalf("gửi lại cùng khóa: %d %s, %d sự kiện", again.Code, again.Body.String(), e.events(e.a))
	}
	expect(t, "thiếu productIds và allReady", e.post("publish", "p2", `{}`), http.StatusBadRequest, "invalid_input")
	expect(t, "chỉ catalog.read", serveAs(e.srv, e.keys, http.MethodPost, launchPath+"publish", launchViewer, "p3",
		`{"allReady":true}`), http.StatusForbidden, "forbidden")
}

// 50 yêu cầu song song cùng khóa → A hiện một lần, một sự kiện, mọi câu 200 cùng thân hoặc 409 request_in_progress;
// hai yêu cầu khác khóa cùng lúc → tổng cộng A hiện một lần.
func TestLaunchAPI_PublishParallel(t *testing.T) {
	t.Parallel()
	e := newLaunchAPIEnv(t)
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 50)
	for i := range recs {
		wg.Go(func() { recs[i] = e.post("publish", "same", `{"allReady":true}`) })
	}
	wg.Wait()
	var first []byte
	for i, rec := range recs {
		switch {
		case rec.Code == http.StatusConflict && bytes.Contains(rec.Body.Bytes(), []byte("request_in_progress")):
		case rec.Code != http.StatusOK:
			t.Errorf("yêu cầu %d: %d %s", i, rec.Code, rec.Body.String())
		case first == nil:
			first = rec.Body.Bytes()
		case !bytes.Equal(first, rec.Body.Bytes()):
			t.Errorf("yêu cầu %d: thân khác %s", i, rec.Body.String())
		}
	}
	if e.events(e.a) != 1 {
		t.Fatalf("50 song song cùng khóa: %d sự kiện của A", e.events(e.a))
	}

	f := newLaunchAPIEnv(t)
	hits := make([]int, 2)
	for i, body := range []string{`{"allReady":true}`, fmt.Sprintf(`{"productIds":[%q]}`, f.a)} {
		wg.Go(func() { hits[i] = len(f.result(f.post("publish", fmt.Sprintf("k%d", i), body)).Published) })
	}
	wg.Wait()
	if hits[0]+hits[1] != 1 || f.events(f.a) != 1 {
		t.Fatalf("hai khóa khác: hiện %v, %d sự kiện", hits, f.events(f.a))
	}
}

// publishProducts {productIds: [A, B]} → A hiện, B bỏ qua no_image; unpublishProducts [A] → A nháp, thêm một sự
// kiện; getCatalogLaunchReadiness: đếm, lọc, trang.
func TestLaunchAPI_IDsUnpublishReadiness(t *testing.T) {
	t.Parallel()
	e := newLaunchAPIEnv(t)
	r := e.result(e.post("publish", "i1", fmt.Sprintf(`{"productIds":[%q,%q]}`, e.a, e.b)))
	if !slices.Equal(r.Published, []uuid.UUID{e.a}) || len(r.Skipped) != 1 || r.Skipped[0].ProductID != e.b ||
		!slices.Equal(r.Skipped[0].Reasons, []string{"no_image"}) || r.Skipped[0].Ready || r.Skipped[0].Status != "draft" {
		t.Fatalf("publish [A, B]: %+v", r)
	}

	get := func(q string) map[string]any {
		rec := serveAs(e.srv, e.keys, http.MethodGet, launchPath+"readiness"+q, launchViewer, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("readiness%s: %d %s", q, rec.Code, rec.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	all := get("?limit=3")
	if all["readyCount"] != float64(0) || all["notReadyCount"] != float64(4) || len(all["items"].([]any)) != 3 || //nolint:forcetypeassert // test
		all["nextCursor"] == nil {
		t.Fatalf("readiness trang 1: %v", all)
	}
	rest := get("?limit=3&cursor=" + all["nextCursor"].(string))      //nolint:forcetypeassert // test
	if len(rest["items"].([]any)) != 1 || rest["nextCursor"] != nil { //nolint:forcetypeassert // test
		t.Fatalf("readiness trang 2: %v", rest)
	}
	if ready := get("?status=ready"); len(ready["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("readiness ready: %v", ready)
	}
	expect(t, "cursor sai", serveAs(e.srv, e.keys, http.MethodGet, launchPath+"readiness?cursor=abc", launchViewer, "", ""),
		http.StatusBadRequest, "invalid_input")

	u := e.result(e.post("unpublish", "u1", fmt.Sprintf(`{"productIds":[%q,%q]}`, e.a, e.b)))
	if !slices.Equal(u.Unpublished, []uuid.UUID{e.a}) || e.status(e.a) != "draft" || e.events(e.a) != 2 {
		t.Fatalf("unpublish: %+v, A %s, %d sự kiện", u, e.status(e.a), e.events(e.a))
	}
	if n := get("")["readyCount"]; n != float64(1) {
		t.Errorf("sau khi ẩn A: readyCount %v", n)
	}
}
