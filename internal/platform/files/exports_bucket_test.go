package files_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/files"
)

// Bucket tệp xuất crm-exports (Task 515; kế hoạch M6 G5 chốt (a)): không giữ phiên bản, không chép kho ngoài, tự xóa
// theo tiền tố của khóa (reports/ 7 ngày, customers/ 1 ngày, docker/seaweedfs/bootstrap.sh).

// testExportsPurpose: mục đích thử ở bucket exports với tiền tố prefix (như report_export, customer_export của M6).
func testExportsPurpose(key, prefix string) files.Purpose {
	return files.Purpose{Key: key, Bucket: files.BucketExports, AllowedTypes: []string{"text/csv"}, MaxBytes: mib, KeyPrefix: prefix}
}

// registerExportsPurposes đăng ký hai mục đích thử (reports/, customers/) cùng dòng của bảng.
func (e *env) registerExportsPurposes() {
	e.t.Helper()
	for k, prefix := range map[string]string{"test_exp_reports": files.ExportReportsPrefix, "test_exp_customers": files.ExportCustomersPrefix} {
		files.RegisterPurpose(testExportsPurpose(k, prefix))
		if err := e.addPurposeRow(k, "exports", "standard"); err != nil {
			e.t.Fatal(err)
		}
	}
}

// Ghi từ máy chủ vào bucket exports: Put rồi Register với khóa <tiền tố của mục đích><tenant>/…, đọc lại đúng nội
// dung, ký được đường dẫn tải; Put lặp lại cùng khóa không ghi đè. Chạy trên MemStore và SeaweedFS dev thật (bucket do
// bootstrap.sh dựng, quyền crm-app ở s3.json).
func TestRegister_ExportsBucket(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.registerExportsPurposes()
			for purpose, prefix := range map[string]string{"test_exp_reports": files.ExportReportsPrefix, "test_exp_customers": files.ExportCustomersPrefix} {
				e.checkExportRoundTrip(store, purpose, prefix)
			}
		})
	}
}

// checkExportRoundTrip: Put + Register một tệp của mục đích purpose (khóa tiền tố prefix), Put lặp lại bị từ chối,
// đọc lại đúng byte, ký được đường dẫn tải.
func (e *env) checkExportRoundTrip(store files.FileStore, purpose, prefix string) {
	t := e.t
	t.Helper()
	body := randomBytes(t, 4096)
	sum := sha256.Sum256(body)
	id := uuid.Must(uuid.NewV7())
	key := files.ExportKey(prefix, e.a, id.String()+"/"+hex.EncodeToString(sum[:])+".csv")
	if err := store.Put(t.Context(), files.BucketExports, key, bytes.NewReader(body), int64(len(body)), "text/csv"); err != nil {
		t.Fatalf("%s: Put vào bucket exports: %v", purpose, err)
	}
	got, err := e.register(e.a, files.NewFile{
		ID: id, Purpose: purpose, Key: key, SHA256: sum[:], Size: int64(len(body)),
		ContentType: "text/csv", FileName: "bao-cao.csv", CreatedBy: &e.actor,
	})
	if err != nil || got != id {
		t.Fatalf("%s: Register: %v %v", purpose, got, err)
	}
	if err := store.Put(t.Context(), files.BucketExports, key, bytes.NewReader([]byte("khac")), 4, "text/csv"); !errors.Is(err, files.ErrObjectExists) {
		t.Fatalf("%s: Put lặp lại cùng khóa: muốn ErrObjectExists, nhận %v", purpose, err)
	}
	rc, err := store.Open(t.Context(), files.BucketExports, key, mib)
	if err != nil {
		t.Fatalf("%s: Open: %v", purpose, err)
	}
	read, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(read, body) {
		t.Fatalf("%s: nội dung đọc lại khác (%v)", purpose, err)
	}
	if url, err := e.downloadURL(store, e.a, id, 5*time.Minute); err != nil || url == "" {
		t.Fatalf("%s: DownloadURL: %q %v", purpose, url, err)
	}
}

// Register của mục đích bucket exports chỉ nhận khóa bắt đầu bằng đúng tiền tố của mục đích + tenant của ngữ cảnh
// (nhầm tiền tố là nhầm hạn xóa: danh sách khách nằm 7 ngày); mục đích bucket private không nhận khóa của exports.
func TestRegister_ExportsKeyRefusals(t *testing.T) {
	e := newEnv(t)
	e.registerExportsPurposes()
	files.RegisterPurpose(testExportPurpose())
	if err := e.addPurposeRow("test_export", "private", "standard"); err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV7())
	cases := map[string]files.NewFile{
		"customers dùng tiền tố reports":   {Purpose: "test_exp_customers", Key: files.ExportKey(files.ExportReportsPrefix, e.a, "a.csv")},
		"reports dùng tiền tố customers":   {Purpose: "test_exp_reports", Key: files.ExportKey(files.ExportCustomersPrefix, e.a, "a.csv")},
		"exports dùng khóa priv":           {Purpose: "test_exp_reports", Key: files.ServerKey(e.a, id)},
		"exports khóa của tenant khác":     {Purpose: "test_exp_reports", Key: files.ExportKey(files.ExportReportsPrefix, e.b, "a.csv")},
		"exports khóa có ..":               {Purpose: "test_exp_reports", Key: files.ExportKey(files.ExportReportsPrefix, e.a, "../a.csv")},
		"exports khóa có //":               {Purpose: "test_exp_reports", Key: files.ExportKey(files.ExportReportsPrefix, e.a, "x//a.csv")},
		"exports thiếu tenant sau tiền tố": {Purpose: "test_exp_reports", Key: files.ExportReportsPrefix + e.a.String() + "a.csv"},
		"private dùng khóa exports":        {Purpose: "test_export", Key: files.ExportKey(files.ExportReportsPrefix, e.a, "a.csv")},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			f.ID, f.Size, f.ContentType, f.FileName = id, 10, "text/csv", "a.csv"
			if _, err := e.register(e.a, f); !errors.Is(err, files.ErrInvalidFile) {
				t.Fatalf("muốn ErrInvalidFile, nhận %v", err)
			}
		})
	}
}
