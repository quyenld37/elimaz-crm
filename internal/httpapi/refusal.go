package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// refusableWrite chạy change trong WriteTx của người gọi. Lỗi mà refusal nhận là lỗi nghiệp vụ (câu trả lời không đổi
// gì): tx lùi, không lưu vào khóa chống trùng (final = false), gửi lại cùng khóa thì xét lại; lỗi đổi sang HTTP ở
// errors.go. Kết quả khác lỗi được lưu với status. Dùng chung cho đường ghi của catalog và inventory.
func (h *handler) refusableWrite(ctx context.Context, status int, refusal func(error) bool, what string,
	change func(tx pgx.Tx, u identity.AuthUser) (any, error),
) (any, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var out any
	var refused error
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		body, err := change(tx, u)
		switch {
		case refusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		out = body
		return status, body, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("%s: %w", what, refused)
	}
	return out, nil
}
