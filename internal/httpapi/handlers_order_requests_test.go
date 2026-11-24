package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const (
	reqSale   = "ban-hang-yc@elimaz.test"
	reqOwner  = "chu-yc@elimaz.test"
	reqNobody = "kho-yc@elimaz.test"
	webFiles  = "/api/v1/web/files"
)

// reqAPIEnv: orderAPIEnv có cài đặt M2, kho tệp trong bộ nhớ, bus ghi báo hiệu, hai khóa dịch vụ (web.requests.write,
// web.orders.read); nhân viên Bán hàng (vai trò mẫu), Chủ, và một người chỉ có order.read.
type reqAPIEnv struct {
	*orderAPIEnv
	mem              *files.MemStore
	bus              *realtimetest.RecordingBus
	writeKey, readKy string
	seq              int
}

func newReqAPIEnv(t *testing.T, store files.FileStore) *reqAPIEnv {
	t.Helper()
	order.RegisterFilePurposes()
	e := &reqAPIEnv{orderAPIEnv: newOrderAPIEnv(t), mem: filestest.MemFileStore(t), bus: &realtimetest.RecordingBus{}}
	e.m2Prepare()
	tenant := e.shop.Tenant
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, reqSale, "active"), rolePerms(t, "sale"), "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, reqNobody, "active"), []string{"order.read"}, "all", "")
	owner := e.staff.User(t, tenant, reqOwner, "active")
	e.staff.Grant(t, tenant, owner, rolePerms(t, "owner"), "all", "")
	u, err := identity.LoadUser(t.Context(), e.staff.App, reqOwner)
	if err != nil {
		t.Fatal(err)
	}
	e.writeKey, e.readKy = e.serviceKey(u, "web.requests.write"), e.serviceKey(u, "web.orders.read")
	if store == nil {
		store = e.mem
	}
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Files, d.Realtime = store, e.bus
	d.ServiceKeys = NewServiceKeyVerifier(e.staff.App, nil, ServiceKeyOptions{Logger: d.Logger})
	e.srv = NewServer(d)
	return e
}

func (e *reqAPIEnv) serviceKey(owner identity.AuthUser, perm identity.Permission) string {
	e.t.Helper()
	var full string
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.shop.Tenant, func(tx pgx.Tx) error {
		var err error
		_, full, err = identity.CreateServiceKey(e.t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Website " + string(perm), Permissions: []identity.Permission{perm}, Actor: owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return full
}

// web gửi yêu cầu của website bằng khóa key; idem khác rỗng thì đặt Idempotency-Key.
func (e *reqAPIEnv) web(method, path, key, idem, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, body)
	req.Header.Set("Authorization", "Bearer "+key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *reqAPIEnv) nextKey() string {
	e.seq++
	return fmt.Sprintf("k%d", e.seq)
}

func (e *reqAPIEnv) upload(contentType string, data []byte) *httptest.ResponseRecorder {
	return e.web(http.MethodPost, webFiles, e.writeKey, e.nextKey(), contentType, bytes.NewReader(data))
}

// webOrder tạo đơn confirmed qua API nhân viên (V ×qty) rồi đổi thành đơn kênh web mã ngoài ref (chủ bảng; đường tạo
// đơn web thật ở Task 193, 194); delivered: giao qua đường của M3.
func (e *reqAPIEnv) webOrder(ref string, v uuid.UUID, qty int, delivered bool) uuid.UUID {
	e.t.Helper()
	o := e.mustCreate(e.nextKey(), m2Body("confirmed", fullAddr, 0, m2Line(v, qty, 0)))
	id := uuid.MustParse(o["id"].(string))
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE ord.orders SET channel = 'web', external_ref = $2 WHERE id = $1`, id, ref)
		return err //nolint:wrapcheck // test
	})
	if delivered {
		e.deliver(id)
	}
	return id
}

func (e *reqAPIEnv) createRequest(ref, body string) *httptest.ResponseRecorder {
	return e.web(http.MethodPost, "/api/v1/web/orders/"+ref+"/requests", e.writeKey, e.nextKey(), "application/json",
		strings.NewReader(body))
}

func exchangeBody(v uuid.UUID, reason string, fileIDs ...string) string {
	ids := `[]`
	if len(fileIDs) > 0 {
		ids = `["` + strings.Join(fileIDs, `","`) + `"]`
	}
	return fmt.Sprintf(`{"kind":"exchange","reason":"SIZE","message":"Áo chật quá, em muốn đổi size L",
		"conditionConfirmed":true,"lines":[{"variantId":"%s","qty":1,"reason":%q}],"fileIds":%s}`, v, reason, ids)
}

func (e *reqAPIEnv) requestID(ref string) string {
	e.t.Helper()
	var id string
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id::text FROM ord.order_requests WHERE ref = $1`, ref).Scan(&id) //nolint:lll // test
	})
	return id
}

func (e *reqAPIEnv) fileOf(kind string) string {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		switch kind {
		case "ready":
			id = filestest.Image(e.t, tx, e.shop.Tenant, order.RequestPhotoPurposeKey)
		default:
			id = filestest.Rejected(e.t, tx, e.shop.Tenant, order.RequestPhotoPurposeKey)
		}
		return nil
	})
	return id.String()
}

// Ảnh khách gửi (R10): image/gif → 422 file_type_not_allowed, không dòng tệp, không lệnh; text/html → 415; 6 MiB + 1
// byte → 413; tệp HTML khai image/jpeg → 201 (CRM không tự kiểm nội dung): dòng uploaded, mục đích order_request_photo,
// bucket private, ảnh thô chỉ ở crm-tmp khóa tmp/<tenant>/<file>, 1 lệnh file.process_private, 0 file.process; gửi lại
// cùng khóa → cùng fileId, không ghi thêm; khóa chỉ web.orders.read → 403; audit không có nội dung ảnh.
func TestOrderRequestAPI_Upload(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, nil)
	expect(t, "gif", e.upload("image/gif", []byte("GIF89a")), http.StatusUnprocessableEntity, "file_type_not_allowed")
	expect(t, "html", e.upload("text/html", []byte("<html>")), http.StatusUnsupportedMediaType, "unsupported_media_type")
	expect(t, "6 MiB + 1", e.upload("image/jpeg", make([]byte, 6<<20+1)), http.StatusRequestEntityTooLarge, "")
	if n := e.count(`SELECT count(*) FROM platform.files`); n != 0 || len(e.mem.Keys(files.BucketTmp)) != 0 {
		t.Fatalf("bị từ chối mà có %d dòng tệp, %v", n, e.mem.Keys(files.BucketTmp))
	}
	expect(t, "khóa đọc", e.web(http.MethodPost, webFiles, e.readKy, "x1", "image/jpeg", strings.NewReader("abc")),
		http.StatusForbidden, "forbidden")

	fake := []byte("<html><script>alert(1)</script></html>")
	rec := e.web(http.MethodPost, webFiles, e.writeKey, "up1", "image/jpeg", bytes.NewReader(fake))
	body := e.ok("ảnh HTML giả jpeg", rec, http.StatusCreated)
	id := uuid.MustParse(body["fileId"].(string))
	if n := e.count(`SELECT count(*) FROM platform.files WHERE id = $1 AND purpose = 'order_request_photo'
		AND bucket = 'private' AND status = 'uploaded'`, id); n != 1 {
		t.Fatalf("dòng tệp: %d", n)
	}
	if ks := e.mem.Keys(files.BucketTmp); len(ks) != 1 || ks[0] != files.TmpKey(e.shop.Tenant, id) {
		t.Fatalf("crm-tmp: %v", ks)
	}
	for _, b := range []files.Bucket{files.BucketPublic, files.BucketOriginals, files.BucketPrivate} {
		if ks := e.mem.Keys(b); len(ks) != 0 {
			t.Fatalf("%s có đối tượng: %v", b, ks)
		}
	}
	jobs := func(kind string) int {
		return e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`, kind, id.String())
	}
	if jobs("file.process_private") != 1 || jobs("file.process") != 0 {
		t.Fatalf("lệnh: private %d, public %d", jobs("file.process_private"), jobs("file.process"))
	}
	again := e.web(http.MethodPost, webFiles, e.writeKey, "up1", "image/jpeg", bytes.NewReader(fake))
	if b := e.ok("gửi lại", again, http.StatusCreated); b["fileId"] != id.String() || len(e.mem.Keys(files.BucketTmp)) != 1 {
		t.Fatalf("gửi lại: %v, %v", b, e.mem.Keys(files.BucketTmp))
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.request_photo.upload'
		AND actor_type = 'service' AND entity_id = $1 AND after->>'contentType' = 'image/jpeg'`, id.String()); n != 1 {
		t.Fatalf("audit tải ảnh: %d", n)
	}
}

// failingStore: kho tệp Put luôn lỗi (kho tạm không ghi được).
type failingStore struct{ files.FileStore }

func (failingStore) Put(context.Context, files.Bucket, string, io.Reader, int64, string) error {
	return errors.New("kho tạm không trả lời")
}

// Kho tạm lỗi → 503 retry_later, không dòng platform.files, không lưu theo khóa (gửi lại xử lý lại).
func TestOrderRequestAPI_UploadStoreDown(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, failingStore{})
	rec := e.web(http.MethodPost, webFiles, e.writeKey, "up1", "image/png", strings.NewReader("png"))
	expect(t, "kho lỗi", rec, http.StatusServiceUnavailable, "retry_later")
	if n := e.count(`SELECT count(*) FROM platform.files`); n != 0 {
		t.Fatalf("có %d dòng tệp", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.idempotency_keys WHERE key = 'up1' AND status = 'completed'`); n != 0 {
		t.Fatalf("lưu theo khóa: %d", n)
	}
}

// [WB04] Website gửi yêu cầu đổi kèm ảnh; nhân viên thấy, ảnh đang xử lý không có url, ảnh ready có link ký 10 phút
// tới biến thể ở bucket riêng tư, ảnh bị từ chối có status rejected; nhân viên trả lời (website thấy) và ghi chú nội
// bộ (website không thấy); audit chỉ tên trường; báo hiệu cho order.requests.read.
func TestOrderRequestAPI_WebAndStaffFlow(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, nil)
	v := e.variant(e.shop.Seller, 250000, 5)
	e.webOrder("W-1", v, 2, true)
	up := e.ok("tải ảnh", e.upload("image/jpeg", []byte("jpeg")), http.StatusCreated)["fileId"].(string)
	ready := e.fileOf("ready")

	expect(t, "ảnh bị imager từ chối", e.createRequest("W-1", exchangeBody(v, "DEFECT", e.fileOf("rejected"))),
		http.StatusUnprocessableEntity, "file_rejected")
	expect(t, "ảnh lạ", e.createRequest("W-1", exchangeBody(v, "DEFECT", uuid.NewString())),
		http.StatusUnprocessableEntity, "file_not_found")
	expect(t, "thiếu ảnh", e.createRequest("W-1", exchangeBody(v, "DEFECT")), http.StatusUnprocessableEntity, "photo_required")
	expect(t, "đơn lạ", e.createRequest("W-LA", exchangeBody(v, "FIT")), http.StatusNotFound, "not_found")
	expect(t, "khóa đọc", e.web(http.MethodPost, "/api/v1/web/orders/W-1/requests", e.readKy, "x2", "application/json",
		strings.NewReader(exchangeBody(v, "FIT"))), http.StatusForbidden, "forbidden")

	c := e.ok("tạo", e.createRequest("W-1", exchangeBody(v, "DEFECT", up, ready)), http.StatusCreated)
	ref := c["ref"].(string)
	if c["status"] != "received" || !strings.HasPrefix(ref, "YC-") {
		t.Fatalf("tạo: %v", c)
	}
	id := e.requestID(ref)
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.request.create' AND entity_id = $1
		AND actor_type = 'service' AND after->'changed_fields' ? 'customer_note' AND after::text NOT LIKE '%size L%'`, id); n != 1 {
		t.Fatalf("audit tạo (không lời nhắn): %d", n)
	}
	var sigs []realtime.Signal
	for _, s := range e.bus.Signals() {
		if s.Topic == "order_request.changed" {
			sigs = append(sigs, s)
		}
	}
	if len(sigs) != 1 || sigs[0].Requires != "order.requests.read" || sigs[0].Entity.Type != "order_request" ||
		sigs[0].Entity.ID != id {
		t.Fatalf("báo hiệu: %+v", sigs)
	}
	expect(t, "lần hai vượt số", e.createRequest("W-1", `{"kind":"exchange","reason":"SIZE","message":"Đổi tiếp ạ",
		"conditionConfirmed":true,"lines":[{"variantId":"`+v.String()+`","qty":2,"reason":"FIT"}]}`),
		http.StatusUnprocessableEntity, "qty_exceeded")

	// Nhân viên: không quyền → 403; Bán hàng đọc được, danh sách tìm theo mã yêu cầu.
	path := "/api/v1/order-requests/" + id
	expect(t, "không quyền", serveAs(e.srv, e.keys, http.MethodGet, path, reqNobody, "", ""), http.StatusForbidden, "forbidden")
	list := e.ok("danh sách", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-requests?q="+strings.ToLower(ref),
		reqSale, "", ""), http.StatusOK)
	if items := list["items"].([]any); len(items) != 1 || items[0].(map[string]any)["ref"] != ref {
		t.Fatalf("danh sách: %v", list)
	}
	e.checkPhotos(path, up, ready)

	msg := path + "/messages"
	e.ok("trả lời", serveAs(e.srv, e.keys, http.MethodPost, msg, reqSale, "m1",
		`{"body":"Dạ shop nhận rồi ạ, chờ shop kiểm tra","visibleToCustomer":true}`), http.StatusCreated)
	e.ok("ghi chú", serveAs(e.srv, e.keys, http.MethodPost, msg, reqSale, "m2",
		`{"body":"Khách quen, ưu tiên xử lý","visibleToCustomer":false}`), http.StatusCreated)
	expect(t, "không quyền trả lời", serveAs(e.srv, e.keys, http.MethodPost, msg, reqNobody, "m3",
		`{"body":"Dạ shop nhận rồi","visibleToCustomer":true}`), http.StatusForbidden, "forbidden")
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.request.reply' AND entity_id = $1
		AND after->'changed_fields' ? 'body' AND after::text NOT LIKE '%shop nhận%' AND after::text NOT LIKE '%Khách quen%'`,
		id); n != 2 {
		t.Fatalf("audit trả lời (không nội dung): %d", n)
	}
	wl := e.ok("website đọc", e.web(http.MethodGet, "/api/v1/web/orders/W-1/requests", e.readKy, "", "", nil), http.StatusOK)
	items := wl["items"].([]any)
	it := items[0].(map[string]any)
	ms := it["messages"].([]any)
	if len(items) != 1 || wl["exchangeWindowDays"] != float64(7) || it["fileCount"] != float64(2) || len(ms) != 1 ||
		ms[0].(map[string]any)["body"] != "Dạ shop nhận rồi ạ, chờ shop kiểm tra" || ms[0].(map[string]any)["from"] != "staff" ||
		it["message"] != "Áo chật quá, em muốn đổi size L" {
		t.Fatalf("website thấy: %v", wl)
	}
	if strings.Contains(fmt.Sprint(wl), "Khách quen") || strings.Contains(fmt.Sprint(wl), "authorId") {
		t.Fatalf("website thấy ghi chú nội bộ: %v", wl)
	}
	expect(t, "website đọc đơn lạ", e.web(http.MethodGet, "/api/v1/web/orders/W-LA/requests", e.readKy, "", "", nil),
		http.StatusNotFound, "not_found")
}

// Trạng thái qua API: version cũ → 409; cạnh cấm → 422 invalid_transition; từ chối không lý do → 422 invalid_input;
// needs_info → khách trả lời qua website → in_review; khách rút yêu cầu khác còn received → cancelled (gửi lại 200),
// rút yêu cầu đang xử lý → 422 request_not_allowed; tenant khác → 404; audit đổi trạng thái chỉ tên trường.
func TestOrderRequestAPI_Status(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, nil)
	v := e.variant(e.shop.Seller, 250000, 5)
	e.webOrder("W-1", v, 3, true)
	ref := e.ok("tạo", e.createRequest("W-1", exchangeBody(v, "FIT")), http.StatusCreated)["ref"].(string)
	path := "/api/v1/order-requests/" + e.requestID(ref) + "/status"
	st := func(key, body string) *httptest.ResponseRecorder {
		return serveAs(e.srv, e.keys, http.MethodPost, path, reqSale, key, body)
	}
	expect(t, "version cũ", st("s1", `{"status":"in_review","version":2}`), http.StatusConflict, "version_conflict")
	expect(t, "cạnh cấm", st("s2", `{"status":"items_received","version":1}`), http.StatusUnprocessableEntity, "invalid_transition")
	expect(t, "từ chối không lý do", st("s3", `{"status":"rejected","version":1}`), http.StatusUnprocessableEntity, "invalid_input")
	r := e.ok("cần thêm thông tin", st("s4", `{"status":"needs_info","version":1,"message":"Chị gửi giúp em số đo nhé"}`),
		http.StatusOK)
	if r["status"] != "needs_info" || r["version"] != float64(2) || len(r["messages"].([]any)) != 1 {
		t.Fatalf("needs_info: %v", r)
	}
	reply := e.web(http.MethodPost, "/api/v1/web/orders/W-1/requests/"+ref+"/messages", e.writeKey, "w1",
		"application/json", strings.NewReader(`{"message":"Dạ em cao 1m60 nặng 50kg"}`))
	if b := e.ok("khách trả lời", reply, http.StatusOK); b["status"] != "in_review" {
		t.Fatalf("khách trả lời: %v", b)
	}
	e.ok("xác nhận đổi", st("s5", `{"status":"confirmed","version":3,"shipBack":{"address":"12 Lê Lợi, Quận 1, TP.HCM",
		"packageCode":"DOI-01","deadline":"2099-12-31"}}`), http.StatusOK)
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.request.status'
		AND after->>'status' = 'confirmed' AND after->'changed_fields' ? 'shipBack' AND after::text NOT LIKE '%Lê Lợi%'`); n != 1 {
		t.Fatalf("audit xác nhận: %d", n)
	}

	cancel := func(ref, key string) *httptest.ResponseRecorder {
		return e.web(http.MethodPost, "/api/v1/web/orders/W-1/requests/"+ref+"/cancel", e.writeKey, key, "", nil)
	}
	expect(t, "rút khi đang xử lý", cancel(ref, "c1"), http.StatusUnprocessableEntity, "request_not_allowed")
	ref2 := e.ok("tạo 2", e.createRequest("W-1", exchangeBody(v, "FIT")), http.StatusCreated)["ref"].(string)
	for _, key := range []string{"c2", "c3"} {
		if b := e.ok("rút", cancel(ref2, key), http.StatusOK); b["status"] != "cancelled" {
			t.Fatalf("rút: %v", b)
		}
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.request.customer_cancel'`); n != 1 {
		t.Fatalf("audit rút: %d", n)
	}

	b := e.staff.Tenant(t, "B")
	e.staff.Grant(t, b, e.staff.User(t, b, "ban-hang-b@elimaz.test", "active"), rolePerms(t, "sale"), "all", "")
	expect(t, "tenant khác", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-requests/"+e.requestID(ref),
		"ban-hang-b@elimaz.test", "", ""), http.StatusNotFound, "not_found")
}

func (e *reqAPIEnv) ok(name string, rec *httptest.ResponseRecorder, status int) map[string]any {
	e.t.Helper()
	return mustOK(e.t, name, rec, status)
}

// checkPhotos: ảnh đang xử lý up không có url, ảnh ready có link ký tới biến thể ở bucket riêng tư; up bị imager từ
// chối sau đó → rejected, không url.
func (e *reqAPIEnv) checkPhotos(path, up, ready string) {
	t := e.t
	t.Helper()
	got := e.ok("chi tiết", serveAs(e.srv, e.keys, http.MethodGet, path, reqSale, "", ""), http.StatusOK)
	fs := got["files"].([]any)
	f0, f1 := fs[0].(map[string]any), fs[1].(map[string]any)
	if len(fs) != 2 || f0["fileId"] != up || f0["status"] != "processing" || f0["url"] != nil ||
		f1["status"] != "ready" || !strings.Contains(fmt.Sprint(f1["url"]), "/private/priv/"+e.shop.Tenant.String()+"/"+ready+"/") ||
		got["customerNote"] != "Áo chật quá, em muốn đổi size L" {
		t.Fatalf("chi tiết: %v", got)
	}
	// Ảnh đang xử lý bị imager từ chối sau đó → rejected, không url.
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE platform.files SET status = 'rejected', reject_reason = 'not_image' WHERE id = $1`, up)
		return err //nolint:wrapcheck // test
	})
	got = e.ok("chi tiết sau từ chối", serveAs(e.srv, e.keys, http.MethodGet, path, reqSale, "", ""), http.StatusOK)
	if f := got["files"].([]any)[0].(map[string]any); f["status"] != "rejected" || f["url"] != nil {
		t.Fatalf("ảnh bị từ chối: %v", f)
	}
}
