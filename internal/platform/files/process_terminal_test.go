package files_test

// Ca thêm ở Task 403 (mutation testing vùng cao M1, docs/mutation.md): OnTerminal, OnDone và Execute của lệnh xử lý
// ảnh gọi thẳng (không qua worker), để mỗi nhánh của điều kiện chặn đầu hàm có một ca riêng.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

func fileJob(tenant *uuid.UUID, id uuid.UUID) queue.Job {
	return queue.Job{TenantID: tenant, Payload: []byte(`{"fileId":"` + id.String() + `"}`)}
}

// setStatus đổi trạng thái tệp bằng chủ bảng (dựng ca).
func (p *procEnv) setStatus(id uuid.UUID, status string) {
	p.t.Helper()
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(p.t.Context(), "UPDATE platform.files SET status = $2, reject_reason = NULL WHERE id = $1", id, status)
		return tag.RowsAffected() == 1, err //nolint:wrapcheck // test
	})
}

// resetFile (chủ bảng) xóa mọi biến thể của tệp id, đặt trạng thái status và đánh dấu xóa nếu deleted — dựng tệp như
// lúc lệnh còn đang chạy để gọi thẳng OnDone.
func (p *procEnv) resetFile(id uuid.UUID, status string, deleted bool) {
	p.t.Helper()
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(p.t.Context(), "DELETE FROM platform.file_variants WHERE file_id = $1", id)
		if err != nil {
			return false, err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(p.t.Context(), `UPDATE platform.files SET status = $2, reject_reason = NULL,
			deleted_at = CASE WHEN $3 THEN now() END WHERE id = $1`, id, status, deleted)
		return tag.RowsAffected() > 0, err //nolint:wrapcheck // test
	})
}

// OnTerminal: chỉ dead và expired làm tệp ảnh còn chờ thành rejected processing_failed; trạng thái khác, lệnh thiếu
// tenant, tệp không phải ảnh hay của chế độ khác (riêng tư khi lệnh công khai) không bị đụng tới.
func TestProcessor_OnTerminal(t *testing.T) {
	p := newProcEnv(t)
	pub := p.pub.(*files.Processor) //nolint:forcetypeassert // test: NewProcessHandler trả *Processor
	terminal := func(j queue.Job, status string) {
		t.Helper()
		p.must(p.a, func(tx pgx.Tx) error { return pub.OnTerminal(t.Context(), j, status, tx) })
	}
	for _, status := range []string{queue.StatusDead, queue.StatusExpired} {
		id := p.upload(p.a, "product_image", []byte("anh-cho-"+status))
		terminal(fileJob(&p.a, id), status)
		if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
			t.Errorf("%s: tệp %+v, mong rejected processing_failed", status, r)
		}
	}
	untouched := func(name string, id uuid.UUID, want string) {
		t.Helper()
		if r := p.fileRow(id); r.Status != want {
			t.Errorf("%s: tệp %+v, mong giữ %s", name, r, want)
		}
	}
	waiting := p.upload(p.a, "product_image", []byte("anh-cho-khac"))
	terminal(fileJob(&p.a, waiting), queue.StatusInDoubt)
	untouched("in_doubt", waiting, files.StatusUploaded)
	terminal(fileJob(nil, waiting), queue.StatusDead)
	untouched("thiếu tenant", waiting, files.StatusUploaded)
	private := p.registerPrivate(p.a, []byte("anh-rieng-cho"))
	before := p.fileRow(private).Status
	terminal(fileJob(&p.a, private), queue.StatusDead)
	untouched("ảnh riêng tư, lệnh công khai", private, before)
	sheet, put := p.mustCreate(p.mem, files.UploadInput{
		TenantID: p.a, Purpose: "catalog_import", FileName: "a.xlsx", ContentType: sheetType, SizeBytes: 5,
	})
	if code := putVia(t, put, []byte("12345")); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	p.setStatus(sheet.ID, files.StatusUploaded)
	terminal(fileJob(&p.a, sheet.ID), queue.StatusDead)
	untouched("tệp không phải ảnh", sheet.ID, files.StatusUploaded)
}

// OnDone với lệnh thiếu tenant: không ghi gì, kể cả khi kết quả hợp lệ và tệp đang processing.
func TestProcessor_OnDoneWithoutTenantIgnored(t *testing.T) {
	p := newProcEnv(t)
	pub := p.pub.(*files.Processor) //nolint:forcetypeassert // test
	id := p.upload(p.a, "product_image", []byte("anh-xong-thieu-tenant"))
	p.drain()
	result := p.jobResult("file.process", id)
	p.resetFile(id, files.StatusProcessing, false)
	p.must(p.a, func(tx pgx.Tx) error { return pub.OnDone(t.Context(), fileJob(nil, id), result, tx) })
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("tệp %+v, mong giữ processing", r)
	}
}

// Execute xử lý xong: kết quả done đi thẳng ra, không qua nhánh chờ thử lại (không giữ lượt, không hẹn giờ).
func TestProcessor_ExecuteDoneSkipsRetry(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = 1 })
	id := p.upload(p.a, "product_image", []byte("anh-xong-thang"))
	out, err := p.pub.Execute(t.Context(), p.queueJob("file.process", id))
	if err != nil || out.Status != commands.StatusDone || out.KeepAttempt || out.RetryAfter != 0 {
		t.Fatalf("kết quả %+v %v", out, err)
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("tệp %+v, mong processing (chờ OnDone)", r)
	}
}

// Trần thử lại tính từ lúc ghi lệnh khi mốc processing của tệp cũ hơn lệnh (tệp kẹt processing từ lần trước): lỗi
// hạ tầng lần này vẫn giữ lượt, không rejected processing_failed ngay.
func TestProcessor_RetryCapFromJobWhenProcessingOlder(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Hour })
	id := p.upload(p.a, "product_image", []byte(busyPrefix))
	p.setStatus(id, files.StatusProcessing)
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(t.Context(), "UPDATE platform.files SET updated_at = now() - interval '2 hours' WHERE id = $1", id)
		return tag.RowsAffected() == 1, err //nolint:wrapcheck // test
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second) // như hạn của worker: imager bận thì thôi chờ
	defer cancel()
	out, err := p.pub.Execute(ctx, p.queueJob("file.process", id))
	if err != nil || out.Status != commands.StatusNotSent || !out.KeepAttempt {
		t.Fatalf("kết quả %+v %v, mong not_sent giữ lượt", out, err)
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("tệp %+v, mong processing", r)
	}
}

// OnDone với kết quả có mã băm sai dạng (thừa một ký tự hex) → lỗi, tệp không ready.
func TestProcessor_OnDoneBadSHARejected(t *testing.T) {
	p := newProcEnv(t)
	pub := p.pub.(*files.Processor) //nolint:forcetypeassert // test
	id := p.upload(p.a, "product_image", []byte("anh-ma-bam-sai"))
	p.drain()
	var res map[string]any
	if err := json.Unmarshal(p.jobResult("file.process", id), &res); err != nil {
		t.Fatal(err)
	}
	res["sha256"] = res["sha256"].(string) + "0" //nolint:forcetypeassert // test: đúng dạng processResult
	bad, _ := json.Marshal(res)
	p.resetFile(id, files.StatusProcessing, false)
	if err := p.inTenant(p.a, func(tx pgx.Tx) error { return pub.OnDone(t.Context(), fileJob(&p.a, id), bad, tx) }); err == nil {
		t.Fatal("mã băm 65 ký tự phải lỗi")
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("tệp %+v, mong giữ processing", r)
	}
}

// Imager quá giờ → not_sent giữ lượt, chạy lại sau ít nhất 30 s (không dồn tải lên imager đang chậm).
func TestProcessor_TimeoutRetryAtLeast30s(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte(hangPrefix))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	out, err := p.pub.Execute(ctx, p.queueJob("file.process", id))
	if err != nil || out.Status != commands.StatusNotSent || out.RetryAfter < 30*time.Second {
		t.Fatalf("kết quả %+v %v, mong not_sent chờ ≥ 30s", out, err)
	}
}

// file.verify: payload thiếu runId, hỏng, hay lệnh thiếu tenant → rejected bad_payload.
func TestVerifier_BadPayload(t *testing.T) {
	p := newProcEnv(t)
	for name, j := range map[string]queue.Job{
		"thiếu runId":  {TenantID: &p.a, Payload: []byte(`{}`)},
		"hỏng":         {TenantID: &p.a, Payload: []byte(`{`)},
		"thiếu tenant": {Payload: []byte(`{"runId":"` + uuid.NewString() + `"}`)},
	} {
		out, err := p.ver.Execute(t.Context(), j)
		if err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
			t.Errorf("%s: %+v %v", name, out, err)
		}
	}
}

// OnDone chạy lại cho tệp đã ready (lệnh khác đã xong) hay tệp đã xóa → không làm gì, không lỗi.
func TestProcessor_OnDoneSkipsReadyAndDeleted(t *testing.T) {
	p := newProcEnv(t)
	pub := p.pub.(*files.Processor) //nolint:forcetypeassert // test
	id := p.upload(p.a, "product_image", []byte("anh-xong-roi"))
	p.drain()
	result := p.jobResult("file.process", id)
	onDone := func() error {
		return p.inTenant(p.a, func(tx pgx.Tx) error { return pub.OnDone(t.Context(), fileJob(&p.a, id), result, tx) })
	}
	if err := onDone(); err != nil {
		t.Fatalf("tệp đã ready: %v", err)
	}
	p.resetFile(id, files.StatusProcessing, true)
	if err := onDone(); err != nil {
		t.Fatalf("tệp đã xóa: %v", err)
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("tệp đã xóa: %+v, mong giữ processing", r)
	}
}

// file.regenerate cho tệp đã có bộ kích thước đó, hay tệp đã xóa → done "skipped", không gọi imager.
func TestRegenerate_SkipsWhenNotNeeded(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte("anh-da-co-v1"))
	p.drain()
	calls := p.img.count()
	run := func() commands.Outcome {
		t.Helper()
		out, err := p.regen.Execute(t.Context(), queue.Job{
			TenantID: &p.a,
			Payload:  []byte(`{"fileId":"` + id.String() + `","preset":1}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := run(); out.Status != commands.StatusDone || !strings.Contains(string(out.Result), "not_needed") {
		t.Errorf("đã có v1: %+v", out)
	}
	p.resetFile(id, files.StatusReady, true)
	if out := run(); out.Status != commands.StatusDone || !strings.Contains(string(out.Result), "not_needed") {
		t.Errorf("tệp đã xóa: %+v", out)
	}
	if p.img.count() != calls {
		t.Errorf("imager bị gọi %d lần", p.img.count()-calls)
	}
}

// OnDone của file.regenerate chỉ chèn biến thể cho tệp còn ready, chưa xóa, cùng mã băm.
func TestRegenerate_OnDoneOnlyReadySameSHA(t *testing.T) {
	p := newProcEnv(t)
	regen := p.regen.(*files.Regenerator) //nolint:forcetypeassert // test: NewRegenerateHandler trả *Regenerator
	body := []byte("anh-tao-lai-on-done")
	sha := shaHex(body)
	for name, c := range map[string]struct {
		status  string
		deleted bool
		sha     string
	}{
		"đã xóa":      {files.StatusReady, true, sha},
		"chưa ready":  {files.StatusProcessing, false, sha},
		"mã băm khác": {files.StatusReady, false, strings.Repeat("0", 64)},
		"đúng (chèn)": {files.StatusReady, false, sha},
	} {
		id := p.upload(p.a, "product_image", body)
		p.drain()
		p.resetFile(id, c.status, c.deleted)
		result := []byte(`{"sha256":"` + c.sha + `","preset":1,"variants":[{"width":320,"format":"jpeg","key":"k/` + id.String() + `","size":1}]}`)
		j := queue.Job{TenantID: &p.a, Payload: []byte(`{"fileId":"` + id.String() + `","preset":1}`)}
		p.must(p.a, func(tx pgx.Tx) error { return regen.OnDone(t.Context(), j, result, tx) })
		want := 0
		if name == "đúng (chèn)" {
			want = 1
		}
		if got := len(p.variants(id)); got != want {
			t.Errorf("%s: %d biến thể, mong %d", name, got, want)
		}
	}
}

// file.regenerate: payload thiếu fileId, hỏng, thiếu tenant → bad_payload; tệp không có → not_found. Kind giữ hạn 30 s
// khi không đặt riêng; đặt riêng thì dùng hạn đó.
func TestRegenerate_ExecuteGuards(t *testing.T) {
	p := newProcEnv(t)
	for name, j := range map[string]queue.Job{
		"thiếu fileId": {TenantID: &p.a, Payload: []byte(`{"preset":1}`)},
		"hỏng":         {TenantID: &p.a, Payload: []byte(`{`)},
		"thiếu tenant": {Payload: []byte(`{"fileId":"` + uuid.NewString() + `","preset":1}`)},
	} {
		if out, err := p.regen.Execute(t.Context(), j); err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
			t.Errorf("%s: %+v %v", name, out, err)
		}
	}
	out, err := p.regen.Execute(t.Context(), queue.Job{TenantID: &p.a, Payload: []byte(`{"fileId":"` + uuid.NewString() + `","preset":1}`)})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "not_found" {
		t.Errorf("tệp không có: %+v %v", out, err)
	}
	if got := p.regen.Kind().Policy.Timeout; got != 30*time.Second {
		t.Errorf("hạn mặc định: %v, mong 30s", got)
	}
	files.SetRegenerateTimeout(p.regen.(*files.Regenerator), time.Second) //nolint:forcetypeassert // test
	if got := p.regen.Kind().Policy.Timeout; got != time.Second {
		t.Errorf("hạn riêng: %v, mong 1s", got)
	}
}

// OnDone của file.regenerate bỏ qua: lệnh thiếu tenant, kết quả rỗng, kết quả "skipped", bản trong kết quả khác bản
// của lệnh.
func TestRegenerate_OnDoneIgnoresOddJobs(t *testing.T) {
	p := newProcEnv(t)
	regen := p.regen.(*files.Regenerator) //nolint:forcetypeassert // test
	body := []byte("anh-tao-lai-bo-qua")
	id := p.upload(p.a, "product_image", body)
	p.drain()
	p.resetFile(id, files.StatusReady, false)
	good := `{"sha256":"` + shaHex(body) + `","preset":1,"variants":[{"width":320,"format":"jpeg","key":"k/x","size":1}]}`
	payload := []byte(`{"fileId":"` + id.String() + `","preset":1}`)
	for name, c := range map[string]struct {
		tenant  *uuid.UUID
		payload []byte
		result  string
	}{
		"thiếu tenant": {nil, payload, good},
		"kết quả rỗng": {&p.a, payload, ""},
		"skipped":      {&p.a, payload, `{"skipped":"not_needed","sha256":"` + shaHex(body) + `","preset":1}`},
		"khác bản":     {&p.a, []byte(`{"fileId":"` + id.String() + `","preset":2}`), good},
	} {
		j := queue.Job{TenantID: c.tenant, Payload: c.payload}
		p.must(p.a, func(tx pgx.Tx) error { return regen.OnDone(t.Context(), j, []byte(c.result), tx) })
		if n := len(p.variants(id)); n != 0 {
			t.Fatalf("%s: chèn %d biến thể", name, n)
		}
	}
}
