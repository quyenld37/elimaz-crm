package files

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// FileStatus là trạng thái công khai của một tệp cho module khác (kiểm trước khi gắn tệp vào bản ghi của mình).
type FileStatus struct {
	Purpose, Status string
	Deleted         bool
}

// getFile đọc dòng tệp trong tx (RLS: tenant khác như không có).
func getFile(ctx context.Context, tx pgx.Tx, fileID uuid.UUID) (store.PlatformFile, error) {
	row, err := store.New(tx).GetFile(ctx, fileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return row, fmt.Errorf("tệp %s: %w", fileID, ErrNotFound)
	case err != nil:
		return row, fmt.Errorf("đọc tệp %s: %w", fileID, err)
	}
	return row, nil
}

// Status trả mục đích, trạng thái, đã xóa chưa của tệp trong tenant của tx; không có → ErrNotFound.
func Status(ctx context.Context, tx pgx.Tx, fileID uuid.UUID) (FileStatus, error) {
	row, err := getFile(ctx, tx, fileID)
	if err != nil {
		return FileStatus{}, err
	}
	return FileStatus{Purpose: row.Purpose, Status: row.Status, Deleted: row.DeletedAt.Valid}, nil
}

// MarkDeleted đặt deleted_at (và người xóa; uuid.Nil là hệ thống), không xóa trên kho (việc dọn Q16 chạy bằng danh
// tính riêng). Không có → ErrNotFound; đã xóa → nil.
func MarkDeleted(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, actor uuid.UUID) error {
	if _, err := getFile(ctx, tx, fileID); err != nil {
		return err
	}
	if _, err := store.New(tx).MarkFileDeleted(ctx, store.MarkFileDeletedParams{ID: fileID, DeletedBy: optUUID(actor)}); err != nil {
		return fmt.Errorf("xóa tệp %s: %w", fileID, err)
	}
	return nil
}

// DownloadURL ký GET hạn ttl tới tệp trong tenant của tx, không gọi mạng. Tệp phải ready, chưa xóa, chưa mất, chưa
// hết hạn: không có/đã xóa/mất → ErrNotFound; quá expires_at → ErrExpired; awaiting_upload/uploaded/processing →
// ErrNotReady; rejected → ErrRejected. Tệp của mục đích chế độ private (R10) không bao giờ được ký tới bản thô: ký
// biến thể JPEG lớn nhất (đã bỏ siêu dữ liệu) của bộ kích thước của tệp trên BucketPrivate. Bên gọi kiểm quyền của
// mình trước.
func DownloadURL(ctx context.Context, tx pgx.Tx, fs FileStore, fileID uuid.UUID, ttl time.Duration, attachmentName string) (string, error) {
	row, err := getFile(ctx, tx, fileID)
	if err != nil {
		return "", err
	}
	switch {
	case row.DeletedAt.Valid, row.Status == StatusLost:
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrNotFound)
	case row.ExpiresAt.Valid && !time.Now().Before(row.ExpiresAt.Time):
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrExpired)
	case row.Status == StatusRejected:
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrRejected)
	case row.Status != StatusReady:
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrNotReady)
	}
	p, ok := PurposeOf(row.Purpose)
	if !ok {
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrNotReady)
	}
	bucket, key := Bucket(row.Bucket), row.Key
	if p.Private {
		if bucket, key, err = largestPrivateJPEG(ctx, tx, row); err != nil {
			return "", err
		}
	}
	url, err := fs.PresignGet(ctx, bucket, key, ttl, attachmentName)
	if err != nil {
		return "", fmt.Errorf("tải tệp %s: %w", fileID, err)
	}
	return url, nil
}

// largestPrivateJPEG là khóa biến thể JPEG rộng nhất của bộ kích thước của tệp riêng tư; chưa có → ErrNotReady.
func largestPrivateJPEG(ctx context.Context, tx pgx.Tx, row store.PlatformFile) (Bucket, string, error) {
	vs, err := store.New(tx).ListFileVariantsFull(ctx, row.ID)
	if err != nil {
		return "", "", fmt.Errorf("đọc biến thể %s: %w", row.ID, err)
	}
	best, key := int32(0), ""
	for _, v := range vs {
		if Bucket(v.Bucket) == BucketPrivate && v.Format == "jpeg" && v.PresetVersion == row.PresetVersion.Int32 && v.Width > best {
			best, key = v.Width, v.Key
		}
	}
	if key == "" {
		return "", "", fmt.Errorf("tải tệp %s: %w", row.ID, ErrNotReady)
	}
	return BucketPrivate, key, nil
}

// VariantURL là một biến thể ảnh công khai và đường dẫn của nó (Preset: phiên bản bộ kích thước).
type VariantURL struct {
	Preset int
	Width  int
	Format string
	URL    string
}

// PublicVariants trả biến thể công khai của các tệp ready, chưa xóa, trong tenant của tx, theo mã tệp, một câu SQL
// cho cả danh sách (module khác đọc đường dẫn ảnh mà không nhắc schema platform). Tệp của mục đích chế độ private
// không bao giờ có trong kết quả (R10); tệp không có, chưa ready, của tenant khác bị bỏ qua. Mọi bộ kích thước đã có
// đều được trả (bên gọi chọn theo IMAGE_PRESET_VERSION).
func PublicVariants(ctx context.Context, tx pgx.Tx, fileIDs []uuid.UUID, baseURL string) (map[uuid.UUID][]VariantURL, error) {
	out := map[uuid.UUID][]VariantURL{}
	if len(fileIDs) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).ListPublicVariants(ctx, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc biến thể công khai: %w", err)
	}
	for _, r := range rows {
		out[r.FileID] = append(out[r.FileID], VariantURL{
			Preset: int(r.PresetVersion), Width: int(r.Width), Format: r.Format, URL: baseURL + "/" + r.Key,
		})
	}
	return out, nil
}

// Info là tệp kèm biến thể công khai (getFile của API).
type Info struct {
	File
	Variants []VariantURL
}

// Describe đọc tệp trong tenant của tx kèm biến thể trên BucketPublic (đường dẫn = publicBaseURL + "/" + khóa);
// biến thể riêng tư không bao giờ có ở đây (R10). Không có hay đã xóa → ErrNotFound.
func Describe(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, publicBaseURL string) (Info, error) {
	row, err := getFile(ctx, tx, fileID)
	if err != nil {
		return Info{}, err
	}
	if row.DeletedAt.Valid {
		return Info{}, fmt.Errorf("tệp %s: %w", fileID, ErrNotFound)
	}
	vs, err := store.New(tx).ListFileVariantsFull(ctx, fileID)
	if err != nil {
		return Info{}, fmt.Errorf("đọc biến thể %s: %w", fileID, err)
	}
	out := Info{File: fileOf(row), Variants: []VariantURL{}}
	for _, v := range vs {
		if Bucket(v.Bucket) != BucketPublic {
			continue
		}
		out.Variants = append(out.Variants, VariantURL{
			Preset: int(v.PresetVersion), Width: int(v.Width), Format: v.Format, URL: publicBaseURL + "/" + v.Key,
		})
	}
	return out, nil
}
