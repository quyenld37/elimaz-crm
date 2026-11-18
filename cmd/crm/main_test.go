package main

import (
	"slices"
	"testing"
)

// Sổ sự kiện dùng chung của api, lệnh vận chuyển và worker có consumer sổ COD (Task M1-32): worker có handler
// event.cash, nên shipment.status_changed ghi được việc cho cash. Ba module M2 (Task M2-02) được nối nhưng chưa có
// consumer, việc nền, lịch nào.
func TestNewModulesRegistersCash(t *testing.T) {
	t.Parallel()
	m, err := newModules(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, h := range m.Events.JobHandlers() {
		kinds = append(kinds, h.Kind().Name)
	}
	if !slices.Equal(kinds, []string{"event.cash"}) {
		t.Fatalf("handler của sổ sự kiện = %v, mong [event.cash]", kinds)
	}
	if len(m.Handlers) != 0 || len(m.Schedules) != 0 {
		t.Fatalf("Task M2-02 chưa có việc nền, lịch: %d, %d", len(m.Handlers), len(m.Schedules))
	}
}
