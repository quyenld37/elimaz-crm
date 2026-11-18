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
// ErrNotReady; rejected → ErrRejected. Tệp của mục đích chế độ private (R10) không bao giờ được ký tới bản thô: tới
// khi Task M1-43 thêm nhánh ký biến thể JPEG lớn nhất trên BucketPrivate, trả ErrNotReady. Bên gọi kiểm quyền của
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
	if p, ok := PurposeOf(row.Purpose); !ok || p.Private {
		return "", fmt.Errorf("tải tệp %s: %w", fileID, ErrNotReady)
	}
	url, err := fs.PresignGet(ctx, Bucket(row.Bucket), row.Key, ttl, attachmentName)
	if err != nil {
		return "", fmt.Errorf("tải tệp %s: %w", fileID, err)
	}
	return url, nil
}

// VariantURL là một biến thể ảnh công khai và đường dẫn của nó.
type VariantURL struct {
	Width  int
	Format string
	URL    string
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
	vs, err := store.New(tx).ListFileVariants(ctx, fileID)
	if err != nil {
		return Info{}, fmt.Errorf("đọc biến thể %s: %w", fileID, err)
	}
	out := Info{File: fileOf(row), Variants: []VariantURL{}}
	for _, v := range vs {
		if Bucket(v.Bucket) != BucketPublic {
			continue
		}
		out.Variants = append(out.Variants, VariantURL{Width: int(v.Width), Format: v.Format, URL: publicBaseURL + "/" + v.Key})
	}
	return out, nil
}
