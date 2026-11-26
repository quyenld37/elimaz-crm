package conversation_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// CH13 (đọc lịch sử): comment trên Page B có 2 dòng lịch sử (sửa, xóa) → người đọc Page B nhận 2 dòng đúng thứ tự,
// đúng nội dung cũ, cờ, nguồn; dòng không ghi cờ (đường tin nhắn) đọc là false. Người chỉ đọc Page A, tin không có,
// ghi chú nội bộ → ErrNotFound. Tin không có lịch sử → danh sách rỗng.
func TestListMessageRevisions(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	readerB := e.user("doc-b@elimaz.test", e.pageB.String(), "conversation.read")
	var msg, plain, note uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		ins := `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction,
			kind, origin, body, sent_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6, $7, now())
			RETURNING id`
		if err := tx.QueryRow(t.Context(), ins, e.convB, e.pageB, "c-1", "in", "comment", "customer", "Chốt 2 áo").Scan(&msg); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(t.Context(), ins, e.convB, e.pageB, "c-2", "in", "comment", "customer", "Còn không").Scan(&plain); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(t.Context(), ins, e.convB, e.pageB, nil, "out", "note", "staff", "ghi chú").Scan(&note); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.message_revisions (tenant_id, message_id, body, is_hidden,
			removed, source, recorded_at) VALUES
			(current_setting('app.tenant_id')::uuid, $1, 'Chốt 1 áo', false, false, 'customer', '2026-11-26 01:00Z'),
			(current_setting('app.tenant_id')::uuid, $1, 'Chốt 2 áo', false, true, 'customer', '2026-11-26 02:00Z'),
			(current_setting('app.tenant_id')::uuid, $2, 'cũ', NULL, NULL, 'customer', '2026-11-26 03:00Z')`, msg, note)
		return err //nolint:wrapcheck // test
	})
	var got []conversation.MessageRevision
	e.mustTx(func(tx pgx.Tx) (err error) {
		got, err = conversation.ListMessageRevisions(t.Context(), tx, readerB, msg)
		return err //nolint:wrapcheck // test
	})
	if len(got) != 2 || got[0].Body != "Chốt 1 áo" || got[0].Removed || got[0].IsHidden || got[0].Source != "customer" ||
		got[1].Body != "Chốt 2 áo" || !got[1].Removed || !got[0].RecordedAt.Before(got[1].RecordedAt) {
		t.Fatalf("lịch sử: %+v", got)
	}
	e.mustTx(func(tx pgx.Tx) (err error) {
		got, err = conversation.ListMessageRevisions(t.Context(), tx, readerB, plain)
		return err //nolint:wrapcheck // test
	})
	if got == nil || len(got) != 0 {
		t.Errorf("tin không có lịch sử: %+v", got)
	}
	for name, c := range map[string]struct {
		id uuid.UUID
		u  bool
	}{"Page khác": {msg, false}, "tin không có": {uuid.New(), true}, "ghi chú nội bộ": {note, true}} {
		u := e.viewer
		if c.u {
			u = readerB
		}
		err := e.tx(func(tx pgx.Tx) error {
			_, err := conversation.ListMessageRevisions(t.Context(), tx, u, c.id)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, conversation.ErrNotFound) {
			t.Errorf("%s: %v, mong ErrNotFound", name, err)
		}
	}
}
