package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Hàng hoàn của đơn cũ qua API (kế hoạch M7, Task 264 = M7-19; MO08): 201, 409 legacy_return_exists kèm existingId,
// chống trùng, quyền theo kho, audit không chứa ghi chú.

const (
	legacyAll = "hoan-all@elimaz.test" // inventory.legacy_return.create mọi kho
	legacyW1  = "hoan-w1@elimaz.test"  // inventory.legacy_return.create chỉ kho w1
)

func newLegacyAPIEnv(t *testing.T) *invAPIEnv {
	t.Helper()
	e := newInvAPIEnv(t)
	perm := []string{"inventory.legacy_return.create"}
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, legacyAll, "active"), perm, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, legacyW1, "active"), perm, "warehouse", e.w1.String())
	return e
}

func legacyBody(wh uuid.UUID, ref, note, lines string) string {
	return fmt.Sprintf(`{"warehouseId":%q,"reference":%q,"note":%q,"lines":%s}`, wh, ref, note, lines)
}

func (e *invAPIEnv) legacy(email, key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(http.MethodPost, "/api/v1/inventory/legacy-returns", email, key, body)
}

func TestLegacyReturnAPI_CreateDuplicateAudit(t *testing.T) {
	t.Parallel()
	e := newLegacyAPIEnv(t)
	a, b := e.variant(e.w1, 0), e.variant(e.w1, 0)
	lines := fmt.Sprintf(`[{"variantId":%q,"qty":2,"condition":"sellable"},{"variantId":%q,"qty":1,"condition":"damaged"}]`, a, b)
	const note = "Chị Mai 0911222333 trả hàng"
	r := mustOK(t, "createLegacyReturn", e.legacy(legacyAll, "k1", legacyBody(e.w1, "vtp 1234 5678", note, lines)), http.StatusCreated)
	id, _ := r["id"].(string)
	if r["reference"] != "vtp 1234 5678" || r["damageDocumentId"] == nil || len(r["lines"].([]any)) != 2 { //nolint:forcetypeassert // test
		t.Fatalf("thân 201 sai: %v", r)
	}
	if e.onHand(a, e.w1) != 2 || e.onHand(b, e.w1) != 1 {
		t.Fatalf("on_hand: a %d b %d", e.onHand(a, e.w1), e.onHand(b, e.w1))
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM inventory.stock_balances WHERE variant_id = $1
		AND damaged = 1`, b); n != 1 {
		t.Fatal("dòng hỏng phải damaged 1")
	}

	wantLegacyExists(t, e.legacy(legacyAll, "k2", legacyBody(e.w1, "VTP12345678", "", lines)), id)
	if e.onHand(a, e.w1) != 2 {
		t.Fatal("409 không được đổi tồn")
	}
	g := mustOK(t, "getLegacyReturn", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns/"+id, legacyW1, "", ""), http.StatusOK)
	if g["id"] != id || g["stockDocumentId"] != r["stockDocumentId"] {
		t.Fatalf("đọc lại: %v", g)
	}
	expect(t, "phiếu không có", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns/"+uuid.NewString(), legacyAll, "", ""),
		http.StatusNotFound, "not_found")

	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'inventory.legacy_return'
		AND entity_id = $1 AND after->>'reference' = 'vtp 1234 5678' AND jsonb_array_length(after->'lines') = 2`, id); n != 1 {
		t.Fatalf("audit: mong 1 dòng, có %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE (coalesce(after::text, '') || coalesce(before::text, '')) LIKE '%Chị Mai%'
		   OR (coalesce(after::text, '') || coalesce(before::text, '')) LIKE '%0911222333%'`); n != 0 {
		t.Fatalf("audit chứa ghi chú: %d dòng", n)
	}
	if strings.Contains(e.logs.text(), "0911222333") || strings.Contains(e.logs.text(), "Chị Mai") {
		t.Fatal("log chứa ghi chú")
	}
}

// 50 yêu cầu song song cùng khóa → 1 phiếu; hai khóa khác cùng tham chiếu cùng lúc → 1 thành công, 1 409.
func TestLegacyReturnAPI_Concurrent(t *testing.T) {
	t.Parallel()
	e := newLegacyAPIEnv(t)
	v := e.variant(e.w1, 0)
	body := legacyBody(e.w1, "GHN-0001", "", fmt.Sprintf(`[{"variantId":%q,"qty":1,"condition":"sellable"}]`, v))
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.legacy(legacyAll, "same-50", body) })
	}
	wg.Wait()
	created := 0
	for _, rec := range recs {
		switch {
		case rec.Code == http.StatusCreated:
			created++
		case rec.Code == http.StatusConflict && errCode(t, rec) == "request_in_progress":
		default:
			t.Errorf("mã lạ %d %s", rec.Code, rec.Body.String())
		}
	}
	if created == 0 || e.onHand(v, e.w1) != 1 ||
		e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.legacy_returns") != 1 {
		t.Fatalf("cùng khóa: %d lần 201, on_hand %d", created, e.onHand(v, e.w1))
	}

	body2 := legacyBody(e.w1, "SPX 9", "", fmt.Sprintf(`[{"variantId":%q,"qty":1,"condition":"damaged"}]`, v))
	pair := make([]*httptest.ResponseRecorder, 2)
	for i := range pair {
		wg.Go(func() { pair[i] = e.legacy(legacyAll, fmt.Sprintf("k-%d", i), body2) })
	}
	wg.Wait()
	codes := map[int]int{pair[0].Code: 1}
	codes[pair[1].Code]++
	if codes[http.StatusCreated] != 1 || codes[http.StatusConflict] != 1 {
		t.Fatalf("hai khóa: %d %s / %d %s", pair[0].Code, pair[0].Body.String(), pair[1].Code, pair[1].Body.String())
	}
	winner := pair[0]
	if winner.Code != http.StatusCreated {
		winner = pair[1]
	}
	for _, rec := range pair {
		if rec.Code == http.StatusConflict {
			wantLegacyExists(t, rec, decodeJSON(t, winner)["id"].(string)) //nolint:forcetypeassert // test
		}
	}
	if e.onHand(v, e.w1) != 2 || e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.legacy_returns") != 2 {
		t.Fatalf("sau hai khóa: on_hand %d", e.onHand(v, e.w1))
	}
}

// qty 0 → 400; người chỉ có quyền ở w1 nhập vào w2, đọc phiếu w2, lọc w2 → 403; danh sách chỉ thấy kho được giao.
func TestLegacyReturnAPI_InputAndScope(t *testing.T) {
	t.Parallel()
	e := newLegacyAPIEnv(t)
	v := e.variant(e.w1, 0)
	line := func(q int) string { return fmt.Sprintf(`[{"variantId":%q,"qty":%d,"condition":"sellable"}]`, v, q) }
	expect(t, "qty 0", e.legacy(legacyAll, "q0", legacyBody(e.w1, "R-0001", "", line(0))), http.StatusBadRequest, "")
	expect(t, "qty 1001", e.legacy(legacyAll, "q1", legacyBody(e.w1, "R-0001", "", line(1001))), http.StatusBadRequest, "")
	expect(t, "tham chiếu chỉ khoảng trắng", e.legacy(legacyAll, "q2", legacyBody(e.w1, "a  b", "", line(1))),
		http.StatusBadRequest, "invalid_input")
	expect(t, "nhập vào w2 bằng quyền w1", e.legacy(legacyW1, "s1", legacyBody(e.w2, "R-0002", "", line(1))),
		http.StatusForbidden, "forbidden")
	expect(t, "không có quyền", e.legacy(invManager, "s2", legacyBody(e.w1, "R-0003", "", line(1))),
		http.StatusForbidden, "forbidden")
	if e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.stock_documents") != 0 {
		t.Fatal("yêu cầu bị từ chối không được để lại phiếu")
	}

	r1 := mustOK(t, "w1", e.legacy(legacyW1, "c1", legacyBody(e.w1, "R-0004", "", line(1))), http.StatusCreated)
	r2 := mustOK(t, "w2", e.legacy(legacyAll, "c2", legacyBody(e.w2, "R-0005", "", line(1))), http.StatusCreated)
	expect(t, "đọc phiếu w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns/"+r2["id"].(string), //nolint:forcetypeassert // test
		legacyW1, "", ""), http.StatusForbidden, "forbidden")
	expect(t, "lọc w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?warehouseId="+e.w2.String(),
		legacyW1, "", ""), http.StatusForbidden, "forbidden")
	lst := mustOK(t, "list w1", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns", legacyW1, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != r1["id"] { //nolint:forcetypeassert // test
		t.Fatalf("người kho w1 chỉ thấy phiếu w1: %v", lst)
	}
	lst = mustOK(t, "list all trang 1", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?limit=1", legacyAll, "", ""),
		http.StatusOK)
	items, _ := lst["items"].([]any)
	next, _ := lst["nextCursor"].(string)
	if len(items) != 1 || items[0].(map[string]any)["id"] != r2["id"] || next == "" { //nolint:forcetypeassert // test
		t.Fatalf("trang 1: %v", lst)
	}
	lst = mustOK(t, "list all trang 2", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?limit=1&cursor="+next,
		legacyAll, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != r1["id"] || lst["nextCursor"] != nil { //nolint:forcetypeassert // test
		t.Fatalf("trang 2: %v", lst)
	}
	lst = mustOK(t, "list ngày xa", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?from=2020-01-01&to=2020-01-31",
		legacyAll, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("khoảng ngày cũ phải rỗng: %v", lst)
	}
	// Ngày lấy từ giờ tạo do database ghi (một đồng hồ), không từ giờ máy test.
	vnDay := func(r map[string]any) string {
		at, err := time.Parse(time.RFC3339Nano, r["createdAt"].(string)) //nolint:forcetypeassert // test
		if err != nil {
			t.Fatalf("createdAt: %v", err)
		}
		return at.In(vietnam).Format(time.DateOnly)
	}
	lst = mustOK(t, "list ngày tạo", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?from="+vnDay(r1)+"&to="+vnDay(r2),
		legacyAll, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 2 { //nolint:forcetypeassert // test
		t.Fatalf("khoảng ngày tạo phải đủ hai phiếu: %v", lst)
	}
	expect(t, "from sau to", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?from=2026-02-02&to=2026-02-01",
		legacyAll, "", ""), http.StatusBadRequest, "invalid_input")
	expect(t, "con trỏ sai", e.do(http.MethodGet, "/api/v1/inventory/legacy-returns?cursor=abc", legacyAll, "", ""),
		http.StatusBadRequest, "invalid_input")
}

// wantLegacyExists: 409 legacy_return_exists, thân đúng ba trường error, request_id, existingId = id.
func wantLegacyExists(t *testing.T, rec *httptest.ResponseRecorder, id string) {
	t.Helper()
	b := decodeJSON(t, rec)
	if rec.Code != http.StatusConflict || b["error"] != "legacy_return_exists" || b["existingId"] != id ||
		b["request_id"] == "" || len(b) != 3 {
		t.Fatalf("mong 409 legacy_return_exists existingId %s, có %d %s", id, rec.Code, rec.Body.String())
	}
}
