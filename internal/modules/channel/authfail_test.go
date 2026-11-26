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

// ReportAuthFailure (Task 156): tài khoản active → degraded lý do token_invalid kèm đúng 1 lệnh cảnh báo gấp; báo lần
// nữa (đã degraded vì token) → không thêm cảnh báo; tài khoản không có → ErrAccountNotFound. Lệnh cảnh báo gửi key
// channel-auth:<id>, mức urgent, có tên Page, không token.
func TestReportAuthFailure(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	id := s.ChannelAccount(t, tenant, "facebook", "page-auth", "Page Auth")
	report := func(acc uuid.UUID, ref string) error {
		var err error
		s.Exec(t, tenant, func(tx pgx.Tx) error {
			err = channel.ReportAuthFailure(t.Context(), tx, acc, ref) // lỗi trả về ngoài, không lùi tx
			return nil
		})
		return err //nolint:wrapcheck // test
	}
	for i, ref := range []string{"job-1", "job-2"} {
		if err := report(id, ref); err != nil {
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
	if err := report(uuid.New(), "x"); !errors.Is(err, channel.ErrAccountNotFound) {
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
