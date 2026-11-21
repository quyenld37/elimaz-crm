package create_test

import (
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
)

// Quy luật (Q-5, "không rõ thì không gửi lại"): Execute chỉ gửi khi lần gửi hiện tại đang chờ kết quả; lệnh in_doubt
// không bao giờ làm vận đơn thành "chắc chưa gửi" hay bị hủy; chỉ hủy khi hãng từ chối hoặc chắc chưa tạo mà có ý định
// hủy; lệnh của lần gửi cũ, vận đơn đã qua creating không đổi gì; "Gửi lại" và xử lý "không rõ" không cùng lúc được.
func TestDecisionProperties(t *testing.T) {
	states := []string{
		create.StatePending, create.StateNotSent, create.StateUnknown, create.StateCreated, create.StateRejected,
		create.StateManual,
	}
	rapid.Check(t, func(t *rapid.T) {
		s := create.Shipment{
			Creating: rapid.Bool().Draw(t, "creating"),
			State:    rapid.SampledFrom(states).Draw(t, "state"),
			Current:  rapid.Bool().Draw(t, "current"),
		}
		job := rapid.SampledFrom([]string{create.JobInDoubt, create.JobDead, create.JobExpired, "done"}).Draw(t, "job")
		intent := rapid.SampledFrom([]string{"", "order_cancel", "replace"}).Draw(t, "intent")
		checkDecision(t, s, job, intent)
	})
}

func checkDecision(t *rapid.T, s create.Shipment, job, intent string) {
	gate := create.ExecuteGate(s)
	act := create.Terminal(s, job, intent)
	if gate == create.GateSend && (s.State != create.StatePending || !s.Creating || !s.Current) {
		t.Fatalf("gửi hãng khi %+v", s)
	}
	if (!s.Creating || !s.Current) && (act != create.ActNone || gate != create.GateStale) {
		t.Fatalf("lệnh cũ / vận đơn đã qua creating đổi vận đơn: %+v → %v, %v", s, act, gate)
	}
	if job == create.JobInDoubt && (act == create.ActNotSent || act == create.ActCancel) {
		t.Fatalf("in_doubt mà coi là chắc chưa gửi: %+v → %v", s, act)
	}
	if act == create.ActCancel && s.State != create.StateRejected && intent == "" {
		t.Fatalf("hủy khi hãng không từ chối và không có ý định hủy: %+v %s", s, job)
	}
	if act == create.ActCancel && s.State == create.StateUnknown && job == create.JobInDoubt {
		t.Fatalf("hủy vận đơn không rõ: %+v", s)
	}
	if create.CanResend(s.Creating, s.State) && create.CanResolve(s.Creating, s.State) {
		t.Fatalf("vừa gửi lại vừa xử lý không rõ: %+v", s)
	}
}
