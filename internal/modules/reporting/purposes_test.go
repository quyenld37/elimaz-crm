package reporting_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

// Hai mục đích tệp xuất của M6 (0604, R5): dòng bucket private, chế độ standard; sổ Go có sau RegisterFilePurposes,
// chỉ ghi từ máy chủ (không tải lên), chỉ nhận .xlsx, tối đa 100 MiB.
func TestFilePurposes_ExportRows(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	for _, key := range []string{reporting.PurposeReportExport, reporting.PurposeCustomerExport} {
		if n := s.Count(t, `SELECT count(*) FROM platform.file_purposes WHERE key = $1 AND bucket = 'private'
			AND mode = 'standard'`, key); n != 1 {
			t.Errorf("%s: %d dòng private/standard, muốn 1", key, n)
		}
	}
	reporting.RegisterFilePurposes()
	for _, want := range reporting.FilePurposes() {
		p, ok := files.PurposeOf(want.Key)
		if !ok {
			t.Fatalf("%s chưa có trong sổ của tiến trình", want.Key)
		}
		if p.Bucket != files.BucketPrivate || p.Upload || p.Image || p.MaxBytes != 100<<20 ||
			len(p.AllowedTypes) != 1 || p.AllowedTypes[0] != reporting.XLSXContentType {
			t.Errorf("%s: %+v", want.Key, p)
		}
	}
	if len(reporting.FilePurposes()) != 2 {
		t.Fatalf("FilePurposes: %d mục, muốn 2", len(reporting.FilePurposes()))
	}
}

// files.Register với report_export → dòng ready ở bucket private; khóa lạ → ErrUnknownPurpose; chèn thẳng dòng tệp có
// mục đích lạ → khóa ngoại 23503; tải lên từ trình duyệt với mục đích xuất → ErrUploadNotAllowed.
func TestFilePurposes_RegisterAndUpload(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	reporting.RegisterFilePurposes()
	tenant := s.Tenant(t, "A")
	sum := sha256.Sum256([]byte("xlsx"))
	register := func(purpose string) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		var got uuid.UUID
		err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
			var err error
			got, err = files.Register(ctx, tx, files.NewFile{
				ID: id, Purpose: purpose, Key: "priv/" + tenant.String() + "/reports/" + id.String() + "/" +
					hex.EncodeToString(sum[:]) + ".xlsx",
				SHA256: sum[:], Size: 4, ContentType: reporting.XLSXContentType, FileName: "bao-cao.xlsx",
			})
			return err //nolint:wrapcheck // test
		})
		return got, err //nolint:wrapcheck // test
	}
	for _, key := range []string{reporting.PurposeReportExport, reporting.PurposeCustomerExport} {
		id, err := register(key)
		if err != nil {
			t.Fatalf("Register %s: %v", key, err)
		}
		if n := s.CountIn(t, tenant, `SELECT count(*) FROM platform.files WHERE id = $1 AND status = 'ready'
			AND bucket = 'private' AND purpose = $2`, id, key); n != 1 {
			t.Fatalf("%s: dòng tệp ready/private: %d", key, n)
		}
	}
	if _, err := register("report_exportx"); !errors.Is(err, files.ErrUnknownPurpose) {
		t.Fatalf("mục đích chưa đăng ký: %v", err)
	}
	err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO platform.files (tenant_id, purpose, bucket, key, original_name, declared_type,
			size_bytes, status) VALUES (current_setting('app.tenant_id')::uuid, 'report_exportx', 'private', $1, 'a.xlsx',
			$2, 1, 'ready')`, "priv/"+tenant.String()+"/reports/"+uuid.NewString(), reporting.XLSXContentType)
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "23503" {
		t.Fatalf("chèn thẳng mục đích lạ: muốn 23503, nhận %v", err)
	}
	store := filestest.MemFileStore(t)
	for _, key := range []string{reporting.PurposeReportExport, reporting.PurposeCustomerExport} {
		err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
			_, _, err := files.CreateUpload(ctx, tx, store, files.UploadInput{
				TenantID: tenant, Purpose: key, FileName: "a.xlsx", ContentType: reporting.XLSXContentType, SizeBytes: 10,
			}, uuid.Must(uuid.NewV7()))
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, files.ErrUploadNotAllowed) {
			t.Fatalf("tải lên %s: muốn ErrUploadNotAllowed, nhận %v", key, err)
		}
	}
}
