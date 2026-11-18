package files

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// NewPrivateImage là ảnh riêng tư của khách do máy chủ nhận (R10: ảnh chat khách gửi ở M4, ảnh kèm yêu cầu đặt hàng
// ở M5). Bên gọi sinh ID (uuidv7), gọi FileStore.Put(BucketTmp, TmpKey(tenantID, ID)) **ngoài** transaction (ảnh thô
// có thể còn EXIF, GPS nên chỉ nằm ở crm-tmp, tự xóa sau 24 giờ), rồi RegisterPrivateImage trong transaction.
type NewPrivateImage struct {
	ID          uuid.UUID
	Purpose     string
	Size        int64
	ContentType string
	FileName    string
	CreatedBy   *uuid.UUID
}

// RegisterPrivateImage chèn dòng uploaded (bucket private, khóa ServerKey: tiền tố của biến thể, không có đối tượng
// ở đúng khóa này) và xếp file.process_private (DedupeKey = mã tệp) trong tx (tenancy.WithTenant). Mục đích chưa đăng
// ký → ErrUnknownPurpose; không phải chế độ private → ErrPurposeMode; loại, dung lượng ngoài giới hạn →
// ErrTypeNotAllowed, ErrTooLarge; thiếu ID, tên sai → ErrInvalidFile. Cùng ID lần hai → trả id cũ, không thêm lệnh.
func RegisterPrivateImage(ctx context.Context, tx pgx.Tx, q queue.JobQueue, in NewPrivateImage) (uuid.UUID, error) {
	p, ok := PurposeOf(in.Purpose)
	switch {
	case !ok:
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: %w", ErrUnknownPurpose)
	case !p.Private:
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư %s: %w", p.Key, ErrPurposeMode)
	case in.ID == uuid.Nil:
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: %w: thiếu ID", ErrInvalidFile)
	}
	if err := checkLimits(p, in.ContentType, in.Size); err != nil {
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: %w", err)
	}
	if err := checkName(in.FileName); err != nil {
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: %w", err)
	}
	s := store.New(tx)
	tenant, err := s.CurrentTenant(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: đọc tenant của ngữ cảnh: %w", err)
	}
	params := store.InsertPrivateImageFileParams{
		ID: in.ID, TenantID: tenant, Purpose: p.Key, Key: ServerKey(tenant, in.ID), OriginalName: in.FileName,
		DeclaredType: in.ContentType, SizeBytes: in.Size,
	}
	if in.CreatedBy != nil {
		params.CreatedBy = optUUID(*in.CreatedBy)
	}
	id, err := s.InsertPrivateImageFile(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		old, gerr := s.GetFile(ctx, in.ID)
		if gerr != nil {
			return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: đọc dòng đã có: %w", gerr)
		}
		return old.ID, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi ảnh riêng tư: %w", err)
	}
	if err := enqueueProcess(ctx, tx, q, store.PlatformFile{ID: id, TenantID: tenant, Purpose: p.Key}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
