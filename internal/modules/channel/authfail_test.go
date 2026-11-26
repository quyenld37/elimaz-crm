package channel_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// ReportTokenInvalid (Task 156, gộp với đường kiểm sức khỏe của Task 150): tài khoản active → degraded lý do
// token_invalid kèm đúng 1 lệnh cảnh báo gấp; báo lần nữa (đã degraded vì mất quyền) → không thêm cảnh báo; Page đã
// ngắt → giữ disconnected, không cảnh báo; tài khoản không có → ErrAccountNotFound. Lệnh cảnh báo gửi key
// channel-auth:<id>, mức urgent, có tên Page, không token.
func TestReportTokenInvalid(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	id := s.ChannelAccount(t, tenant, "facebook", "page-auth", "Page Auth")
	report := func(acc uuid.UUID) error {
		var err error
		s.Exec(t, tenant, func(tx pgx.Tx) error {
			err = channel.Writer{}.ReportTokenInvalid(t.Context(), tx, acc) // lỗi trả về ngoài, không lùi tx
			return nil
		})
		return err //nolint:wrapcheck // test
	}
	for i := range 2 {
		if err := report(id); err != nil {
			t.Fatalf("lần %d: %v", i+1, err)
		}
	}
	if n := s.CountIn(t, tenant, `SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND status = 'degraded'
		AND status_reason = 'token_invalid'`, id); n != 1 {
		t.Fatal("tài khoản chưa degraded token_invalid")
	}
	var alerts int
	if err := s.Owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
		channel.AuthAlertKind).Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("%d lệnh cảnh báo, muốn 1 (%v)", alerts, err)
	}
	gone := s.ChannelAccount(t, tenant, "facebook", "page-gone", "Page Gone")
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := channel.MarkStatus(t.Context(), tx, gone, channel.StatusDisconnected, "")
		return err //nolint:wrapcheck // test
	})
	if err := report(gone); err != nil {
		t.Fatalf("Page đã ngắt: %v", err)
	}
	if n := s.CountIn(t, tenant, `SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND status = 'disconnected'`,
		gone); n != 1 {
		t.Fatal("Page đã ngắt bị đổi trạng thái")
	}
	if err := s.Owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
		channel.AuthAlertKind).Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("Page đã ngắt: %d lệnh cảnh báo, muốn 1 (%v)", alerts, err)
	}
	if err := report(uuid.New()); !errors.Is(err, channel.ErrAccountNotFound) {
		t.Fatalf("tài khoản không có: %v", err)
	}

	n := &recNotifier{}
	h := channel.NewAuthAlertCommand(n, nil)
	payload := []byte(`{"accountId":"` + id.String() + `","channel":"facebook","name":"Page Auth"}`)
	out, err := h.Execute(t.Context(), queue.Job{Payload: payload})
	if err != nil || out.Status != commands.StatusDone || h.Kind().Name != channel.AuthAlertKind ||
		n.key != "channel-auth:"+id.String() || n.sev != "urgent" || !strings.Contains(n.text, "Page Auth") {
		t.Fatalf("cảnh báo: %+v %v %+v", out, err, n)
	}
}
