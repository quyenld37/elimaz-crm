// Package fieldcrypt mã hóa từng trường bí mật lưu trong database (khóa API hãng của M3, token Page của M4, bí mật của
// M7): AES-256-GCM của thư viện chuẩn, nonce 12 byte ngẫu nhiên, bản mã gắn với đúng dòng bằng AAD
// (tenant|bảng|cột|dòng) nên chép bản mã sang dòng, cột hay tenant khác thì không mở được. Một biến môi trường
// FIELD_ENC_KEYS giữ mọi khóa (keyspec); khóa đầu mã hóa, mọi khóa giải mã; đổi khóa = thêm khóa mới lên đầu, ghi lại
// các dòng NeedsRewrap, rồi bỏ khóa cũ (docs/RUNBOOK.md).
package fieldcrypt

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

// Kích thước của bản mã: nonce || ciphertext || tag.
const (
	nonceSize = 12
	tagSize   = 16
)

// Lỗi của gói: không nêu lý do chi tiết (sai khóa, sai AAD hay bản mã hỏng đều là ErrDecrypt).
var (
	// ErrUnknownKey: key_id của bản mã không có trong FIELD_ENC_KEYS (khóa đã bị bỏ trước khi ghi lại hết các dòng).
	ErrUnknownKey = errors.New("fieldcrypt: không có khóa mang key_id này")
	// ErrDecrypt: không mở được bản mã (bị sửa, sai khóa, sai AAD, quá ngắn).
	ErrDecrypt = errors.New("fieldcrypt: không giải mã được")
	// ErrInvalidAAD: Table hoặc Column rỗng hay có ký tự ngoài [a-z0-9_.] (lỗi lập trình của bên gọi).
	ErrInvalidAAD = errors.New("fieldcrypt: AAD không hợp lệ")
)

// aadName: Table, Column là tên bảng, cột (có schema), không chứa dấu phân cách "|" nên mã hóa AAD không mơ hồ.
var aadName = regexp.MustCompile(`^[a-z_][a-z0-9_.]*$`)

// AAD là dữ liệu đi kèm gắn bản mã với đúng một ô: tenant, bảng (đủ schema), cột, mã dòng.
type AAD struct {
	TenantID      uuid.UUID
	Table, Column string
	RowID         uuid.UUID
}

// bytes mã hóa AAD thành chuỗi cố định "tenant|table|column|row".
func (a AAD) bytes() ([]byte, error) {
	if !aadName.MatchString(a.Table) || !aadName.MatchString(a.Column) {
		return nil, ErrInvalidAAD
	}
	return []byte(a.TenantID.String() + "|" + a.Table + "|" + a.Column + "|" + a.RowID.String()), nil
}

// Seal mã hóa plain bằng khóa đầu: trả nonce || ciphertext || tag và key_id của khóa đã dùng (lưu cạnh bản mã).
func (k *Keyring) Seal(plain []byte, aad AAD) (ct []byte, kid string, err error) {
	ad, err := aad.bytes()
	if err != nil {
		return nil, "", err
	}
	cur := k.keys[k.current]
	nonce := make([]byte, nonceSize, nonceSize+len(plain)+tagSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", fmt.Errorf("fieldcrypt: sinh nonce: %w", err)
	}
	return cur.Seal(nonce, nonce, plain, ad), k.current, nil
}

// Open giải mã ct (của Seal) bằng khóa kid với đúng AAD lúc mã hóa.
func (k *Keyring) Open(ct []byte, kid string, aad AAD) ([]byte, error) {
	ad, err := aad.bytes()
	if err != nil {
		return nil, err
	}
	aead, ok := k.keys[kid]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(ct) < nonceSize+tagSize {
		return nil, ErrDecrypt
	}
	plain, err := aead.Open(nil, ct[:nonceSize], ct[nonceSize:], ad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// NeedsRewrap báo bản mã mang kid cần ghi lại bằng khóa đầu (kid khác khóa đầu, kể cả kid không còn trong danh sách).
func (k *Keyring) NeedsRewrap(kid string) bool { return kid != k.current }
