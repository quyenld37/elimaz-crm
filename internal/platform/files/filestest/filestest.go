// Package filestest là helper test cho module khác cần tệp ảnh đã xử lý (gắn ảnh vào sản phẩm, bài viết): tạo dòng
// platform.files ready kèm biến thể của bộ kích thước v1 bằng đúng câu ghi của kho tệp, không gọi imager, không ghi
// đối tượng nào lên kho. Để test của họ không chép SQL của platform.files.
//
// Khi nào không dùng: mã sản phẩm (chỉ dùng trong test); test của chính đường xử lý ảnh (dùng imager giả của gói files).
package filestest

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// Image tạo tệp mục đích purpose của tenantID ở trạng thái ready kèm 12 biến thể của bộ kích thước v1 (khóa
// img/<sha>/v1/… trên public; mục đích có Private trong sổ mục đích → priv/<tenant>/<id>/v1/… trên private) và trả mã
// tệp. tx là crm_app hay chủ bảng trong tenancy.WithTenant của tenantID. Lỗi làm test dừng.
func Image(t testing.TB, tx pgx.Tx, tenantID uuid.UUID, purpose string) uuid.UUID {
	t.Helper()
	id := Uploaded(t, tx, tenantID, purpose)
	q := store.New(tx)
	row, err := q.LockFile(t.Context(), id)
	if err != nil {
		t.Fatalf("filestest: khóa tệp: %v", err)
	}
	sum := make([]byte, 32)
	_, _ = rand.Read(sum)
	if _, err := q.SetFileReady(t.Context(), store.SetFileReadyParams{
		DetectedType: pgtype.Text{String: "image/jpeg", Valid: true}, Width: pgtype.Int4{Int32: 2000, Valid: true},
		Height: pgtype.Int4{Int32: 3000, Valid: true}, ContentSha256: sum, PresetVersion: pgtype.Int4{Int32: 1, Valid: true},
		Bucket: row.Bucket, Key: row.Key, ID: id,
	}); err != nil {
		t.Fatalf("filestest: đặt ready: %v", err)
	}
	p, _ := files.PurposeOf(purpose)
	specs, _ := files.Preset(1)
	for _, s := range specs {
		b, key := files.BucketPublic, files.VariantKey(hex.EncodeToString(sum), 1, s.Width, s.Format)
		if p.Private {
			b, key = files.BucketPrivate, files.PrivateVariantKey(tenantID, id, 1, s.Width, s.Format)
		}
		if err := q.InsertFileVariant(t.Context(), store.InsertFileVariantParams{
			TenantID: tenantID, FileID: id, PresetVersion: 1, Width: int32(s.Width), Format: s.Format, //nolint:gosec // cỡ nhỏ
			Bucket: string(b), Key: key, SizeBytes: 1000,
		}); err != nil {
			t.Fatalf("filestest: ghi biến thể: %v", err)
		}
	}
	return id
}

// Uploaded tạo tệp mục đích purpose của tenantID đã tải lên, chưa xử lý xong (status uploaded) và trả mã tệp.
func Uploaded(t testing.TB, tx pgx.Tx, tenantID uuid.UUID, purpose string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	q := store.New(tx)
	if _, err := q.InsertUploadFile(t.Context(), store.InsertUploadFileParams{
		ID: id, TenantID: tenantID, Purpose: purpose, Key: files.TmpKey(tenantID, id), OriginalName: "anh.jpg",
		DeclaredType: "image/jpeg", SizeBytes: 1000,
	}); err != nil {
		t.Fatalf("filestest: ghi tệp: %v", err)
	}
	if _, err := q.LockFile(t.Context(), id); err != nil {
		t.Fatalf("filestest: khóa tệp: %v", err)
	}
	if _, err := q.SetUploadResult(t.Context(), store.SetUploadResultParams{Status: "uploaded", ID: id}); err != nil {
		t.Fatalf("filestest: đặt uploaded: %v", err)
	}
	return id
}

// MarkLost đánh dấu tệp ready fileID là mất trên kho (TP12), như đối chiếu sau khi dựng lại kho tệp.
func MarkLost(t testing.TB, tx pgx.Tx, fileID uuid.UUID) {
	t.Helper()
	n, err := store.New(tx).SetFileLost(t.Context(), fileID)
	if err != nil || n != 1 {
		t.Fatalf("filestest: đánh dấu mất %s: %d dòng, %v", fileID, n, err)
	}
}

// Rejected tạo tệp mục đích purpose của tenantID đã tải lên rồi bị từ chối khi xử lý (status rejected, lý do
// not_image), như sau khi imager của M1 thấy tệp không phải ảnh hợp lệ (R10), và trả mã tệp.
func Rejected(t testing.TB, tx pgx.Tx, tenantID uuid.UUID, purpose string) uuid.UUID {
	t.Helper()
	id := Uploaded(t, tx, tenantID, purpose)
	q := store.New(tx)
	if _, err := q.LockFile(t.Context(), id); err != nil {
		t.Fatalf("filestest: khóa tệp: %v", err)
	}
	if n, err := q.SetFileRejected(t.Context(), store.SetFileRejectedParams{ID: id, RejectReason: pgtype.Text{String: "not_image", Valid: true}}); err != nil || n != 1 {
		t.Fatalf("filestest: đặt rejected: %d dòng, %v", n, err)
	}
	return id
}
