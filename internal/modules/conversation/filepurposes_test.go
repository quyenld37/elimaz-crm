package conversation_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/migrations"
)

// tablePurposes là mọi mục đích có dòng trong platform.file_purposes sau migration của nhánh này.
func tablePurposes() []files.Purpose {
	ps := append(files.M1Purposes(), customer.ImportPurpose(), fulfillment.LabelPurpose(), cash.StatementPurpose(),
		order.RequestPhotoPurpose(), content.BannerPurpose(), conversation.OutgoingImageFilePurpose())
	return append(ps, conversation.InboundFilePurposes()...)
}

// migrationUp là phần Up của migration name.
func migrationUp(t *testing.T, name string) string {
	t.Helper()
	raw, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatal(err)
	}
	_, up, ok := strings.Cut(string(raw), "-- +goose Up")
	if !ok {
		t.Fatalf("%s không có phần Up", name)
	}
	return up
}

// Mục đích tệp của tin vào (R5, R10): sổ Go (InboundFilePurposes) khớp bảng sau 0404 (chat_image chế độ private,
// chat_attachment, chat_avatar standard, cả ba bucket private); chạy lại 0404 không lỗi, vẫn ba dòng; bảng lệch (đổi
// mode của chat_image, thiếu dòng chat_attachment) → Check đỏ nêu đúng khóa; khóa lạ → ErrUnknownPurpose, chèn thẳng
// dòng tệp có khóa lạ → khóa ngoại 23503; chat_image không ghi được bằng Register (ErrPurposeMode, ảnh phải qua imager).
func TestFilePurposes_MatchTable(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	conversation.RegisterFilePurposes()
	reg := files.NewRegistry(tablePurposes()...)
	if err := reg.Check(ctx, s.App); err != nil {
		t.Fatalf("sổ khớp bảng sau 0404: %v", err)
	}
	want := map[string][3]string{
		conversation.PurposeChatImage:      {"private", "private"},
		conversation.PurposeChatAttachment: {"private", "standard"},
		conversation.PurposeChatAvatar:     {"private", "standard"},
	}
	for k, w := range want {
		p, ok := files.PurposeOf(k)
		if !ok || string(p.Bucket) != w[0] {
			t.Fatalf("%s: sổ của tiến trình = %+v, %v", k, p, ok)
		}
	}
	for range 2 {
		if _, err := s.Owner.Pool.Exec(ctx, migrationUp(t, "0404_files_chat_purposes.sql")); err != nil {
			t.Fatalf("chạy lại 0404: %v", err)
		}
	}
	if n := s.Count(t, `SELECT count(*) FROM platform.file_purposes WHERE key LIKE 'chat\_%' AND key <> 'chat_outgoing'`); n != 3 {
		t.Fatalf("sau hai lần chạy lại: %d dòng chat_*", n)
	}

	checkPurposeDrift(t, s, reg)

	tenant := s.Tenant(t, "A")
	register := func(purpose, ct string) error {
		id := uuid.Must(uuid.NewV7())
		var err error
		s.Exec(t, tenant, func(tx pgx.Tx) error {
			_, err = files.Register(ctx, tx, files.NewFile{
				ID: id, Purpose: purpose, Key: files.ServerKey(tenant, id), Size: 10, ContentType: ct, FileName: "t.bin",
			})
			return nil
		})
		return err //nolint:wrapcheck // test
	}
	if err := register("chat_attachmentx", "application/pdf"); !errors.Is(err, files.ErrUnknownPurpose) {
		t.Fatalf("khóa lạ: %v", err)
	}
	if err := register(conversation.PurposeChatImage, "image/jpeg"); !errors.Is(err, files.ErrPurposeMode) {
		t.Fatalf("chat_image qua Register: %v", err)
	}
	for _, ct := range []string{"application/pdf", "text/html; charset=utf-8", "image/gif"} {
		if err := register(conversation.PurposeChatAttachment, ct); err != nil {
			t.Fatalf("chat_attachment %s: %v", ct, err)
		}
	}
	err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO platform.files (tenant_id, purpose, bucket, key, original_name, declared_type,
			size_bytes, status) VALUES (current_setting('app.tenant_id')::uuid, 'chat_attachmentx', 'private', $1, 'a',
			'application/pdf', 1, 'ready')`, "priv/"+tenant.String()+"/"+uuid.NewString())
		return err //nolint:wrapcheck // test
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); !ok || pe.Code != "23503" {
		t.Fatalf("chèn thẳng khóa lạ: muốn 23503, nhận %v", err)
	}
}

// checkPurposeDrift: bảng lệch sổ (đổi mode của chat_image, thiếu dòng chat_attachment) → Check đỏ nêu đúng khóa;
// chạy lại 0404 trả bảng về như cũ.
func checkPurposeDrift(t *testing.T, s *testkit.Staff, reg *files.Registry) {
	t.Helper()
	ctx := t.Context()
	exec := func(q string) {
		t.Helper()
		if _, err := s.Owner.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE platform.file_purposes SET mode = 'standard' WHERE key = 'chat_image'`)
	if err := reg.Check(ctx, s.App); !errors.Is(err, files.ErrPurposeMismatch) || !strings.Contains(err.Error(), "chat_image") {
		t.Fatalf("mode chat_image lệch: %v", err)
	}
	exec(`UPDATE platform.file_purposes SET mode = 'private' WHERE key = 'chat_image'`)
	exec(`DELETE FROM platform.file_purposes WHERE key = 'chat_attachment'`)
	if err := reg.Check(ctx, s.App); !errors.Is(err, files.ErrPurposeMismatch) || !strings.Contains(err.Error(), "chat_attachment") {
		t.Fatalf("thiếu dòng chat_attachment: %v", err)
	}
	exec(migrationUp(t, "0404_files_chat_purposes.sql"))
}
