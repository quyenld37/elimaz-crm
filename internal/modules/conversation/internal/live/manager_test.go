package live_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
)

// LV10: worker 1 đọc 100 comment rồi chết (hủy luồng, không nhả khóa) → worker 2 nhận phiên trong ≤ LeaseTTL +
// ScanEvery (test thu nhỏ: 1,5 s), Run của worker 2 có since = last_comment_at − 5 s; nguồn có tổng 200 comment →
// đúng 200 việc comment khác nhau, received_count = 200 (comment trùng do hỏi bù không đếm).
func TestLV10_CrashedWorkerReopensWithin15s(t *testing.T) {
	t.Parallel()
	e, s, src, base := newSessionEnv(t, 200)
	src.setAvail(100)
	o1, o2 := scaled("w1"), scaled("w2")
	m1 := live.NewManager(e.d, src, o1)
	stop1 := runManager(t, m1)
	eventually(t, 5*time.Second, "worker 1 đọc 100 comment", func() bool {
		_, _, n, _ := e.session(s.ID)
		return n == 100
	})
	// Worker 2 chạy song song nhưng không nhận được phiên worker 1 đang giữ.
	m2 := live.NewManager(e.d, src, o2)
	runManager(t, m2)
	time.Sleep(2 * o2.ScanEvery)
	if runs := src.runsOf(s.ID); len(runs) != 1 {
		t.Fatalf("khi worker 1 còn sống: %d lần Run, muốn 1", len(runs))
	}
	m1.Crash()
	stop1()
	crashed := time.Now()
	if _, owner, _, _ := e.session(s.ID); owner != "w1" {
		t.Fatalf("worker chết không được nhả khóa: chủ = %q", owner)
	}
	src.setAvail(200)
	eventually(t, 5*time.Second, "worker 2 nhận phiên", func() bool { return len(src.runsOf(s.ID)) == 2 })
	r2 := src.runsOf(s.ID)[1]
	// Đo bằng đồng hồ của máy test; cộng 300 ms cho độ trễ database lúc chạy -race.
	if limit := live.MaxReopenDelay(o2.LeaseTTL, o2.ScanEvery); r2.at.Sub(crashed) > limit+300*time.Millisecond {
		t.Errorf("worker 2 nhận sau %v, muốn ≤ %v", r2.at.Sub(crashed), limit)
	}
	lastOf100 := base.Add(99 * 100 * time.Millisecond)
	if want := lastOf100.Add(-live.Overlap); !r2.since.Equal(want) {
		t.Errorf("since của worker 2 = %v, muốn last_comment_at − 5 s = %v", r2.since, want)
	}
	eventually(t, 5*time.Second, "đủ 200 comment", func() bool {
		_, _, n, _ := e.session(s.ID)
		return n == 200
	})
	if n := e.distinctJobs(); n != 200 {
		t.Errorf("%d việc comment khác nhau, muốn 200", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fakechat'`); n != 200 {
		t.Errorf("%d việc comment, muốn 200 (không trùng)", n)
	}
	if _, owner, _, _ := e.session(s.ID); owner != "w2" {
		t.Errorf("chủ khóa sau khi mở lại = %q", owner)
	}
	if src.overlapped() {
		t.Error("hai Run của cùng phiên chạy chồng nhau")
	}
}

// Tắt êm worker 1 → khóa nhả ngay; worker 2 nhận trong ≤ ScanEvery.
func TestGracefulStopReleasesLease(t *testing.T) {
	t.Parallel()
	e, s, src, _ := newSessionEnv(t, 0)
	o1, o2 := scaled("w1"), scaled("w2")
	stop1 := runManager(t, live.NewManager(e.d, src, o1))
	eventually(t, 5*time.Second, "worker 1 nhận phiên", func() bool { return len(src.runsOf(s.ID)) == 1 })
	runManager(t, live.NewManager(e.d, src, o2))
	stop1()
	stopped := time.Now()
	if _, owner, _, _ := e.session(s.ID); owner != "" && owner != "w2" {
		t.Fatalf("tắt êm chưa nhả khóa: chủ = %q", owner)
	}
	eventually(t, 5*time.Second, "worker 2 nhận phiên", func() bool { return len(src.runsOf(s.ID)) == 2 })
	if d := src.runsOf(s.ID)[1].at.Sub(stopped); d > o2.ScanEvery+300*time.Millisecond {
		t.Errorf("worker 2 nhận sau %v, muốn ≤ ScanEvery %v", d, o2.ScanEvery)
	}
}

// 04 mục 18: hai worker quét song song khoảng 50 lần trên ba phiên → không lúc nào hai Run của cùng phiên chồng nhau;
// mỗi phiên đúng một chủ; mất khóa (ép hết hạn) thì luồng cũ dừng trước khi luồng mới đọc ghi được.
//
// Task 497: câu kiểm an toàn (không chồng nhau) không phụ thuộc giờ; câu kiểm "mỗi phiên đúng một luồng" chờ theo điều
// kiện (eventually, trần 10 s chỉ chống treo) thay vì chụp đúng lúc sau 2,5 s. Khóa 2 s, gia hạn 100 ms: hạn cục bộ
// (LeaseTTL − RenewEvery/2 = 1,95 s) chịu được một lần gia hạn chậm gần 2 s khi máy bận; bản cũ (300 ms, 80 ms) mất khóa
// khi một lần gia hạn chậm hơn 260 ms, luồng mở lại và lúc chụp có phiên 0 luồng (đỏ 1/5 trên máy, một lần trên GitHub).
func TestTwoWorkersNeverReadSameSession(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	base := time.Date(2026, 11, 26, 12, 15, 0, 0, time.UTC)
	var ids []uuid.UUID
	for _, v := range []string{"lv1", "lv2", "lv3"} {
		ids = append(ids, e.observe(v, live.StatusLive, live.DetectedWebhook, base).ID)
	}
	src := newFakeSource(0, base)
	fast := func(owner string) live.Options {
		return live.Options{
			Owner: owner, LeaseTTL: 2 * time.Second, RenewEvery: 100 * time.Millisecond,
			ScanEvery: 20 * time.Millisecond, Overlap: live.Overlap,
		}
	}
	m1 := live.NewManager(e.d, src, fast("w1"))
	m2 := live.NewManager(e.d, src, fast("w2"))
	runManager(t, m1)
	runManager(t, m2)
	oneOwnerEach := func() bool {
		for _, id := range ids {
			if src.runningOf(id) != 1 {
				return false
			}
			if _, owner, _, _ := e.session(id); owner != "w1" && owner != "w2" {
				return false
			}
		}
		return true
	}
	eventually(t, 10*time.Second, "mỗi phiên đúng một luồng của w1 hay w2", oneOwnerEach)
	time.Sleep(50 * 20 * time.Millisecond) // cửa sổ quan sát: khoảng 50 lần quét của mỗi worker trong lúc phiên đang có chủ
	eventually(t, 10*time.Second, "sau 50 lần quét, mỗi phiên vẫn đúng một luồng", oneOwnerEach)
	if src.overlapped() {
		t.Fatal("hai Run của cùng phiên chạy chồng nhau")
	}
	// Ép mất khóa: chủ khác giữ phiên đầu → luồng đang đọc dừng ở lần gia hạn kế (live_lease_lost); khóa của chủ lạ
	// hết hạn thì một worker nhận lại.
	before := len(src.runsOf(ids[0]))
	e.mustTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.live_sessions SET lease_owner = 'intruder',
			lease_until = now() + interval '1 second' WHERE id = $1`, ids[0])
		return err //nolint:wrapcheck // test
	})
	eventually(t, 10*time.Second, "luồng dừng khi mất khóa", func() bool { return src.runningOf(ids[0]) == 0 })
	eventually(t, 10*time.Second, "worker nhận lại sau khi khóa lạ hết hạn", func() bool { return len(src.runsOf(ids[0])) > before })
	lost := int64(0)
	for _, r := range e.d.Metrics.Drain() {
		if r.Name == "live_lease_lost" {
			lost += r.Count
		}
	}
	if lost < 1 {
		t.Errorf("live_lease_lost = %d, muốn ≥ 1", lost)
	}
	if src.overlapped() {
		t.Error("hai Run của cùng phiên chạy chồng nhau")
	}
}
