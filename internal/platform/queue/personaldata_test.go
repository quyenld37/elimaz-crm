package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Handler trả lỗi có SĐT, địa chỉ, token (lỗi tạm và lỗi vĩnh viễn): log của worker, error_events, last_error của
// việc và span của việc đều không chứa chúng (Task 319).
func TestHandlerErrorPersonalDataNotLeaked(t *testing.T) {
	exp := testkit.CaptureSpans(t)

	e := newEnv(t)
	temp := newFH(kind("t.pii.temp"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		return nil, testkit.PersonalDataError()
	})
	perm := newFH(kind("t.pii.perm"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		return nil, fmt.Errorf("%w: %w", queue.ErrPermanent, testkit.PersonalDataError())
	})
	tempID := e.enqueue(t, temp.kind, queue.NewJob{DedupeKey: "a"})
	permID := e.enqueue(t, perm.kind, queue.NewJob{DedupeKey: "b"})
	var logs testkit.SyncBuffer
	w := queue.NewWorker(e.app, []queue.Handler{temp, perm}, queue.WorkerOptions{
		WorkerID: "w1", Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	runOnce(t, w, 10)

	for _, id := range []uuid.UUID{tempID, permID} {
		r := e.job(t, id)
		if r.LastError == nil {
			t.Fatalf("việc %v thiếu last_error", id)
		}
		testkit.AssertNoPersonalData(t, "last_error", *r.LastError)
	}
	rows := e.errorEvents(t)
	if len(rows) != 1 {
		t.Fatalf("error_events có %d dòng, muốn 1 (lỗi tạm; ErrPermanent không ghi)", len(rows))
	}
	testkit.AssertNoPersonalData(t, "error_events.message", rows[0].Message)
	testkit.AssertNoPersonalData(t, "log worker", logs.String())
	spans := exp.GetSpans().Snapshots()
	if len(spans) == 0 {
		t.Fatal("không có span của việc")
	}
	testkit.AssertNoPersonalData(t, "span", testkit.SpanText(spans))
}
