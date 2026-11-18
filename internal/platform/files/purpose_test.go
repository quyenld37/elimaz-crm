package files_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

func testPrivateImagePurpose() files.Purpose {
	return files.Purpose{
		Key: "test_private_image", Bucket: files.BucketPrivate, AllowedTypes: []string{"image/jpeg"},
		MaxBytes: 20 * mib, Image: true, Upload: true, Private: true,
	}
}

func pgCode(err error) string {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pe.Code
	}
	return ""
}

// Sổ mục đích trong Go phải bằng đúng các dòng của platform.file_purposes, cùng bucket, cùng mode (Q38, R10).
func TestPurposes_GoMatchesTable(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	m1 := files.NewRegistry(files.M1Purposes()...)
	if err := m1.Check(ctx, e.staff.App); err != nil {
		t.Fatalf("mục đích M1 phải khớp bảng sau migration: %v", err)
	}
	for _, key := range []string{"product_image", "catalog_import"} {
		p, ok := files.PurposeOf(key)
		if !ok {
			t.Fatalf("sổ mặc định thiếu %s", key)
		}
		if mp, _ := m1.Lookup(key); mp.Bucket != p.Bucket || mp.MaxBytes != p.MaxBytes {
			t.Fatalf("%s: sổ mặc định khác M1Purposes", key)
		}
	}

	// Go có test_export mà bảng không có → lệch, nêu khóa.
	extra := files.NewRegistry(append(files.M1Purposes(), testExportPurpose())...)
	if err := extra.Check(ctx, e.staff.App); !errors.Is(err, files.ErrPurposeMismatch) || !strings.Contains(err.Error(), "test_export") {
		t.Fatalf("thiếu dòng test_export phải báo lệch nêu khóa, nhận %v", err)
	}
	// Bảng có dòng mà Go không có → lệch.
	if err := e.addPurposeRow("test_export", "private", "standard"); err != nil {
		t.Fatal(err)
	}
	if err := m1.Check(ctx, e.staff.App); !errors.Is(err, files.ErrPurposeMismatch) || !strings.Contains(err.Error(), "test_export") {
		t.Fatalf("dòng test_export không có trong Go phải báo lệch, nhận %v", err)
	}
	if err := extra.Check(ctx, e.staff.App); err != nil {
		t.Fatalf("đã có dòng test_export: %v", err)
	}
	// Private trong Go mà bảng mode = standard → lệch nêu khóa.
	if err := e.addPurposeRow("test_private_image", "private", "standard"); err != nil {
		t.Fatal(err)
	}
	priv := files.NewRegistry(append(files.M1Purposes(), testExportPurpose(), testPrivateImagePurpose())...)
	if err := priv.Check(ctx, e.staff.App); !errors.Is(err, files.ErrPurposeMismatch) || !strings.Contains(err.Error(), "test_private_image") {
		t.Fatalf("Private trong Go, standard trong bảng phải báo lệch nêu khóa, nhận %v", err)
	}
	// Bucket khác → lệch.
	pub := testExportPurpose()
	pub.Bucket = files.BucketPublic
	if err := files.NewRegistry(append(files.M1Purposes(), pub)...).Check(ctx, e.staff.App); !errors.Is(err, files.ErrPurposeMismatch) {
		t.Fatalf("bucket khác phải báo lệch, nhận %v", err)
	}
}

// CHECK của bảng: mode private đòi bucket private; khóa đúng dạng. crm_app không ghi được bảng.
func TestPurposes_TableConstraints(t *testing.T) {
	e := newEnv(t)
	if err := e.addPurposeRow("test_bad", "public", "private"); pgCode(err) != "23514" {
		t.Fatalf("mode private, bucket public phải lỗi CHECK 23514, nhận %v", err)
	}
	if err := e.addPurposeRow("X", "private", "standard"); pgCode(err) != "23514" {
		t.Fatalf("khóa sai dạng phải lỗi CHECK, nhận %v", err)
	}
	if _, err := e.staff.App.Pool.Exec(t.Context(),
		"INSERT INTO platform.file_purposes (key, bucket) VALUES ('test_app', 'private')"); pgCode(err) != "42501" {
		t.Fatalf("crm_app chèn file_purposes phải bị từ chối 42501, nhận %v", err)
	}
	// Tệp có purpose không có trong bảng → lỗi khóa ngoại.
	err := e.staff.TryExec(t.Context(), e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.files (tenant_id, purpose, bucket, key, original_name, declared_type,
			size_bytes, status) VALUES ($1, 'khong_co', 'tmp', $2, 'a', 'image/jpeg', 1, 'awaiting_upload')`, e.a, "tmp/x/"+uuid.NewString())
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "23503" {
		t.Fatalf("purpose lạ phải lỗi khóa ngoại 23503, nhận %v", err)
	}
}

func TestRegisterPurpose_PanicsOnInvalid(t *testing.T) {
	cases := map[string]files.Purpose{
		"private ngoài bucket private": {Key: "test_p1", Bucket: files.BucketPublic, AllowedTypes: []string{"image/jpeg"}, MaxBytes: 1, Image: true, Private: true},
		"private không phải ảnh":       {Key: "test_p2", Bucket: files.BucketPrivate, AllowedTypes: []string{"image/jpeg"}, MaxBytes: 1, Private: true},
		"khóa sai dạng":                {Key: "Ab", Bucket: files.BucketPrivate, AllowedTypes: []string{"a/b"}, MaxBytes: 1},
		"bucket lạ":                    {Key: "test_p3", Bucket: "khac", AllowedTypes: []string{"a/b"}, MaxBytes: 1},
		"không có loại":                {Key: "test_p4", Bucket: files.BucketPrivate, MaxBytes: 1},
		"MaxBytes 0":                   {Key: "test_p5", Bucket: files.BucketPrivate, AllowedTypes: []string{"a/b"}},
		"khai lại khác":                {Key: "product_image", Bucket: files.BucketPrivate, AllowedTypes: []string{"image/jpeg"}, MaxBytes: 1},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("RegisterPurpose(%+v) phải panic", p)
				}
			}()
			files.RegisterPurpose(p)
		})
	}
	// Khai lại đúng như cũ thì không panic (khởi động lại trong cùng tiến trình test).
	for _, p := range files.M1Purposes() {
		files.RegisterPurpose(p)
	}
}

// Register với mục đích chế độ private → ErrPurposeMode (ảnh riêng tư phải qua imager, R10). Tải lên với mục đích
// private (Upload) → complete xếp file.process_private, không xếp file.process.
func TestPurposes_PrivateMode(t *testing.T) {
	e := newEnv(t)
	files.RegisterPurpose(testPrivateImagePurpose())
	if err := e.addPurposeRow("test_private_image", "private", "private"); err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := e.register(e.a, files.NewFile{
		ID: id, Purpose: "test_private_image", Key: files.ServerKey(e.a, id), Size: 10,
		ContentType: "image/jpeg", FileName: "a.jpg",
	}); !errors.Is(err, files.ErrPurposeMode) {
		t.Fatalf("Register mục đích private: muốn ErrPurposeMode, nhận %v", err)
	}
	store := testkit.MemFileStore(t)
	f, put := e.mustCreate(store, files.UploadInput{TenantID: e.a, Purpose: "test_private_image", FileName: "a.jpg", ContentType: "image/jpeg", SizeBytes: 10})
	if f.Bucket != files.BucketTmp || f.Key != files.TmpKey(e.a, f.ID) {
		t.Fatalf("ảnh riêng tư vẫn chỉ vào crm-tmp: %+v", f)
	}
	if code := putVia(t, put, randomBytes(t, 10)); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	if _, err := e.complete(store, e.a, f.ID); err != nil {
		t.Fatal(err)
	}
	if e.jobs("file.process_private", f.ID) != 1 || e.jobs("file.process", f.ID) != 0 {
		t.Fatalf("mục đích private: muốn 1 file.process_private, 0 file.process; có %d, %d",
			e.jobs("file.process_private", f.ID), e.jobs("file.process", f.ID))
	}
	// Tệp không phải ảnh (catalog_import) không xếp lệnh xử lý ảnh.
	x, xput := e.mustCreate(store, files.UploadInput{
		TenantID: e.a, Purpose: "catalog_import", FileName: "a.xlsx",
		ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", SizeBytes: 10,
	})
	if code := putVia(t, xput, randomBytes(t, 10)); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	got, err := e.complete(store, e.a, x.ID)
	if err != nil || got.Status != files.StatusUploaded || e.jobs("file.process", x.ID) != 0 {
		t.Fatalf("catalog_import: %+v %v, %d lệnh", got, err, e.jobs("file.process", x.ID))
	}
}
