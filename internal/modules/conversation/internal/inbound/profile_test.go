package inbound_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
)

// profileOf đọc tên, nguồn tên của khách và ảnh đại diện của hội thoại có tin mid.
func (e *fileEnv) profileOf(mid string) (name, source string, avatar *uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT k.full_name, k.name_source, c.avatar_file_id
			FROM conversation.messages m JOIN conversation.conversations c ON c.id = m.conversation_id
			JOIN customer.customers k ON k.id = c.customer_id WHERE m.external_id = $1`, mid).Scan(&name, &source, &avatar)
	})
	return name, source, avatar
}

// CH08: khách mới từ tin vào → một lệnh đọc hồ sơ (payload không có PSID); tên "Lan Nguyễn" từ kênh, name_source
// channel; ảnh đại diện → conversations.avatar_file_id, tệp chat_avatar ready ở bucket private. Kênh báo thiếu quyền
// đọc hồ sơ → tên tạm giữ nguyên, lệnh dead, không thêm việc nào khác (không cảnh báo gấp). Ảnh đại diện không tải
// được → vẫn đặt tên, không ảnh.
func TestFetchProfile_CH08(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	avatar := e.chat.AddFile("/a/u1.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...))
	e.chat.SetProfile("u1", channel.Profile{Name: " Lan Nguyễn ", AvatarURL: avatar})
	e.receive(e.tenant, "u1", "m-1")
	name, src, av := e.profileOf("m-1")
	if name != "Lan Nguyễn" || src != "channel" || av == nil {
		t.Fatalf("hồ sơ: %q %q %v", name, src, av)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files WHERE id = $1 AND purpose = $2
		AND status = 'ready' AND bucket = 'private'`, *av, conversation.PurposeChatAvatar); n != 1 {
		t.Fatal("tệp ảnh đại diện")
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.fb.fetch_profile'
		AND status = 'done' AND payload::text NOT LIKE '%u1%' AND coalesce(result::text, '') NOT LIKE '%Lan%'`); n != 1 {
		t.Fatalf("lệnh hồ sơ done không mang PSID trong payload, tên trong kết quả: %d", n)
	}
	// Tin sau của cùng khách: không lệnh hồ sơ mới.
	e.receive(e.tenant, "u1", "m-1b")
	if n := e.jobs("command.fb.fetch_profile", ""); n != 1 {
		t.Fatalf("lệnh hồ sơ = %d", n)
	}

	e.chat.SetProfile("u2", channel.Profile{Name: "Hoa", AvatarURL: "https://evil.example/a.png"})
	e.receive(e.tenant, "u2", "m-2")
	if name, _, av := e.profileOf("m-2"); name != "Hoa" || av != nil {
		t.Fatalf("ảnh đại diện lỗi vẫn phải đặt tên: %q %v", name, av)
	}

	e.chat.FailProfile("u3", fake.ErrPermanent)
	jobsBefore := e.staff.Count(t, `SELECT count(*) FROM platform.jobs`)
	e.receive(e.tenant, "u3", "m-3")
	if name, src, _ := e.profileOf("m-3"); name != "Khách Facebook" || src != "placeholder" {
		t.Fatalf("thiếu quyền: tên %q %q phải giữ tên tạm", name, src)
	}
	if n := e.jobs("command.fb.fetch_profile", "dead"); n != 1 {
		t.Fatalf("lệnh thiếu quyền dead = %d", n)
	}
	// Việc mới chỉ là việc vào và lệnh hồ sơ của tin này (không cảnh báo, không lệnh khác).
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs`) - jobsBefore; n != 2 {
		t.Fatalf("%d việc mới, muốn 2 (việc vào + lệnh hồ sơ)", n)
	}
}
