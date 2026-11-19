package main

import (
	"slices"
	"testing"
)

// Sổ sự kiện dùng chung của api, lệnh vận chuyển và worker có consumer sổ COD (Task M1-32): worker có handler
// event.cash, nên shipment.status_changed ghi được việc cho cash. Ba module M2 (Task M2-02) được nối; order có lịch hủy
// đơn nháp quá hạn (Task M2-21) và consumer đơn trùng order_duplicates (Task M2-23).
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
	if !slices.Equal(kinds, []string{"event.cash", "event.order_duplicates"}) {
		t.Fatalf("handler của sổ sự kiện = %v, mong [event.cash event.order_duplicates]", kinds)
	}
	// Task M2-21: lịch order.expire_drafts theo tenant và handler cùng Kind (worker chạy được việc của lịch).
	if len(m.Schedules) != 1 || m.Schedules[0].Name != "order.expire_drafts" || !m.Schedules[0].PerTenant ||
		len(m.Handlers) != 1 || m.Handlers[0].Kind().Name != m.Schedules[0].Kind.Name {
		t.Fatalf("việc nền, lịch của module: %d, %+v", len(m.Handlers), m.Schedules)
	}
}
