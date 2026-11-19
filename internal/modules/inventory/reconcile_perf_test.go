//go:build perf

// Test thời gian của đối soát sổ kho (make perf-test). Tách khỏi test đúng/sai (Task 421, như Task 400): thời gian phụ
// thuộc máy bận hay rảnh, nên chỉ chạy riêng trên máy rảnh; TestReconcile_LargeTenant giữ các tiêu chí không phụ thuộc
// tải.
package inventory_test

import (
	"testing"
	"time"
)

// reconcileBudget là trần thời gian đối soát 50.000 số dư.
const reconcileBudget = 60 * time.Second

// 50.000 số dư (5.000 biến thể × 10 kho) → đối soát xong dưới 60 s, mỗi câu dưới statement_timeout 30 s, đếm đủ,
// không lệch.
func TestReconcile_LargeTenantTimeBudget(t *testing.T) {
	e := newInvEnv(t)
	e.fillLargeTenant()
	start := time.Now()
	run := e.reconcile(0)
	took := time.Since(start)
	t.Logf("50.000 số dư: %v", took)
	if !run.OK || run.Checked != 50000 || took > reconcileBudget {
		t.Fatalf("ok %v, checked %d, %v (trần %v)", run.OK, run.Checked, took, reconcileBudget)
	}
}
