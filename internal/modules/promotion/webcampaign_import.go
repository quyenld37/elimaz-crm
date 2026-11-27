package promotion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
)

// ImportedCampaign là một chiến dịch cũ của website (Task 467): giữ id (liên kết /uu-dai/<tên>-<8 hex đầu id> của
// website còn chạy), trạng thái lưu (chỉ draft hay paused: chiến dịch đang bật nhập ở paused, QĐ35) và ngày tạo.
type ImportedCampaign struct {
	ID        uuid.UUID
	State     string
	CreatedAt time.Time
	Input     WebCampaignInput
}

// ImportCampaign ghi chiến dịch in của người bán seller trong tx của bên gọi (tx tenant), cùng luật với CreateCampaign
// (checkCampaignInput: đầu vào sai, đích không có → ErrInvalidInput; vượt trần → ErrPercentTooHigh), revision 1, một
// sự kiện TopicWebCampaignChanged. id đã có ở tenant này (kể cả đã xóa) → trả bản đang có (nil-zero khi đã xóa hay
// của người bán khác), created false, không ghi gì (chạy lại 0 thay đổi). id rỗng, State khác draft/paused →
// ErrInvalidInput; trùng link_key (8 hex đầu của id) hay id của tenant khác (id là khóa chính toàn bảng) →
// ErrInvalidInput kèm "mã chiến dịch đã được dùng" (lỗi chỉ của lệnh nhập, không qua HTTP).
func (w Writer) ImportCampaign(ctx context.Context, tx pgx.Tx, a Actor, seller uuid.UUID, in ImportedCampaign) (
	c WebCampaign, created bool, err error,
) {
	if in.ID == uuid.Nil || (in.State != StateDraft && in.State != StatePaused) || in.CreatedAt.IsZero() {
		return WebCampaign{}, false, fmt.Errorf("mã, trạng thái, ngày tạo của chiến dịch nhập: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	found, err := q.WebCampaignIDExists(ctx, in.ID)
	if err != nil {
		return WebCampaign{}, false, fmt.Errorf("kiểm chiến dịch có sẵn: %w", err)
	}
	if found {
		c, err := GetWebCampaign(ctx, tx, seller, in.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return WebCampaign{}, false, err
		}
		return c, false, nil
	}
	input, err := checkCampaignInput(ctx, tx, seller, in.Input)
	if err != nil {
		return WebCampaign{}, false, err
	}
	row, err := q.InsertImportedWebCampaign(ctx, store.InsertImportedWebCampaignParams{
		ID: in.ID, SellerID: seller, Name: input.Name, Percent: int32(input.Percent), //nolint:gosec // G115: 1–99
		Scope: input.Scope, AllowCoupon: input.AllowCoupon, Clearance: input.Clearance, StartsAt: input.StartsAt,
		EndsAt: input.EndsAt, State: in.State, CreatedBy: a.UserID, CreatedAt: in.CreatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebCampaign{}, false, fmt.Errorf("mã chiến dịch đã được dùng (link_key hay tenant khác): %w", ErrInvalidInput)
	}
	if err != nil {
		return WebCampaign{}, false, fmt.Errorf("ghi chiến dịch nhập: %w", err)
	}
	c = campaignFrom(store.WebCampaignByIDRow(row))
	if err := replaceTargets(ctx, q, c.ID, input.Scope, input.TargetIDs); err != nil {
		return WebCampaign{}, false, err
	}
	c.TargetIDs = input.TargetIDs
	return c, true, w.campaignChanged(ctx, tx, a.TenantID, c, false)
}
