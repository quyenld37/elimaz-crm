package conversation_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Không có bộ adapter: không phần chạy nền nào. Có bộ adapter: mỗi adapter đúng một handler vào cùng khóa (Task 153);
// lệnh, việc nền, lịch, luồng nền chưa có (task sau thêm).
func TestWiring(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.JobHandlers(d)) +
		len(conversation.Schedules(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("không bộ adapter mà có %d phần chạy nền", n)
	}
	reg, err := channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	hs := conversation.InboundHandlers(d)
	if len(hs) != 1 || hs[0].Source() != fake.Key {
		t.Errorf("handler vào: %d, mong 1 của %s", len(hs), fake.Key)
	}
	if n := len(conversation.CommandHandlers(d)) + len(conversation.JobHandlers(d)) + len(conversation.Schedules(d)) +
		len(conversation.Background(d)); n != 0 {
		t.Errorf("phần chạy nền khác: %d, mong 0", n)
	}
}
