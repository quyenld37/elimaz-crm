package files

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// ReadContent đọc toàn bộ nội dung tệp fileID của tenantID cho module dùng tệp không phải ảnh (tệp nhập Excel, Task
// M1-47): đọc dòng tệp trong một transaction đọc ngắn, rồi đọc kho **ngoài** transaction (ADR-46), tối đa maxBytes.
// Tệp phải có mục đích purpose, trạng thái uploaded hay ready, chưa xóa. Không có, của tenant khác, đã xóa →
// ErrNotFound; sai mục đích hay chưa tải xong (awaiting_upload, processing, rejected, lost) → ErrNotReady; nội dung
// vượt maxBytes → ErrTooLarge; đối tượng mất trên kho → ErrObjectNotFound; lỗi khác (database, kho) trả bọc nguyên.
func ReadContent(ctx context.Context, d *db.DB, fs FileStore, tenantID, fileID uuid.UUID, purpose string, maxBytes int64) ([]byte, error) {
	var row store.PlatformFile
	if err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		var err error
		row, err = getFile(ctx, tx, fileID)
		return err
	}); err != nil {
		return nil, err //nolint:wrapcheck // getFile đã bọc; lỗi của RetryTx trả nguyên
	}
	switch {
	case row.DeletedAt.Valid:
		return nil, fmt.Errorf("đọc tệp %s: %w", fileID, ErrNotFound)
	case row.Purpose != purpose, row.Status != StatusUploaded && row.Status != StatusReady:
		return nil, fmt.Errorf("đọc tệp %s: %w", fileID, ErrNotReady)
	}
	rc, err := fs.Open(ctx, Bucket(row.Bucket), row.Key, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("đọc tệp %s: %w", fileID, err)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("đọc tệp %s: %w", fileID, err)
	case int64(len(body)) > maxBytes:
		return nil, fmt.Errorf("đọc tệp %s: %w", fileID, ErrTooLarge)
	}
	return body, nil
}
