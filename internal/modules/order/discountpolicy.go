package order

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
)

// PendingDiscountApproval là lý do chờ khi giảm tay vượt ngưỡng duyệt (Q3): đơn ở new (hoặc draft) cho tới khi người
// có quyền order.discount.approve duyệt (Task M2-19).
const PendingDiscountApproval = "discount_approval"

// reasonDiscountOverThreshold là lý do ghi ở dòng lịch sử tạo đơn chờ duyệt giảm giá (CHECK ^[a-z][a-z0-9_]*$).
const reasonDiscountOverThreshold = "manual_discount_over_threshold"

// NeedsApproval báo đơn phải chờ duyệt: giảm tay (giảm dòng + giảm cấp đơn, không tính mã giảm giá; Q3) vượt ngưỡng
// s và người gọi không có quyền duyệt giảm giá.
func NeedsApproval(t pricing.Totals, s Settings, actorCanApprove bool) bool {
	return overThreshold(t, s) && !actorCanApprove
}

func overThreshold(t pricing.Totals, s Settings) bool {
	return pricing.ManualDiscountBP(t) > int64(s.DiscountApprovalThresholdBP)
}

// approval là trạng thái lúc tạo sau khi xét ngưỡng duyệt giảm giá.
type approval struct {
	status, pending string
	approvedBy      pgtype.UUID
	approvedAt      pgtype.Timestamptz
}

// decideApproval xét ngưỡng cho ý định status (draft, new, confirmed): cần duyệt → draft giữ draft, còn lại sang new,
// lý do chờ PendingDiscountApproval; vượt ngưỡng mà người gọi có quyền duyệt → giữ status, ghi người duyệt là người gọi
// (cùng giờ tạo); không vượt → giữ status, không ghi người duyệt.
func decideApproval(status string, t pricing.Totals, s Settings, a Actor, now time.Time) approval {
	out := approval{status: status}
	switch {
	case !overThreshold(t, s):
	case NeedsApproval(t, s, a.CanApproveDiscount):
		out.pending = PendingDiscountApproval
		if status != StatusDraft {
			out.status = StatusNew
		}
	case a.UserID != uuid.Nil:
		out.approvedBy = a.id()
		out.approvedAt = pgtype.Timestamptz{Time: now, Valid: true}
	}
	return out
}
