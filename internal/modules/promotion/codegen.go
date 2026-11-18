package promotion

import (
	"crypto/rand"
	"fmt"
	"regexp"
)

// CodeAlphabet là bảng chữ của mã sinh: bỏ I, O, 0, 1 (dễ đọc nhầm), đúng 32 ký tự.
const CodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// codeLength: số ký tự ngẫu nhiên của mã sinh (32^8 ≈ 1,1 × 10^12 khả năng).
const codeLength = 8

// codeAttempts: số lần sinh lại tối đa cho một mã khi trùng mã có sẵn.
const codeAttempts = 5

var prefixPattern = regexp.MustCompile(`^[A-Z0-9]{0,8}$`)

// NewCode sinh một mã 8 ký tự trong CodeAlphabet bằng crypto/rand; prefix khác rỗng (chữ hoa, số, tối đa 8 ký tự) thì
// mã là "<prefix>-<8 ký tự>". Prefix sai → ErrInvalidInput.
func NewCode(prefix string) (string, error) {
	if !prefixPattern.MatchString(prefix) {
		return "", fmt.Errorf("tiền tố mã: %w", ErrInvalidInput)
	}
	var b [codeLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("sinh mã ngẫu nhiên: %w", err)
	}
	for i := range b {
		b[i] = CodeAlphabet[b[i]&byte(len(CodeAlphabet)-1)] // 32 ký tự: 5 bit thấp, không lệch
	}
	if prefix == "" {
		return string(b[:]), nil
	}
	return prefix + "-" + string(b[:]), nil
}
