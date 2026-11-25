package channel_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func testKeyring(t *testing.T) *fieldcrypt.Keyring {
	t.Helper()
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

// Token lưu mã hóa (cột không chứa chuỗi gốc), đọc lại đúng; bản mã chép sang tài khoản khác không mở được (AAD theo
// dòng); xóa token → ErrNoCredentials; loại token lạ, thiếu khóa, token rỗng là lỗi.
func TestCredentials_RoundTripAndBinding(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	a := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-a", "A")
	b := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-b", "B")
	kr := testKeyring(t)
	ctx := t.Context()
	const secret = "EAAB-secret-77"
	if err := e.in(ctx, func(tx pgx.Tx) error {
		if err := channel.StoreCredentials(ctx, tx, kr, a, logging.Secret(secret), "page_user"); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := channel.StoreCredentials(ctx, tx, kr, b, logging.Secret("khac"), "system_user"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT credential_enc FROM channel.channel_accounts WHERE id = $1", a).Scan(&raw)
	})
	if len(raw) == 0 || bytes.Contains(raw, []byte(secret)) || strings.Contains(string(raw), "EAAB") {
		t.Fatalf("credential_enc chứa token gốc hoặc rỗng: %q", raw)
	}
	if a := e.account(a); a.TokenKind != "page_user" {
		t.Fatalf("token_kind = %q", a.TokenKind)
	}
	if err := e.in(ctx, func(tx pgx.Tx) error {
		c, err := channel.CredentialsFor(ctx, tx, kr, a)
		if err != nil || string(c.AccessToken) != secret {
			t.Errorf("CredentialsFor = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Chủ bảng chép bản mã của a sang b: b không mở được.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE channel.channel_accounts AS d SET credential_enc = s.credential_enc,
			credential_kid = s.credential_kid FROM channel.channel_accounts AS s WHERE s.id = $1 AND d.id = $2`, a, b)
		return err //nolint:wrapcheck // test
	})
	checkCopiedAndCleared(t, e, kr, a, b)
	if a := e.account(a); a.TokenKind != "" || a.Version != 1 {
		t.Fatalf("sau khi xóa token: %+v", a)
	}
}

// StoreCredentials từ chối loại token lạ, thiếu khóa, token rỗng, tài khoản không có.
func TestCredentials_StoreRejects(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	a := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-a", "A")
	kr := testKeyring(t)
	ctx := t.Context()
	const secret = "EAAB-secret-77"
	if err := e.in(ctx, func(tx pgx.Tx) error {
		for _, c := range []struct {
			kr    *fieldcrypt.Keyring
			id    uuid.UUID
			tok   string
			kind  string
			want  error
			label string
		}{
			{kr, a, secret, "lạ", channel.ErrInvalidTokenKind, "loại lạ"},
			{nil, a, secret, "page_user", channel.ErrNoKeyring, "thiếu khóa"},
			{kr, a, "", "page_user", channel.ErrInvalidCredentials, "token rỗng"},
			{kr, uuid.New(), secret, "page_user", channel.ErrAccountNotFound, "không có tài khoản"},
		} {
			if err := channel.StoreCredentials(ctx, tx, c.kr, c.id, logging.Secret(c.tok), c.kind); !errors.Is(err, c.want) {
				t.Errorf("%s: %v, muốn %v", c.label, err, c.want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// checkCopiedAndCleared: b mang bản mã chép từ a → không mở được; thiếu khóa → ErrNoKeyring; xóa token của a →
// ErrNoCredentials; xóa tài khoản không có → ErrAccountNotFound.
func checkCopiedAndCleared(t *testing.T, e *chEnv, kr *fieldcrypt.Keyring, a, b uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	if err := e.in(ctx, func(tx pgx.Tx) error {
		if _, err := channel.CredentialsFor(ctx, tx, kr, b); !errors.Is(err, fieldcrypt.ErrDecrypt) {
			t.Errorf("bản mã chép sang dòng khác: %v, muốn ErrDecrypt", err)
		}
		if _, err := channel.CredentialsFor(ctx, tx, nil, a); !errors.Is(err, channel.ErrNoKeyring) {
			t.Errorf("thiếu khóa: %v", err)
		}
		if err := channel.ClearCredentials(ctx, tx, a); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := channel.CredentialsFor(ctx, tx, kr, a); !errors.Is(err, channel.ErrNoCredentials) {
			t.Errorf("sau khi xóa: %v", err)
		}
		if err := channel.ClearCredentials(ctx, tx, uuid.New()); !errors.Is(err, channel.ErrAccountNotFound) {
			t.Errorf("xóa tài khoản không có: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
