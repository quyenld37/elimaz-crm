package idempotency_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

var errHandler = errors.New("handler lỗi giữa transaction")

// propRun là một lượt của test thuộc tính: một tenant riêng, 1–5 khóa, đồng hồ giả.
type propRun struct {
	t      *testing.T
	e      *env
	tenant uuid.UUID
	scope  idempotency.Scope
	now    time.Time
	final  map[string]bool // khóa đã có kết quả cuối
}

// Quy luật (ADR-41, vùng rủi ro cao): với một chuỗi ngẫu nhiên các lần gửi, chết giữa chừng, "thử lại", lỗi giữa
// transaction và gửi lại của 1–5 khóa (đồng hồ tiến ngẫu nhiên), số lần bộ đếm tăng bằng đúng số khóa có kết quả
// cuối, mỗi khóa tăng tối đa một lần, và khóa có kết quả cuối luôn phát lại đúng kết quả đó.
func TestPropertyCounterMatchesFinalKeys(t *testing.T) {
	e := newEnv(t)
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		p := &propRun{t: t, e: e, tenant: e.staff.Tenant(t, fmt.Sprintf("P%d", n)), now: t0, final: map[string]bool{}}
		p.scope = scope(p.tenant, "user:1")
		nKeys := rapid.IntRange(1, 5).Draw(rt, "keys")
		for range rapid.IntRange(1, 30).Draw(rt, "steps") {
			key := fmt.Sprintf("k%d", rapid.IntRange(0, nKeys-1).Draw(rt, "key"))
			p.now = p.now.Add(time.Duration(rapid.IntRange(0, 90).Draw(rt, "advance_s")) * time.Second)
			p.step(rt, key, rapid.SampledFrom([]string{"ok", "crash", "retry", "fail"}).Draw(rt, "action"))
		}
		p.check(rt, nKeys)
	})
}

// step gửi một yêu cầu cho key rồi làm action nếu được giữ khóa.
func (p *propRun) step(rt *rapid.T, key, action string) {
	c, st, err := idempotency.Acquire(p.t.Context(), p.e.staff.App, p.scope, key, hashOf(`{}`), p.now)
	switch {
	case errors.Is(err, idempotency.ErrInProgress):
		return
	case err != nil:
		rt.Fatalf("Acquire %s: %v", key, err)
	case st != nil:
		if !p.final[key] || st.Status != 200 {
			rt.Fatalf("%s phát lại %+v mà chưa có kết quả cuối", key, st)
		}
		return
	case p.final[key]:
		rt.Fatalf("%s đã có kết quả cuối mà Acquire lại trả Claim", key)
	}
	switch action {
	case "ok":
		if err := p.e.bump(p.t.Context(), p.tenant, key, c, 200, []byte(`{"ok":true}`)); err != nil {
			rt.Fatalf("bump %s: %v", key, err)
		}
		p.final[key] = true
	case "crash": // tiến trình chết sau bước ghi nhận khóa: không làm gì nữa
	case "retry": // câu trả lời "thử lại": xóa khóa
		if err := c.Release(p.t.Context()); err != nil {
			rt.Fatalf("Release %s: %v", key, err)
		}
	case "fail": // lỗi giữa transaction: bộ đếm lùi, dòng khóa giữ running
		if err := p.failMidway(key); !errors.Is(err, errHandler) {
			rt.Fatalf("fail %s: %v", key, err)
		}
	}
}

func (p *propRun) failMidway(key string) error {
	ctx := p.t.Context()
	return tenancy.WithTenant(ctx, p.e.staff.App, p.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		if _, err := tx.Exec(ctx, `INSERT INTO probe.counter (tenant_id, key, n) VALUES ($1, $2, 1)
			ON CONFLICT (tenant_id, key) DO UPDATE SET n = probe.counter.n + 1`, p.tenant, key); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errHandler
	})
}

// check: mỗi khóa có kết quả cuối tăng đúng 1, khóa khác 0; tổng bằng số khóa có kết quả cuối.
func (p *propRun) check(rt *rapid.T, nKeys int) {
	total := 0
	for k := range nKeys {
		key := fmt.Sprintf("k%d", k)
		got, want := p.e.count(p.t, p.tenant, key), 0
		if p.final[key] {
			want = 1
		}
		if got != want {
			rt.Fatalf("%s: bộ đếm %d, muốn %d", key, got, want)
		}
		total += got
	}
	if total != len(p.final) {
		rt.Fatalf("tổng bộ đếm %d khác số khóa có kết quả cuối %d", total, len(p.final))
	}
}
