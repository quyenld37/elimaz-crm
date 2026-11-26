// Package livetest dựng dữ liệu thử của phiên live và chốt comment live (Task 172) cho test của module conversation
// và lớp HTTP: khách, phiên live, comment live, bật gửi thật cho Page, câu sửa dữ liệu thử.
package livetest

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Customer tạo khách tên name (customer.Create, nguồn tên do nhân viên nhập); trả mã.
func Customer(t testing.TB, s *testkit.Staff, tenant uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tenancy.WithTenant(t.Context(), s.Owner, tenant, func(tx pgx.Tx) (err error) {
		id, err = customer.Create(t.Context(), tx, name)
		return err //nolint:wrapcheck // dữ liệu thử
	}); err != nil {
		t.Fatalf("livetest: tạo khách %s: %v", name, err)
	}
	return id
}

// LiveSession chèn phiên live đang chạy của Page page với mã video video, bắt đầu lúc start; trả mã phiên.
func LiveSession(t testing.TB, s *testkit.Staff, tenant, page uuid.UUID, video string, start time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.live_sessions (tenant_id, channel_account_id,
			live_video_id, status, started_at, detected_by) VALUES ($1, $2, $3, 'live', $4, 'webhook') RETURNING id`,
			tenant, page, video, start.UTC()).Scan(&id)
	})
	return id
}

// Comment là một comment live của khách: mã tin, hội thoại comment.
type Comment struct {
	ID, Conversation uuid.UUID
}

// LiveComment chèn (chủ bảng) comment live mã ngoài ext, nội dung body, của khách customerID (mã người from) dưới bài
// video của Page page lúc at: bài (kind live), hội thoại comment (khóa luồng <video>:<from>, dùng lại khi đã có), tin.
func LiveComment(t testing.TB, s *testkit.Staff, tenant, page, customerID uuid.UUID, video, from, ext, body string,
	at time.Time,
) Comment {
	t.Helper()
	var c Comment
	ctx := t.Context()
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		var post uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind)
			VALUES ($1, $2, $3, 'live') ON CONFLICT (tenant_id, channel_account_id, external_id) DO UPDATE SET kind = 'live'
			RETURNING id`, tenant, page, video).Scan(&post); err != nil {
			return err //nolint:wrapcheck // dữ liệu thử
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id,
			kind, external_thread_ref, post_id, last_message_at, last_customer_message_at)
			VALUES ($1, $2, $3, 'comment', $4, $5, $6, $6)
			ON CONFLICT (tenant_id, channel_account_id, kind, external_thread_ref)
			DO UPDATE SET last_message_at = EXCLUDED.last_message_at RETURNING id`,
			tenant, page, customerID, video+":"+from, post, at.UTC()).Scan(&c.Conversation); err != nil {
			return err //nolint:wrapcheck // dữ liệu thử
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			external_id, direction, kind, origin, post_id, body, sender_name, is_live, sent_at)
			VALUES ($1, $2, $3, $4, 'in', 'comment', 'customer', $5, $6, 'Khách', true, $7) RETURNING id`,
			tenant, c.Conversation, page, ext, post, body, at.UTC()).Scan(&c.ID)
	})
	return c
}

// Keyring là bộ khóa mã hóa token Page của test (một khóa k1 cố định).
func Keyring(t testing.TB) *fieldcrypt.Keyring {
	t.Helper()
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))))
	if err != nil {
		t.Fatalf("livetest: bộ khóa: %v", err)
	}
	return kr
}

// EnableSend bật gửi thật (send_mode enabled) cho Page page và lưu token (page_user) bằng kr.
func EnableSend(t testing.TB, s *testkit.Staff, tenant, page uuid.UUID, kr *fieldcrypt.Keyring, token logging.Secret) {
	t.Helper()
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, page); err != nil {
			return err //nolint:wrapcheck // dữ liệu thử
		}
		return channel.StoreCredentials(t.Context(), tx, kr, page, token, "page_user")
	})
}

// Exec chạy câu sửa dữ liệu thử sql (chủ bảng) trong ngữ cảnh tenant; lỗi làm test dừng.
func Exec(t testing.TB, s *testkit.Staff, tenant uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := s.TryExec(t.Context(), tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), sql, args...)
		return err //nolint:wrapcheck // dữ liệu thử
	}); err != nil {
		t.Fatalf("livetest: sửa dữ liệu thử: %v", err)
	}
}
