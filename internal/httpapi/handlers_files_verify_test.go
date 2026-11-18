package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const filesAdmin = "kho@elimaz.test" // tenant A: files.upload, platform.files.manage

// verifyAPIEnv: catalogAPIEnv có kho MemStore, một worker chạy file.process (imager giả) và file.verify.
type verifyAPIEnv struct {
	*catalogAPIEnv
	w *queue.Worker
	n int
}

func newVerifyAPIEnv(t *testing.T) *verifyAPIEnv {
	t.Helper()
	c := newCatalogAPIEnv(t)
	c.useImageServer()
	c.staff.Grant(t, c.tenant, c.staff.User(t, c.tenant, filesAdmin, "active"),
		[]string{"files.upload", "platform.files.manage"}, "all", "")
	o := files.ProcessorOptions{DB: c.staff.App, Store: c.mem, Imager: testkit.StubImager{}, PresetVersion: 1}
	w := queue.NewWorker(c.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(c.staff.App, []commands.Handler{files.NewProcessHandler(o), files.NewVerifyHandler(o)}),
	})
	return &verifyAPIEnv{catalogAPIEnv: c, w: w}
}

// drain chạy worker tới khi không còn việc đến hạn.
func (e *verifyAPIEnv) drain() {
	e.t.Helper()
	for range 30 {
		n, err := e.w.RunOnce(e.t.Context(), 10)
		if err != nil {
			e.t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
	e.t.Fatal("worker còn việc sau 30 lượt")
}

func (e *verifyAPIEnv) key() string {
	e.n++
	return fmt.Sprintf("v%d", e.n)
}

// upload tải body lên như màn hình: createUpload → PUT → complete; trả mã tệp.
func (e *verifyAPIEnv) upload(body []byte) uuid.UUID {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/files/uploads", filesAdmin, e.key(),
		fmt.Sprintf(`{"purpose":"product_image","fileName":"anh.jpg","contentType":"image/jpeg","sizeBytes":%d}`, len(body)))
	var u uploadResp
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &u) != nil {
		e.t.Fatalf("createUpload: %d %s", rec.Code, rec.Body)
	}
	req, err := http.NewRequestWithContext(e.t.Context(), http.MethodPut, u.PutURL, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	if code, _, _ := testkit.Do(e.t, req); code != http.StatusOK {
		e.t.Fatalf("PUT: %d", code)
	}
	if rec := e.do(http.MethodPost, "/api/v1/files/"+u.FileID.String()+"/complete", filesAdmin, e.key(), ""); rec.Code != http.StatusOK {
		e.t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	return u.FileID
}

func shaOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type lostPage struct {
	Items []struct {
		FileID      uuid.UUID `json:"fileId"`
		Purpose     string    `json:"purpose"`
		ProductID   string    `json:"productId"`
		ProductCode string    `json:"productCode"`
		LostAt      time.Time `json:"lostAt"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
}

func (e *verifyAPIEnv) lost(query string) lostPage {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/platform/files/lost"+query, filesAdmin, "", "")
	var p lostPage
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
		e.t.Fatalf("listLostFiles%s: %d %s", query, rec.Code, rec.Body)
	}
	return p
}

// imageStatuses đọc trạng thái từng ảnh của mẫu id qua getProduct (theo mã tệp không có trong API: theo thứ tự gắn).
func (e *verifyAPIEnv) imageStatuses(id string) []string {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogViewer, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("getProduct: %d %s", rec.Code, rec.Body)
	}
	var out []string
	for _, img := range listOf(e.t, decodeJSON(e.t, rec), "images") {
		m := img.(map[string]any) //nolint:forcetypeassert // test
		st, _ := m["status"].(string)
		if (st == "lost") != (m["variants"] == nil) {
			e.t.Fatalf("ảnh %v: lost thì không variants, ready thì có", m)
		}
		out = append(out, st)
	}
	return out
}

// TestVerify_MarksLost (TP12): 5 ảnh ready gắn vào mẫu DAM01, xóa đối tượng của 2 (ảnh gốc của một, một biến thể của
// một) → verifyFiles 202 {jobId}, sau khi worker chạy: đúng 2 lost; listLostFiles có đúng 2 dòng kèm productId,
// productCode, phân trang theo con trỏ; getProduct 200 với ảnh lost (không variants); tải lại đúng ảnh đã mất → ảnh
// đó về ready. Quyền: chỉ platform.files.manage; tệp lost của tenant khác không lộ.
func TestVerify_MarksLost(t *testing.T) {
	e := newVerifyAPIEnv(t)
	bodies := make([][]byte, 5)
	ids := make([]uuid.UUID, 5)
	for i := range bodies {
		bodies[i] = fmt.Appendf(nil, "anh-dam01-%d-%s", i, uuid.NewString())
		ids[i] = e.upload(bodies[i])
	}
	e.drain()
	p := e.createDam01()
	pid := p["id"].(string) //nolint:forcetypeassert // test
	for i, f := range ids {
		p = e.attach(pid, p["version"], f, fmt.Sprintf("img%d", i), "")
	}
	b := e.staff.Tenant(t, "B")
	e.staff.Exec(t, b, func(tx pgx.Tx) error {
		filestest.MarkLost(t, tx, filestest.Image(t, tx, b, "product_image"))
		return nil
	})

	specs, _ := files.Preset(1)
	if err := e.mem.Delete(t.Context(), files.BucketOriginals, files.OriginalKey(shaOf(bodies[1]))); err != nil {
		t.Fatal(err)
	}
	if err := e.mem.Delete(t.Context(), files.BucketPublic, files.VariantKey(shaOf(bodies[3]), 1, specs[0].Width, specs[0].Format)); err != nil {
		t.Fatal(err)
	}

	e.checkForbidden()
	e.verifyOnce()
	e.drain()

	e.checkLostList(pid, ids[1], ids[3])
	if st := e.imageStatuses(pid); !slices.Equal(st, []string{"ready", "lost", "ready", "lost", "ready"}) {
		t.Fatalf("ảnh của mẫu sau đối chiếu: %v", st)
	}

	// Tải lại đúng ảnh thứ hai (cùng nội dung) → tệp mất về ready, mẫu hiện lại ảnh; còn đúng một tệp mất.
	e.upload(bodies[1])
	e.drain()
	if st := e.imageStatuses(pid); !slices.Equal(st, []string{"ready", "ready", "ready", "lost", "ready"}) {
		t.Fatalf("ảnh của mẫu sau khi tải lại: %v", st)
	}
	if page := e.lost(""); len(page.Items) != 1 || page.Items[0].FileID != ids[3] {
		t.Fatalf("sau khi tải lại còn tệp mất: %+v", page)
	}
}

// checkForbidden: người không có platform.files.manage nhận 403 ở cả hai đường.
func (e *verifyAPIEnv) checkForbidden() {
	e.t.Helper()
	for _, who := range []string{catalogViewer, catalogManager} {
		if rec := e.do(http.MethodPost, "/api/v1/platform/files/verify", who, e.key(), ""); rec.Code != http.StatusForbidden {
			e.t.Fatalf("%s không có platform.files.manage: verify muốn 403, nhận %d", who, rec.Code)
		}
		if rec := e.do(http.MethodGet, "/api/v1/platform/files/lost", who, "", ""); rec.Code != http.StatusForbidden {
			e.t.Fatalf("%s không có platform.files.manage: lost muốn 403, nhận %d", who, rec.Code)
		}
	}
}

// verifyOnce: verifyFiles 202 {jobId} (đúng mã lệnh file.verify), gửi lại cùng khóa trả cùng kết quả, một dòng audit.
func (e *verifyAPIEnv) verifyOnce() {
	e.t.Helper()
	t := e.t
	rec := e.do(http.MethodPost, "/api/v1/platform/files/verify", filesAdmin, "verify-1", "")
	var acc struct {
		JobID uuid.UUID `json:"jobId"`
	}
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &acc) != nil ||
		e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE id = $1 AND kind = 'file.verify'", acc.JobID) != 1 {
		t.Fatalf("verifyFiles: %d %s", rec.Code, rec.Body)
	}
	if again := e.do(http.MethodPost, "/api/v1/platform/files/verify", filesAdmin, "verify-1", ""); again.Body.String() != rec.Body.String() {
		t.Fatalf("gửi lại cùng khóa phải trả cùng kết quả: %s / %s", again.Body, rec.Body)
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = 'platform.files.verify'"); n != 1 {
		t.Fatalf("có %d dòng audit platform.files.verify", n)
	}
}

// checkLostList: listLostFiles có đúng hai tệp a, b gắn mẫu pid (DAM01); phân trang theo con trỏ; con trỏ hỏng → 400.
func (e *verifyAPIEnv) checkLostList(pid string, a, b uuid.UUID) {
	e.t.Helper()
	t := e.t
	page := e.lost("")
	got := []uuid.UUID{}
	for _, it := range page.Items {
		got = append(got, it.FileID)
		if it.ProductID != pid || it.ProductCode != "DAM01" || it.Purpose != "product_image" || it.LostAt.IsZero() {
			t.Errorf("dòng tệp mất sai: %+v", it)
		}
	}
	if len(got) != 2 || !slices.Contains(got, a) || !slices.Contains(got, b) || page.NextCursor != "" {
		t.Fatalf("listLostFiles: muốn đúng 2 tệp %s, %s; có %+v", a, b, page)
	}
	first := e.lost("?limit=1")
	if len(first.Items) != 1 || first.NextCursor == "" || first.Items[0].FileID != page.Items[0].FileID {
		t.Fatalf("trang 1: %+v", first)
	}
	second := e.lost("?limit=1&cursor=" + url.QueryEscape(first.NextCursor))
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].FileID != page.Items[1].FileID {
		t.Fatalf("trang 2: %+v", second)
	}
	if rec := e.do(http.MethodGet, "/api/v1/platform/files/lost?cursor=hong", filesAdmin, "", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("con trỏ hỏng: muốn 400, nhận %d", rec.Code)
	}
}
