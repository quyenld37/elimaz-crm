package files

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// NewFile là tệp máy chủ vừa ghi lên kho (nhãn vận đơn, tệp chat, tệp xuất; A8). Bên gọi sinh ID, khóa bằng
// ServerKey(tenantID, ID), gọi FileStore.Put **ngoài** transaction, rồi Register trong transaction.
type NewFile struct {
	ID          uuid.UUID
	Purpose     string
	Key         string
	SHA256      []byte // nil hoặc 32 byte
	Size        int64
	ContentType string
	FileName    string
	ExpiresAt   *time.Time // tệp tạm (tệp xuất): DownloadURL trả ErrExpired sau thời điểm này
	CreatedBy   *uuid.UUID
}

// Register ghi dòng ready cho tệp máy chủ đã Put, bucket theo mục đích, trong tx (tenancy.WithTenant). Khóa đã có
// dòng → trả id cũ (chạy lại an toàn). Mục đích chưa đăng ký → ErrUnknownPurpose; chế độ private hay mục đích ảnh
// → ErrPurposeMode (ảnh phải qua imager, ADR-45: ảnh riêng tư dùng RegisterPrivateImage của Task M1-43, R10; ảnh
// standard dùng đường tải lên có file.process — không có tệp ảnh ready nào chưa qua imager); loại, dung lượng ngoài giới hạn của mục
// đích → ErrTypeNotAllowed, ErrTooLarge; khóa không bắt đầu bằng priv/<tenant trong ngữ cảnh>/, tên quá dài, mã băm
// sai độ dài → ErrInvalidFile. Put xong mà transaction lỗi thì đối tượng mồ côi, việc dọn của Q16 xử lý sau.
func Register(ctx context.Context, tx pgx.Tx, f NewFile) (uuid.UUID, error) {
	p, ok := PurposeOf(f.Purpose)
	switch {
	case !ok:
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w", ErrUnknownPurpose)
	case p.Private, p.Image:
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ %s: %w", p.Key, ErrPurposeMode)
	case f.ID == uuid.Nil:
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w: thiếu ID", ErrInvalidFile)
	case f.SHA256 != nil && len(f.SHA256) != 32:
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w: mã băm phải 32 byte", ErrInvalidFile)
	}
	if err := checkLimits(p, f.ContentType, f.Size); err != nil {
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w", err)
	}
	if err := checkName(f.FileName); err != nil {
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w", err)
	}
	s := store.New(tx)
	tenant, err := s.CurrentTenant(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: đọc tenant của ngữ cảnh: %w", err)
	}
	if !strings.HasPrefix(f.Key, serverPrefix(tenant)) || strings.Contains(f.Key, "..") || strings.Contains(f.Key, "//") {
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w: khóa không thuộc tenant trong ngữ cảnh", ErrInvalidFile)
	}
	params := store.InsertServerFileParams{
		ID: f.ID, TenantID: tenant, Purpose: p.Key, Bucket: string(p.Bucket), Key: f.Key, OriginalName: f.FileName,
		DeclaredType: f.ContentType, SizeBytes: f.Size, ContentSha256: f.SHA256,
	}
	if f.CreatedBy != nil {
		params.CreatedBy = optUUID(*f.CreatedBy)
	}
	if f.ExpiresAt != nil {
		params.ExpiresAt = pgtype.Timestamptz{Time: f.ExpiresAt.UTC(), Valid: true}
	}
	id, err := s.InsertServerFile(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = s.FileIDByKey(ctx, store.FileIDByKeyParams{Bucket: string(p.Bucket), Key: f.Key})
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi tệp máy chủ: %w", err)
	}
	return id, nil
}
