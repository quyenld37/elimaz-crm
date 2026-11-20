package main

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// Sổ sự kiện dùng chung của api, lệnh vận chuyển và worker có consumer sổ COD (Task M1-32): worker có handler
// event.cash, nên shipment.status_changed ghi được việc cho cash. Ba module M2 (Task M2-02) được nối; order có lịch hủy
// đơn nháp quá hạn (Task M2-21) và consumer đơn trùng order_duplicates (Task M2-23); customer có consumer chỉ số khách
// customer_stats và lịch nhãn đêm customer.tag_rules_nightly (Task M2-25); fulfillment có consumer
// fulfillment_return_received (Task M3-23).
func TestNewModulesRegistersCash(t *testing.T) {
	t.Parallel()
	m, err := newModules(config.Config{Role: config.RoleWorker}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, h := range m.Events.JobHandlers() {
		kinds = append(kinds, h.Kind().Name)
	}
	want := []string{
		"event.fulfillment_return_received", "event.cash", "event.customer_stats", "event.order_duplicates",
	}
	if !slices.Equal(kinds, want) {
		t.Fatalf("handler của sổ sự kiện = %v, mong %v", kinds, want)
	}
	// Lịch theo tenant, mỗi lịch một handler cùng Kind (worker chạy được việc của lịch): customer.tag_rules_nightly
	// (Task M2-25), order.expire_drafts (Task M2-21).
	names := []string{"customer.tag_rules_nightly", "order.expire_drafts"}
	if len(m.Schedules) != len(names) || len(m.Handlers) != len(names) {
		t.Fatalf("việc nền, lịch của module: %d, %+v", len(m.Handlers), m.Schedules)
	}
	for i, s := range m.Schedules {
		if s.Name != names[i] || !s.PerTenant || m.Handlers[i].Kind().Name != s.Kind.Name {
			t.Errorf("lịch %d: %+v, handler %s", i, s, m.Handlers[i].Kind().Name)
		}
	}
}
