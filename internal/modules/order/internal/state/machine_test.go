package state_test

import (
	"errors"
	"fmt"
	"testing"

	"elimaz.com.vn/crm/internal/modules/order/internal/state"
)

func allStatuses() []state.Status {
	return []state.Status{
		state.Draft, state.New, state.Confirmed, state.CancelRequested, state.Cancelled, state.Delivered, state.Returned,
	}
}

// allConditions: 16 tổ hợp điều kiện (HasCurrentShipment × AddressComplete × 4 lý do chờ).
func allConditions() []state.Conditions {
	var out []state.Conditions
	for _, ship := range []bool{false, true} {
		for _, addr := range []bool{false, true} {
			for _, p := range []string{state.PendingNone, state.PendingAwaitingCustomer, state.PendingOutOfStock, state.PendingDiscountApproval} {
				out = append(out, state.Conditions{HasCurrentShipment: ship, AddressComplete: addr, PendingReason: p})
			}
		}
	}
	return out
}

func TestStatusValues(t *testing.T) {
	t.Parallel()
	want := []string{"draft", "new", "confirmed", "cancel_requested", "cancelled", "delivered", "returned"}
	for i, s := range allStatuses() {
		if string(s) != want[i] {
			t.Fatalf("hằng thứ %d = %q, muốn %q (CHECK của ord.orders)", i, s, want[i])
		}
	}
}

// 49 cặp: đúng 12 cặp đi được (10 cạnh của sơ đồ đơn 05 mục 2, new → new và draft → draft chỉ đổi lý do chờ — Task
// M2-19 duyệt giảm giá đơn nháp) với ít nhất một tổ hợp điều kiện; 37 cặp còn lại trả ErrInvalidTransition với mọi tổ
// hợp (Q-T65-1).
func TestCheck_AllPairs(t *testing.T) {
	t.Parallel()
	valid := map[[2]state.Status]bool{
		{state.Draft, state.Confirmed}:           true,
		{state.Draft, state.Cancelled}:           true,
		{state.New, state.Confirmed}:             true,
		{state.New, state.Cancelled}:             true,
		{state.New, state.New}:                   true,
		{state.Draft, state.Draft}:               true,
		{state.Confirmed, state.Cancelled}:       true,
		{state.Confirmed, state.CancelRequested}: true,
		{state.CancelRequested, state.Cancelled}: true,
		{state.CancelRequested, state.Confirmed}: true,
		{state.Confirmed, state.Delivered}:       true,
		{state.Confirmed, state.Returned}:        true,
	}
	nValid := 0
	for _, from := range allStatuses() {
		for _, to := range allStatuses() {
			pair := [2]state.Status{from, to}
			if checkPair(t, from, to, valid[pair]) {
				nValid++
			}
		}
	}
	if nValid != 12 {
		t.Fatalf("số cặp đi được = %d, muốn 12", nValid)
	}
}

// checkPair thử from → to với mọi tổ hợp điều kiện, trả có tổ hợp nào cho qua không; cặp không hợp lệ phải luôn trả
// ErrInvalidTransition.
func checkPair(t *testing.T, from, to state.Status, valid bool) bool {
	t.Helper()
	ok := false
	for _, c := range allConditions() {
		err := state.Check(from, to, c)
		switch {
		case err == nil:
			ok = true
		case !valid && !errors.Is(err, state.ErrInvalidTransition):
			t.Errorf("Check(%s → %s, %+v) = %v, muốn ErrInvalidTransition", from, to, c, err)
		}
	}
	if ok != valid {
		t.Errorf("%s → %s: đi được = %v, muốn %v", from, to, ok, valid)
	}
	return ok
}

func TestCheck_Conditions(t *testing.T) {
	t.Parallel()
	full := state.Conditions{AddressComplete: true}
	cases := []struct {
		from, to state.Status
		c        state.Conditions
		want     error
	}{
		{state.Draft, state.New, full, state.ErrInvalidTransition},
		{state.Draft, state.Confirmed, full, nil},
		{state.Draft, state.Confirmed, state.Conditions{}, state.ErrAddressIncomplete},
		{state.Draft, state.Confirmed, state.Conditions{PendingReason: state.PendingDiscountApproval}, state.ErrAddressIncomplete},
		{state.Draft, state.Confirmed, state.Conditions{AddressComplete: true, PendingReason: state.PendingDiscountApproval}, state.ErrNeedsApproval},
		{state.Draft, state.Confirmed, state.Conditions{AddressComplete: true, PendingReason: state.PendingAwaitingCustomer}, state.ErrInvalidTransition},
		{state.Draft, state.Confirmed, state.Conditions{AddressComplete: true, PendingReason: state.PendingOutOfStock}, state.ErrInvalidTransition},
		{state.Draft, state.Cancelled, state.Conditions{}, nil},
		{state.New, state.Confirmed, state.Conditions{}, nil},
		{state.New, state.Confirmed, state.Conditions{PendingReason: state.PendingAwaitingCustomer}, nil},
		{state.New, state.Confirmed, state.Conditions{PendingReason: state.PendingOutOfStock}, nil},
		{state.New, state.Confirmed, state.Conditions{AddressComplete: true, PendingReason: state.PendingDiscountApproval}, state.ErrNeedsApproval},
		{state.New, state.New, state.Conditions{PendingReason: state.PendingDiscountApproval}, nil},
		{state.Draft, state.Draft, state.Conditions{PendingReason: state.PendingDiscountApproval}, nil},
		{state.New, state.Cancelled, state.Conditions{PendingReason: state.PendingDiscountApproval}, nil},
		{state.Confirmed, state.Cancelled, state.Conditions{}, nil},
		{state.Confirmed, state.Cancelled, state.Conditions{HasCurrentShipment: true}, state.ErrInvalidTransition},
		{state.Confirmed, state.CancelRequested, state.Conditions{HasCurrentShipment: true}, nil},
		{state.Confirmed, state.CancelRequested, state.Conditions{}, state.ErrInvalidTransition},
		{state.CancelRequested, state.Cancelled, state.Conditions{HasCurrentShipment: true}, nil},
		{state.CancelRequested, state.Confirmed, state.Conditions{HasCurrentShipment: true}, nil},
		{state.Confirmed, state.Delivered, state.Conditions{HasCurrentShipment: true}, nil},
		{state.Confirmed, state.Returned, state.Conditions{HasCurrentShipment: true}, nil},
		{state.Confirmed, state.Confirmed, state.Conditions{}, state.ErrInvalidTransition},
		{state.Delivered, state.Returned, state.Conditions{}, state.ErrInvalidTransition},
		{state.Cancelled, state.New, state.Conditions{}, state.ErrInvalidTransition},
		{state.Status("shipping"), state.Delivered, state.Conditions{}, state.ErrInvalidTransition},
		{state.Confirmed, state.Status(""), state.Conditions{}, state.ErrInvalidTransition},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s→%s/%+v", c.from, c.to, c.c), func(t *testing.T) {
			t.Parallel()
			err := state.Check(c.from, c.to, c.c)
			if c.want == nil {
				if err != nil {
					t.Fatalf("lỗi %v, muốn nil", err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("lỗi %v, muốn %v", err, c.want)
			}
		})
	}
}
