package channel

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Loại token Page (cột token_kind).
const (
	TokenPageUser   = "page_user"
	TokenSystemUser = "system_user"
)

// AAD của token: gắn bản mã với đúng ô channel.channel_accounts.credential_enc của dòng (chép sang dòng khác không mở).
const (
	aadTable  = "channel.channel_accounts"
	aadColumn = "credential_enc"
)

var (
	// ErrNoKeyring báo tiến trình không có FIELD_ENC_KEYS (kênh Facebook chưa bật).
	ErrNoKeyring = errors.New("channel: chưa có khóa mã hóa token")
	// ErrNoCredentials báo tài khoản chưa có token (chưa kết nối hoặc đã ngắt).
	ErrNoCredentials = errors.New("channel: tài khoản kênh chưa có token")
	// ErrInvalidTokenKind báo loại token không phải page_user, system_user.
	ErrInvalidTokenKind = errors.New("channel: loại token không hợp lệ")
	// ErrInvalidCredentials báo token rỗng.
	ErrInvalidCredentials = errors.New("channel: token rỗng")
)

func credentialAAD(tenantID, id uuid.UUID) fieldcrypt.AAD {
	return fieldcrypt.AAD{TenantID: tenantID, Table: aadTable, Column: aadColumn, RowID: id}
}

// StoreCredentials mã hóa token (AES-GCM, khóa hiện hành của kr, AAD theo tenant và dòng) rồi ghi vào tài khoản id
// cùng loại kind (page_user, system_user), trong tx của bên gọi. Token không bao giờ ra log, audit, view.
func StoreCredentials(ctx context.Context, tx pgx.Tx, kr *fieldcrypt.Keyring, id uuid.UUID, token logging.Secret, kind string) error {
	switch {
	case kr == nil:
		return ErrNoKeyring
	case token == "":
		return ErrInvalidCredentials
	case kind != TokenPageUser && kind != TokenSystemUser:
		return ErrInvalidTokenKind
	}
	q := store.New(tx)
	cur, err := q.GetCredentials(ctx, id)
	if err != nil {
		return notFound(err, "đọc tài khoản kênh")
	}
	plain := []byte(token)
	ct, kid, err := kr.Seal(plain, credentialAAD(cur.TenantID, id))
	clear(plain)
	if err != nil {
		return fmt.Errorf("mã hóa token Page: %w", err)
	}
	if _, err := q.SetCredentials(ctx, store.SetCredentialsParams{
		CredentialEnc: ct, CredentialKid: pgtype.Text{String: kid, Valid: true},
		TokenKind: pgtype.Text{String: kind, Valid: true}, ID: id,
	}); err != nil {
		return fmt.Errorf("ghi token Page: %w", err)
	}
	return nil
}

// CredentialsFor giải mã token của tài khoản id ngay trước khi gọi kênh. Chưa có token → ErrNoCredentials; bản mã
// không thuộc dòng này hay khóa đã bỏ → lỗi bọc fieldcrypt.ErrDecrypt, fieldcrypt.ErrUnknownKey.
func CredentialsFor(ctx context.Context, tx pgx.Tx, kr *fieldcrypt.Keyring, id uuid.UUID) (Credentials, error) {
	if kr == nil {
		return Credentials{}, ErrNoKeyring
	}
	cur, err := store.New(tx).GetCredentials(ctx, id)
	if err != nil {
		return Credentials{}, notFound(err, "đọc token Page")
	}
	if cur.CredentialEnc == nil {
		return Credentials{}, ErrNoCredentials
	}
	plain, err := kr.Open(cur.CredentialEnc, cur.CredentialKid.String, credentialAAD(cur.TenantID, id))
	if err != nil {
		return Credentials{}, fmt.Errorf("giải mã token Page: %w", err)
	}
	c := Credentials{AccessToken: logging.Secret(plain)}
	clear(plain)
	return c, nil
}

// ClearCredentials xóa token và loại token của tài khoản id (ngắt kết nối, Meta báo token hỏng). Không có tài khoản
// → ErrAccountNotFound.
func ClearCredentials(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	n, err := store.New(tx).ClearCredentials(ctx, id)
	if err != nil {
		return fmt.Errorf("xóa token Page: %w", err)
	}
	if n == 0 {
		return ErrAccountNotFound
	}
	return nil
}
