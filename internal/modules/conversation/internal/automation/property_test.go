package automation_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// Quy luật (CH12 nền, ADR-25): với chuỗi ngẫu nhiên 1–3 comment, mỗi comment tới 1–5 lần qua webhook, đọc comment
// định kỳ hay hỏi bù (xử lý lại), hai worker chạy song song, quy tắc auto_hide đổi bật/tắt và chế độ giữa các lần tới
// → với mỗi (quy tắc, comment): tối đa 1 lượt chạy, tối đa 1 lệnh ẩn, tối đa 1 lời gọi ẩn tới Meta; lượt done/dropped/
// failed/enqueued luôn có đúng 1 lệnh, lượt bỏ qua không có lệnh.
func TestProperty_AtMostOneCommand(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	second := e.newWorker("w-2")
	rapid.Check(t, func(rt *rapid.T) {
		seen := e.propArrivals(rt) // comment của lượt này (lượt sau không chạm comment cũ)
		var wg sync.WaitGroup
		for _, w := range []*queue.Worker{e.worker, second} {
			wg.Go(func() { // hai worker cùng nhận việc; phần còn lại (hẹn lại, lệnh sau) chạy hết bằng DrainDue
				for range 20 {
					if k, err := w.RunOnce(t.Context(), 10); err != nil || k == 0 {
						return
					}
				}
			})
		}
		wg.Wait()
		testkit.DrainDue(t, e.staff.Owner, e.worker)
		for _, id := range seen {
			e.propCheck(rt, id)
		}
	})
}

// Bảng hằng của quy luật: đường tới, chế độ, chữ comment (có và không có SĐT).
var (
	propVias  = []string{webhooks.ViaWebhook, webhooks.ViaCommentPoll, webhooks.ViaBackfill} //nolint:gochecknoglobals // bảng hằng của test
	propModes = []string{automation.HideAll, automation.HidePhoneOnly, automation.HideOff}   //nolint:gochecknoglobals // bảng hằng của test
	propTexts = []string{"chốt 0912 345 678", "còn size M không", "+84 987 654 321 ib"}      //nolint:gochecknoglobals // bảng hằng của test
)

// propArrivals ghi 1–3 comment mới, mỗi comment tới 1–5 lần qua đường ngẫu nhiên, đôi khi đổi quy tắc trước một lần
// tới; trả mã các comment.
func (e *env) propArrivals(rt *rapid.T) []string {
	var seen []string
	n := rapid.IntRange(1, 3).Draw(rt, "comment")
	for i := range n {
		p := e.comment(postA, userA, rapid.SampledFrom(propTexts).Draw(rt, "chữ"+strconv.Itoa(i)), time.Now().UTC().Add(-time.Minute))
		seen = append(seen, p.CommentID)
		for k := range rapid.IntRange(1, 5).Draw(rt, "lần"+strconv.Itoa(i)) {
			if rapid.Bool().Draw(rt, "đổi quy tắc"+strconv.Itoa(i)+"."+strconv.Itoa(k)) {
				e.setRule(rapid.Bool().Draw(rt, "bật"), rapid.SampledFrom(propModes).Draw(rt, "chế độ"))
			}
			e.enqueue(p, rapid.SampledFrom(propVias).Draw(rt, "đường"))
		}
	}
	return seen
}

// propCheck: comment id có tối đa 1 lượt, 1 lệnh, 1 lời gọi Meta; lượt có lệnh thì đúng 1 lệnh, lượt bỏ qua thì 0.
func (e *env) propCheck(rt *rapid.T, id string) {
	out, rows, jobs := e.run(id)
	calls := e.hideCalls(id)
	if rows > 1 || jobs > 1 || calls > 1 {
		rt.Fatalf("comment %s: %d lượt, %d lệnh, %d lời gọi Meta", id, rows, jobs, calls)
	}
	switch automation.Outcome(out) {
	case automation.OutcomeDone, automation.OutcomeDropped, automation.OutcomeFailed, automation.OutcomeEnqueued:
		if jobs != 1 {
			rt.Fatalf("comment %s: lượt %s mà %d lệnh", id, out, jobs)
		}
	default:
		if jobs != 0 || calls != 0 {
			rt.Fatalf("comment %s: lượt %q mà %d lệnh, %d lời gọi", id, out, jobs, calls)
		}
	}
}
