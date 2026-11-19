package files_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/selftest"
)

const (
	kindPublic  = "file.process"
	kindPrivate = "file.process_private"
)

// TP01: ảnh tốt → tệp ready, 12 biến thể, getFile trả 12 đường dẫn công khai theo mã băm; ảnh gốc ở orig/<sha>.
func TestFileProcess_ReadyWithTwelveVariants(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("anh-ao-dai-mau-do")
	id := p.upload(p.a, "product_image", body)
	if n := p.jobs(kindPublic, id); n != 1 {
		t.Fatalf("muốn 1 lệnh file.process, có %d", n)
	}
	p.drain()
	if j := p.job(kindPublic, id); j.Status != "done" {
		t.Fatalf("lệnh: %+v", j)
	}
	sha := shaHex(body)
	r := p.fileRow(id)
	if r.Status != files.StatusReady || r.Detected != "image/jpeg" || r.Width == nil || *r.Width != 2000 ||
		r.Height == nil || *r.Height != 1500 || r.Preset == nil || *r.Preset != 1 || shaHexRaw(r.SHA) != sha {
		t.Fatalf("dòng tệp: %+v", r)
	}
	p.checkPublicVariants(id, sha)
	if got := readAll(t, p.mem, files.BucketOriginals, files.OriginalKey(sha)); !bytes.Equal(got, body) {
		t.Fatal("ảnh gốc khác ảnh tải lên")
	}
	if p.img.count() != 1 {
		t.Fatalf("imager gọi %d lần", p.img.count())
	}
	req := p.img.requests()[0]
	if b, key := bucketOf(t, req.SourceURL); b != files.BucketOriginals || key != files.OriginalKey(sha) {
		t.Fatalf("nguồn của imager: %s/%s", b, key)
	}
	if req.MaxPixels != files.MaxPixels || req.MaxBytes != 20<<20 || len(req.Outputs) != 12 {
		t.Fatalf("yêu cầu imager: %+v", req)
	}
	sigs := p.bus.signals() // processing (Task 409), rồi ready
	if len(sigs) != 2 {
		t.Fatalf("báo hiệu: %+v", sigs)
	}
	for _, s := range sigs {
		if s.Topic != "files.file" || s.Requires != "files.upload" || s.Entity.ID != id.String() || s.TenantID != p.a {
			t.Fatalf("báo hiệu: %+v", sigs)
		}
	}
	m := metricRows(p.reg)
	if m["image_process_ms|ok"] != 1 {
		t.Fatalf("chỉ số: %v", m)
	}
}

// checkPublicVariants: getFile trả 12 đường dẫn img/<sha>/v1/, mỗi dòng biến thể ở public và có trên kho.
func (p *procEnv) checkPublicVariants(id uuid.UUID, sha string) {
	p.t.Helper()
	info := p.info(p.a, id)
	if len(info.Variants) != 12 {
		p.t.Fatalf("muốn 12 biến thể, có %d", len(info.Variants))
	}
	prefix := publicBase + "/img/" + sha + "/v1/"
	for _, v := range info.Variants {
		if !strings.HasPrefix(v.URL, prefix) || v.Preset != 1 {
			p.t.Fatalf("đường dẫn %s không bắt đầu %s (preset %d)", v.URL, prefix, v.Preset)
		}
	}
	for _, v := range p.variants(id) {
		if v.Bucket != "public" || v.Size <= 0 {
			p.t.Fatalf("biến thể: %+v", v)
		}
		if _, err := p.mem.Head(p.t.Context(), files.BucketPublic, v.Key); err != nil {
			p.t.Fatalf("biến thể %s không có trên kho: %v", v.Key, err)
		}
	}
}

func shaHexRaw(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hex[c>>4], hex[c&15])
	}
	return string(out)
}

// Cùng ảnh tải lên lần hai (tệp khác) → ready mà imager không được gọi lần hai; biến thể trỏ cùng khóa. Tenant khác
// cùng ảnh: không thấy tệp của A, nhưng bản kê trên kho có sẵn nên cũng không gọi imager (Q4).
func TestFileProcess_DuplicateReusesVariants(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("anh-trung-nhau")
	first := p.upload(p.a, "product_image", body)
	p.drain()
	second := p.upload(p.a, "product_image", body)
	p.drain()
	if p.img.count() != 1 {
		t.Fatalf("imager gọi %d lần, muốn 1", p.img.count())
	}
	if r := p.fileRow(second); r.Status != files.StatusReady || r.Width == nil || *r.Width != 2000 {
		t.Fatalf("tệp thứ hai: %+v", r)
	}
	var res map[string]string
	if err := json.Unmarshal(p.jobResult(kindPublic, second), &res); err != nil || res["reuse"] != first.String() {
		t.Fatalf("kết quả lệnh thứ hai: %v %v", res, err)
	}
	keys := func(id uuid.UUID) []string {
		var ks []string
		for _, v := range p.variants(id) {
			ks = append(ks, v.Key)
		}
		return ks
	}
	if k1, k2 := keys(first), keys(second); len(k2) != 12 || !slices.Equal(k1, k2) {
		t.Fatalf("biến thể khác nhau: %v / %v", k1, k2)
	}
	other := p.upload(p.b, "product_image", body)
	p.drain()
	if p.img.count() != 1 {
		t.Fatalf("tenant B: imager gọi %d lần, muốn 1 (bản kê)", p.img.count())
	}
	if r := p.fileRow(other); r.Status != files.StatusReady || len(p.variants(other)) != 12 {
		t.Fatalf("tệp của B: %+v, %d biến thể", r, len(p.variants(other)))
	}
}

// rejected not_image → tệp rejected ngay, imager gọi 1 lần, không biến thể; lệnh dead (không thử lại).
func TestFileProcess_RejectedNotImage(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte(rejPrefix+"not_image\n<html>"))
	p.drain()
	p.makeDue()
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "not_image" {
		t.Fatalf("tệp: %+v", r)
	}
	if j := p.job(kindPublic, id); j.Status != "dead" || j.Attempts != 1 || !strings.Contains(j.LastError, "not_image") {
		t.Fatalf("lệnh: %+v", j)
	}
	if p.img.count() != 1 || len(p.variants(id)) != 0 {
		t.Fatalf("imager %d lần, %d biến thể", p.img.count(), len(p.variants(id)))
	}
	m := metricRows(p.reg)
	if m["image_rejected|not_image"] != 1 || m["image_process_ms|rejected"] != 1 {
		t.Fatalf("chỉ số: %v", m)
	}
	if n := len(p.bus.signals()); n != 2 {
		t.Fatalf("muốn 2 báo hiệu (processing, rejected), có %d", n)
	}
}

// Lỗi tạm của imager (Q1, việc (1) của Task 42): không từ chối tệp, lệnh về hàng chờ, không tính lượt (Task 409),
// tệp ở processing.
func TestFileProcess_TransientReasonsRetry(t *testing.T) {
	for _, reason := range []string{"upload_failed", "source_failed", "internal", "timeout"} {
		t.Run(reason, func(t *testing.T) {
			p := newProcEnv(t)
			id := p.upload(p.a, "product_image", []byte(rejPrefix+reason+"\n"))
			p.drain()
			p.assertWaiting(kindPublic, id)
		})
	}
	t.Run("busy", func(t *testing.T) {
		p := newProcEnv(t)
		files.SetProcessTimeout(p.pub, 500*time.Millisecond) // client chờ Retry-After tới hết hạn của lần chạy
		p.w = p.newWorker()
		id := p.upload(p.a, "product_image", []byte(busyPrefix))
		p.drain()
		if j := p.job(kindPublic, id); j.Status != "pending" || !strings.Contains(j.LastError, "busy") {
			t.Fatalf("lệnh: %+v", j)
		}
		var wait float64
		if err := p.staff.Owner.Pool.QueryRow(t.Context(), "SELECT extract(epoch FROM run_at - now()) FROM platform.jobs WHERE dedupe_key = $1",
			id.String()).Scan(&wait); err != nil || wait < 5 || wait > 8 {
			t.Fatalf("muốn hẹn lại theo Retry-After 7 s, còn %.1f s (%v)", wait, err)
		}
	})
}

// Nhiều ảnh hơn số việc imager nhận cùng lúc (worker chạy 8 việc, imager 2): client giữ đúng 2 việc gửi cùng lúc,
// không ảnh nào nhận 503 hay tốn thêm lượt thử, tất cả ready ở lần đầu (phát hiện của reviewer).
func TestFileProcess_ManyImagesNoBusyRetry(t *testing.T) {
	p := newProcEnv(t)
	p.img.delay = 300 * time.Millisecond
	var ids []uuid.UUID
	for i := range 8 {
		ids = append(ids, p.upload(p.a, "product_image", []byte{'n', 'h', 'i', 'e', 'u', byte('0' + i)}))
	}
	w := queue.NewWorker(p.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-nhieu", Poll: 50 * time.Millisecond, Concurrency: 8,
		Commands: commands.NewRunner(p.staff.App, []commands.Handler{p.pub, p.priv}),
	})
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Stop(5 * time.Second) })
	deadline := time.Now().Add(20 * time.Second)
	for _, id := range ids {
		for p.job(kindPublic, id).Status != "done" {
			if time.Now().After(deadline) {
				t.Fatalf("lệnh %s: %+v", id, p.job(kindPublic, id))
			}
			time.Sleep(100 * time.Millisecond)
		}
		if j := p.job(kindPublic, id); j.Attempts != 1 || p.fileRow(id).Status != files.StatusReady {
			t.Fatalf("tệp %s: lệnh %+v, tệp %+v", id, j, p.fileRow(id))
		}
	}
	p.img.mu.Lock()
	defer p.img.mu.Unlock()
	if p.img.busy != 0 || p.img.peak > fakeCapacity || p.img.calls != 8 {
		t.Fatalf("imager: %d lần 503, cao nhất %d việc cùng lúc, %d lần gọi", p.img.busy, p.img.peak, p.img.calls)
	}
}

// Treo: imager không trả lời → sau Timeout của lệnh not_sent (không tính lượt, Task 409), chạy lại; hết trần thời
// gian (3 s trong test) → dead, tệp rejected processing_failed. Trong lúc đó 20 lệnh selftest và 5 lệnh file.process
// khác xong; worker không khởi động lại.
func TestFileProcess_HangDoesNotBlockWorker(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = 3 * time.Second })
	files.SetProcessTimeout(p.pub, time.Second)
	w := queue.NewWorker(p.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-treo", Poll: 50 * time.Millisecond, Concurrency: 4,
		Commands: commands.NewRunner(p.staff.App, []commands.Handler{p.pub, p.priv, selftest.NewCommand(selftest.Loopback{})}),
	})
	hang := p.upload(p.a, "product_image", []byte(hangPrefix))
	var others []uuid.UUID
	for i := range 5 {
		others = append(others, p.upload(p.a, "product_image", []byte{'o', 'k', byte('0' + i)}))
	}
	var selftests []uuid.UUID
	for range 20 {
		var id uuid.UUID
		p.must(p.a, func(tx pgx.Tx) error {
			var err error
			id, _, err = p.q.Enqueue(t.Context(), tx, selftest.CommandKind(), queue.NewJob{TenantID: &p.a, DedupeKey: uuid.NewString()})
			return err //nolint:wrapcheck // test
		})
		selftests = append(selftests, id)
	}
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Stop(5 * time.Second) })
	deadline := time.Now().Add(40 * time.Second)
	for {
		p.makeDue() // lịch thử lại 30 s của timeout: đưa về hạn ngay
		if j := p.job(kindPublic, hang); j.Status == "dead" {
			if j.Attempts != 1 {
				t.Fatalf("lệnh treo: %+v", j)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lệnh treo chưa dead: %+v", p.job(kindPublic, hang))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if r := p.fileRow(hang); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("tệp treo: %+v", r)
	}
	for _, id := range others {
		if r := p.fileRow(id); r.Status != files.StatusReady {
			t.Fatalf("tệp %s: %+v", id, r)
		}
	}
	n := p.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done' AND id = ANY($2)",
		selftest.CommandKind().Name, selftests)
	if n != 20 {
		t.Fatalf("muốn 20 lệnh selftest xong, có %d", n)
	}
}

// Looker: lần trước ghi xong mà mất câu trả lời (worker chết) → in_doubt → tra lại đưa về hàng chờ, chạy lại đọc bản
// kê trên kho, xong mà không gọi imager lần hai (Q3).
func TestFileProcess_LookerAfterLostAnswer(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("mat-cau-tra-loi")
	id := p.upload(p.a, "product_image", body)
	job := p.queueJob(kindPublic, id)
	out, err := p.pub.Execute(t.Context(), job) // gửi xong, kết quả không được ghi
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute: %+v %v", out, err)
	}
	if _, err := p.staff.Owner.Pool.Exec(t.Context(),
		"UPDATE platform.jobs SET status = 'in_doubt', attempts = 1, finished_at = now() WHERE id = $1", job.ID); err != nil {
		t.Fatal(err)
	}
	look, ok := p.pub.(commands.Looker)
	if !ok {
		t.Fatal("file.process phải có Looker")
	}
	if st, err := look.Lookup(t.Context(), job); err != nil || st != commands.StatusNotSent {
		t.Fatalf("Lookup: %s %v", st, err)
	}
	if _, requeued, _, err := commands.ResolveInDoubt(t.Context(), p.staff.App, []commands.Handler{p.pub}, 10); err != nil || requeued != 1 {
		t.Fatalf("ResolveInDoubt: %d %v", requeued, err)
	}
	p.drain()
	if j := p.job(kindPublic, id); j.Status != "done" {
		t.Fatalf("lệnh: %+v", j)
	}
	if r := p.fileRow(id); r.Status != files.StatusReady || len(p.variants(id)) != 12 {
		t.Fatalf("tệp: %+v", r)
	}
	if p.img.count() != 1 {
		t.Fatalf("imager gọi %d lần, muốn 1", p.img.count())
	}
}

// Chạy lại sau khi lần trước ghi được một phần: chỉ xin imager các biến thể còn thiếu (ghi lại khóa đã có sẽ bị 412).
// Đủ 12 mà chưa có bản kê: một đầu ra thử ghi vào crm-tmp để lấy kết quả kiểm.
func TestFileProcess_SkipsExistingVariants(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("da-ghi-mot-nua")
	sha := shaHex(body)
	specs, _ := files.Preset(1)
	for _, s := range specs[:6] {
		putObj(t, p.mem, files.BucketPublic, files.VariantKey(sha, 1, s.Width, s.Format), "cu")
	}
	id := p.upload(p.a, "product_image", body)
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusReady || len(p.variants(id)) != 12 {
		t.Fatalf("tệp: %+v, %d biến thể", r, len(p.variants(id)))
	}
	if outs := p.img.requests()[0].Outputs; len(outs) != 6 {
		t.Fatalf("muốn xin 6 biến thể còn thiếu, xin %d", len(outs))
	}

	q := newProcEnv(t)
	body2 := []byte("du-bien-the-thieu-ban-ke")
	sha2 := shaHex(body2)
	for _, s := range specs {
		putObj(t, q.mem, files.BucketPublic, files.VariantKey(sha2, 1, s.Width, s.Format), "cu")
	}
	id2 := q.upload(q.a, "product_image", body2)
	q.drain()
	if r := q.fileRow(id2); r.Status != files.StatusReady || len(q.variants(id2)) != 12 {
		t.Fatalf("tệp 2: %+v", r)
	}
	outs := q.img.requests()[0].Outputs
	if len(outs) != 1 {
		t.Fatalf("muốn 1 đầu ra thử, có %d", len(outs))
	}
	if b, _ := bucketOf(t, outs[0].PutURL); b != files.BucketTmp {
		t.Fatalf("đầu ra thử phải vào crm-tmp, vào %s", b)
	}
}

// putObj ghi một biến thể có sẵn với content-type đúng định dạng của khóa (đuôi .avif → image/avif), như imager ghi.
func putObj(t *testing.T, s files.FileStore, b files.Bucket, key, body string) {
	t.Helper()
	putObjType(t, s, b, key, body, "image/"+key[strings.LastIndexByte(key, '.')+1:])
}

func putObjType(t *testing.T, s files.FileStore, b files.Bucket, key, body, contentType string) {
	t.Helper()
	if err := s.Put(t.Context(), b, key, strings.NewReader(body), int64(len(body)), contentType); err != nil {
		t.Fatal(err)
	}
}

// L1 của threat model M1 (Task 414): trước khi đặt ready, worker Head mọi biến thể và đòi content-type đúng định dạng
// đã ký (image/<định dạng>). Biến thể trên kho mang loại khác (imager bị chiếm ghi HTML, kho trả sai) → tệp rejected
// output_invalid, không có biến thể nào được công bố, không ghi bản kê. Cả hai đường: biến thể có sẵn trước khi gọi
// imager, và bản kê đã có (lần sau cùng ảnh) nhưng một biến thể bị thay.
func TestFileProcess_RejectsVariantWithWrongType(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("bien-the-html")
	sha := shaHex(body)
	specs, _ := files.Preset(1)
	bad := files.VariantKey(sha, 1, specs[0].Width, specs[0].Format)
	putObjType(t, p.mem, files.BucketPublic, bad, "<html><script>x</script></html>", "text/html")
	id := p.upload(p.a, "product_image", body)
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "output_invalid" {
		t.Fatalf("tệp: %+v", r)
	}
	if j := p.job(kindPublic, id); j.Status != "dead" || !strings.Contains(j.LastError, "output_invalid") {
		t.Fatalf("lệnh: %+v", j)
	}
	if n := len(p.variants(id)); n != 0 {
		t.Fatalf("không được công bố biến thể nào, có %d", n)
	}
	if _, err := p.mem.Head(t.Context(), files.BucketPublic, "img/"+sha+"/v1/manifest.json"); err == nil {
		t.Fatal("không được ghi bản kê khi biến thể sai loại")
	}

	// Đường bản kê: ảnh khác xử lý xong (có bản kê), rồi một biến thể bị thay bằng loại khác; tenant B tải cùng ảnh
	// (không trùng trong tenant nên đi đường bản kê) → bản kê không được tin, tệp mới rejected.
	good := []byte("anh-tot-roi-bi-thay")
	sha2 := shaHex(good)
	first := p.upload(p.a, "product_image", good)
	p.drain()
	if r := p.fileRow(first); r.Status != files.StatusReady {
		t.Fatalf("lần đầu: %+v", r)
	}
	key := files.VariantKey(sha2, 1, specs[1].Width, specs[1].Format)
	if err := p.mem.Delete(t.Context(), files.BucketPublic, key); err != nil {
		t.Fatal(err)
	}
	putObjType(t, p.mem, files.BucketPublic, key, "<html>", "text/html")
	calls := p.img.count()
	second := p.upload(p.b, "product_image", good)
	p.drain()
	if r := p.fileRow(second); r.Status != files.StatusRejected || r.Reason != "output_invalid" {
		t.Fatalf("lần hai: %+v", r)
	}
	if p.img.count() != calls {
		t.Fatalf("bản kê đủ biến thể: không gọi imager, đã gọi thêm %d lần", p.img.count()-calls)
	}
}

// Tenant isolation: lệnh của tenant A trỏ tệp của B → không đọc được, từ chối; tệp của B không đổi.
func TestFileProcess_TenantIsolation(t *testing.T) {
	p := newProcEnv(t)
	idB := p.upload(p.b, "product_image", []byte("anh-cua-b"))
	var jobID uuid.UUID
	p.must(p.a, func(tx pgx.Tx) error {
		var err error
		jobID, _, err = p.q.Enqueue(t.Context(), tx, files.ProcessKind(), queue.NewJob{
			TenantID: &p.a, Payload: map[string]string{"fileId": idB.String()}, DedupeKey: "a-tro-b",
		})
		return err //nolint:wrapcheck // test
	})
	ctx := context.WithoutCancel(t.Context())
	p.drain()
	var st, last string
	if err := p.staff.Owner.Pool.QueryRow(ctx, "SELECT status, coalesce(last_error, '') FROM platform.jobs WHERE id = $1", jobID).
		Scan(&st, &last); err != nil {
		t.Fatal(err)
	}
	if st != "dead" || !strings.Contains(last, "not_found") {
		t.Fatalf("lệnh A trỏ B: %s %s", st, last)
	}
	if p.img.count() != 1 { // chỉ lệnh đúng của B
		t.Fatalf("imager gọi %d lần", p.img.count())
	}
	if r := p.fileRow(idB); r.Status != files.StatusReady {
		t.Fatalf("tệp của B: %+v", r)
	}
}

// PublicVariants: một lần đọc cho nhiều tệp; tệp chưa ready, tệp private, tệp tenant khác không có trong kết quả.
func TestPublicVariants(t *testing.T) {
	p := newProcEnv(t)
	ready := p.upload(p.a, "product_image", []byte("anh-cong-khai"))
	pending := p.upload(p.a, "product_image", []byte(hangPrefix))
	priv := p.registerPrivate(p.a, []byte("anh-rieng"))
	ofB := p.upload(p.b, "product_image", []byte("anh-b"))
	files.SetProcessTimeout(p.pub, 500*time.Millisecond)
	p.w = p.newWorker() // Worker đọc hạn của loại lúc dựng
	p.drain()
	var got map[uuid.UUID][]files.VariantURL
	p.must(p.a, func(tx pgx.Tx) error {
		var err error
		got, err = files.PublicVariants(t.Context(), tx, []uuid.UUID{ready, pending, priv, ofB, uuid.New()}, publicBase)
		return err //nolint:wrapcheck // test
	})
	if len(got) != 1 || len(got[ready]) != 12 {
		t.Fatalf("PublicVariants: %v", got)
	}
	for _, v := range got[ready] {
		if !strings.HasPrefix(v.URL, publicBase+"/img/") || v.Preset != 1 {
			t.Fatalf("biến thể: %+v", v)
		}
	}
	p.must(p.a, func(tx pgx.Tx) error {
		var err error
		got, err = files.PublicVariants(t.Context(), tx, nil, publicBase)
		return err //nolint:wrapcheck // test
	})
	if len(got) != 0 {
		t.Fatalf("rỗng: %v", got)
	}
}

// jobResult đọc result của lệnh kind cho tệp id.
func (p *procEnv) jobResult(kind string, id uuid.UUID) []byte {
	p.t.Helper()
	var res []byte
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(), "SELECT result FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2",
		kind, id.String()).Scan(&res); err != nil {
		p.t.Fatal(err)
	}
	return res
}

// queueJob đọc lệnh kind của tệp id thành queue.Job (như Worker giao cho Runner).
func (p *procEnv) queueJob(kind string, id uuid.UUID) queue.Job {
	p.t.Helper()
	j := queue.Job{Kind: kind}
	var tenant uuid.UUID
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(),
		"SELECT id, tenant_id, payload, created_at FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2", kind, id.String()).
		Scan(&j.ID, &tenant, &j.Payload, &j.CreatedAt); err != nil {
		p.t.Fatal(err)
	}
	j.TenantID, j.Attempts = &tenant, 1
	return j
}
