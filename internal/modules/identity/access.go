package identity

import (
	"context"
	"errors"
	"slices"
)

// Lỗi của việc kiểm thẻ đăng nhập. Lớp HTTP đổi cả hai sang 401 (mã khác nhau).
var (
	// ErrInvalidToken: thẻ thiếu, sai chữ ký, sai aud, sai iss, hết hạn, thiếu email, hoặc chưa có khóa để kiểm.
	ErrInvalidToken = errors.New("identity: thẻ đăng nhập không hợp lệ")
	// ErrMFARequired: thẻ hợp lệ nhưng không mang dấu đã qua bước hai (ADR-35).
	ErrMFARequired = errors.New("identity: thẻ chưa qua bước hai")
)

// Verifier kiểm một thẻ đăng nhập và trả email của nhân viên. Lỗi bọc ErrInvalidToken hoặc ErrMFARequired. Bản
// hiện thực cho Cloudflare Access: internal/adapters/cfaccess (cmd/crm nối vào lớp HTTP).
type Verifier interface {
	Verify(ctx context.Context, token string) (email string, err error)
}

// CheckMFA là luật bước hai của thẻ Access: giá trị trường bước hai (claim, đọc từ JSON nên là string hoặc []any;
// []string cũng nhận) phải chứa ít nhất một phần tử của allowed. Thiếu trường, kiểu khác, hay allowed rỗng → lỗi bọc
// ErrMFARequired. So khớp nguyên văn (phân biệt hoa thường).
func CheckMFA(claim any, allowed []string) error {
	var vals []string
	switch v := claim.(type) {
	case string:
		vals = []string{v}
	case []string:
		vals = v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				vals = append(vals, s)
			}
		}
	}
	if slices.ContainsFunc(vals, func(s string) bool { return slices.Contains(allowed, s) }) {
		return nil
	}
	return ErrMFARequired
}
