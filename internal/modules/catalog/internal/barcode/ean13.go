// Package barcode sinh và kiểm mã EAN-13 nội bộ của catalog (kế hoạch M1, Task M1-27, Q5): tiền tố "2" (dải dùng
// trong cửa hàng của GS1), 11 chữ số thứ tự lấy từ bộ đếm theo tenant, chữ số kiểm cuối.
package barcode

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
)

// ErrExhausted báo bộ đếm vượt 11 chữ số (99.999.999.999 mã).
var ErrExhausted = errors.New("barcode: hết dải mã nội bộ")

const maxSerial = 99_999_999_999

// NextEAN13 lấy số kế tiếp của bộ đếm tenant trong ngữ cảnh của tx (một câu, khóa dòng của tenant tới khi tx kết
// thúc) và trả mã EAN-13 "2" + 11 chữ số + số kiểm.
func NextEAN13(ctx context.Context, tx pgx.Tx) (string, error) {
	r, err := Reserve(ctx, tx, 1)
	if err != nil {
		return "", err
	}
	code, _, err := r.Next() // dải một số: luôn còn
	return code, err
}

// Range là dải số [From, To] của bộ đếm đã cấp cho bên giữ nó (From > To: dải rỗng hay đã dùng hết). Ghi được ra
// JSON (payload việc nền) để mỗi lần chạy lại dùng đúng dải đó mà không khóa bộ đếm lần nữa.
type Range struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// Reserve cấp n số liền nhau của bộ đếm tenant trong tx (một câu; khóa dòng bộ đếm tới khi tx kết thúc). n < 1: dải
// rỗng, không chạm database.
func Reserve(ctx context.Context, tx pgx.Tx, n int64) (Range, error) {
	if n < 1 {
		return Range{From: 1, To: 0}, nil
	}
	last, err := store.New(tx).ReserveBarcodeNumbers(ctx, n)
	if err != nil {
		return Range{}, fmt.Errorf("tăng bộ đếm barcode: %w", err)
	}
	return Range{From: last - n + 1, To: last}, nil
}

// Next lấy số đầu của dải r và trả mã EAN-13 của nó; ok = false khi dải đã hết. Số vượt 11 chữ số → ErrExhausted.
func (r *Range) Next() (code string, ok bool, err error) {
	if r.From > r.To {
		return "", false, nil
	}
	n := r.From
	r.From++
	code, err = formatInternal(n)
	return code, err == nil, err
}

// formatInternal dựng mã EAN-13 nội bộ cho số thứ tự n (1 ≤ n ≤ 99.999.999.999).
func formatInternal(n int64) (string, error) {
	if n < 1 || n > maxSerial {
		return "", ErrExhausted
	}
	body := fmt.Sprintf("2%011d", n)
	return body + strconv.Itoa(checkDigit(body)), nil
}

// ValidEAN13 cho biết s là 13 chữ số với chữ số kiểm đúng.
func ValidEAN13(s string) bool {
	if len(s) != 13 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return int(s[12]-'0') == checkDigit(s[:12])
}

// checkDigit của 12 chữ số đầu: trọng số 1, 3 xen kẽ từ trái.
func checkDigit(body string) int {
	sum := 0
	for i := range len(body) {
		d := int(body[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return (10 - sum%10) % 10
}
