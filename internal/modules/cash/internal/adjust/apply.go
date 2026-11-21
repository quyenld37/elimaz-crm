package adjust

import "errors"

// ErrStale báo token gửi lên khác token tính lại trong transaction ghi (sổ của vận đơn đã đổi từ lúc xem trước, hay
// yêu cầu khác yêu cầu đã xem trước). Không ghi gì.
var ErrStale = errors.New("cash: bản xem trước điều chỉnh đã cũ")

// Check so token của bản xem trước người dùng đã xem với p (tính lại sau khi khóa sổ của vận đơn): khác → ErrStale.
func (p Plan) Check(token string) error {
	if token == "" || token != p.Token {
		return ErrStale
	}
	return nil
}

// IdempotencyKey là khóa chống trùng của bút toán chuyển ngăn: "adjustment:<token>" — một bản xem trước chỉ ghi được
// một lần (ghi xong thì sổ đổi, token đổi). Bút toán đảo dùng khóa của cash.Reverse.
func (p Plan) IdempotencyKey() string { return KindAdjustment + ":" + p.Token }
