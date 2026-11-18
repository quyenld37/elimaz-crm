package main

import "testing"

// Sổ sự kiện dùng chung của api, lệnh vận chuyển và worker có consumer sổ COD (Task M1-32): worker có handler
// event.cash, nên shipment.status_changed ghi được việc cho cash.
func TestNewEventsRegistersCash(t *testing.T) {
	t.Parallel()
	r, err := newEvents()
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, h := range r.JobHandlers() {
		kinds = append(kinds, h.Kind().Name)
	}
	if len(kinds) != 1 || kinds[0] != "event.cash" {
		t.Fatalf("handler của sổ sự kiện = %v, mong [event.cash]", kinds)
	}
}
