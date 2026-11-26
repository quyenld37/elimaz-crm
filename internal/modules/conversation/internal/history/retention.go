package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Hạn giữ hội thoại (Q17, H11): lịch conversation.retention chạy mỗi giờ theo tenant, chỉ dọn trong khung 2:00–2:59
// giờ Việt Nam (lịch nền không neo được giờ cố định; tệp task-164, Quyết định 3). Mỗi việc một transaction, một lô tối
// đa retentionBatch tin gửi trước now − CONVERSATION_RETENTION_MONTHS: đặt app.retention = on; khóa các hội thoại của
// lô và các hội thoại rỗng quá hạn (id tăng, ADR-46 nhóm 6); xóa lịch sử sửa rồi tin; xóa hội thoại không còn tin,
// tin cuối quá hạn, không có hẹn gọi lại hay nhật ký gọi, kèm nhãn và lịch sử phân công; đánh dấu xóa tệp đính kèm
// không còn tin hay mẫu trả lời nào dùng (files.MarkDeleted, nhóm 12b). Lô đầy → ghi một việc tiếp theo (chạy ngay,
// không chờ khung giờ). Chạy lại không lỗi: lần sau xóa 0.

const (
	// RetentionSchedule là tên lịch dọn theo hạn giữ.
	RetentionSchedule = "conversation.retention"
	// RetentionKind là loại việc của lịch dọn.
	RetentionKind = "task." + RetentionSchedule

	retentionEvery = time.Hour
	retentionBatch = 5000
	// retentionConvBatch là số hội thoại rỗng tối đa mỗi lô.
	retentionConvBatch = 1000
	// retentionHour là giờ (giờ Việt Nam) lịch được dọn.
	retentionHour = 2
	vnOffset      = 7 * time.Hour

	skippedOutsideHours = "outside_hours"
)

func retentionKind() queue.Kind {
	return queue.Kind{Name: RetentionKind, Policy: queue.Policy{MaxAttempts: 3, ExpiresAfter: retentionEvery}}
}

func retentionSchedule() schedules.Schedule {
	return schedules.Schedule{Name: RetentionSchedule, Every: retentionEvery, PerTenant: true, Kind: retentionKind()}
}

// retentionPayload là payload của việc dọn: Continue = việc tiếp theo của một lô đầy (bỏ qua khung giờ).
type retentionPayload struct {
	Continue bool `json:"continue,omitempty"`
}

// retentionResult là kết quả của một việc dọn (jobs.result, chỉ có số).
type retentionResult struct {
	Messages      int    `json:"messages"`
	Conversations int    `json:"conversations"`
	Files         int    `json:"files"`
	More          bool   `json:"more"`
	Skipped       string `json:"skipped,omitempty"`
}

// retentionHandler là việc nền của lịch dọn; batch là số tin tối đa mỗi lô.
type retentionHandler struct {
	d     deps.Deps
	now   func() time.Time
	batch int
}

// NewRetentionHandler trả việc dọn theo hạn giữ, mỗi lô tối đa batch tin (Parts dùng retentionBatch).
func NewRetentionHandler(d deps.Deps, batch int) queue.Handler { //nolint:ireturn // việc nền là interface của queue
	return retentionHandler{d: d, now: d.Clock(), batch: batch}
}

func (retentionHandler) Kind() queue.Kind { return retentionKind() }

// Handle dọn một lô trong tx của việc (worker đặt WithTenant của tenant của việc).
func (h retentionHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	var p retentionPayload
	_ = json.Unmarshal(j.Payload, &p) // payload của lịch ({"slot": …}) không có continue
	now := h.now().UTC()
	var res retentionResult
	if !p.Continue && now.Add(vnOffset).Hour() != retentionHour {
		res.Skipped = skippedOutsideHours
	} else {
		var err error
		if res, err = h.sweep(ctx, tx, now); err != nil {
			return nil, err
		}
		if res.More {
			if _, _, err := h.d.Queue.Enqueue(ctx, tx, retentionKind(), queue.NewJob{
				TenantID: j.TenantID, Payload: retentionPayload{Continue: true},
				DedupeKey: retentionContinueDedupPrefix + j.ID.String(),
			}); err != nil {
				return nil, fmt.Errorf("ghi việc dọn lô tiếp theo: %w", err)
			}
		}
	}
	out, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả việc dọn: %w", err)
	}
	return out, nil
}

// sweep dọn một lô ở mốc now (xem chú thích đầu tệp).
func (h retentionHandler) sweep(ctx context.Context, tx pgx.Tx, now time.Time) (retentionResult, error) {
	var res retentionResult
	cutoff := retentionCutoff(h.d, now)
	q := store.New(tx)
	if err := q.EnableRetention(ctx); err != nil {
		return res, fmt.Errorf("bật cờ dọn theo hạn giữ: %w", err)
	}
	batch, err := q.ExpiredMessageBatch(ctx, store.ExpiredMessageBatchParams{Cutoff: cutoff, MaxRows: int32(h.batch)}) //nolint:gosec // lô nhỏ
	if err != nil {
		return res, fmt.Errorf("chọn tin quá hạn giữ: %w", err)
	}
	empty, err := q.ExpiredEmptyConversations(ctx, store.ExpiredEmptyConversationsParams{
		Cutoff: cutoff, MaxRows: retentionConvBatch,
	})
	if err != nil {
		return res, fmt.Errorf("chọn hội thoại rỗng quá hạn giữ: %w", err)
	}
	msgIDs := make([]uuid.UUID, 0, len(batch))
	convIDs := slices.Clone(empty)
	for _, m := range batch {
		msgIDs = append(msgIDs, m.ID)
		convIDs = append(convIDs, m.ConversationID)
	}
	slices.SortFunc(convIDs, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	convIDs = slices.Compact(convIDs)
	if len(convIDs) == 0 {
		return res, nil
	}
	if _, err := q.LockConversationsForReminders(ctx, convIDs); err != nil {
		return res, fmt.Errorf("khóa hội thoại cần dọn: %w", err)
	}
	fileIDs, deleted, err := deleteMessages(ctx, q, msgIDs, cutoff)
	if err != nil {
		return res, err
	}
	res.Messages = deleted
	gone, err := q.DeleteEmptyConversations(ctx, store.DeleteEmptyConversationsParams{Ids: convIDs, Cutoff: cutoff})
	if err != nil {
		return res, fmt.Errorf("xóa hội thoại quá hạn giữ: %w", err)
	}
	res.Conversations = len(gone)
	if res.Files, err = markFiles(ctx, tx, q, fileIDs); err != nil {
		return res, err
	}
	res.More = len(batch) >= h.batch || len(empty) >= retentionConvBatch
	return res, nil
}

// deleteMessages xóa lịch sử sửa rồi các tin ids (còn trước cutoff); trả mã tệp đính kèm của các tin đã xóa và số tin
// đã xóa.
func deleteMessages(ctx context.Context, q *store.Queries, ids []uuid.UUID, cutoff time.Time) ([]uuid.UUID, int, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	if _, err := q.DeleteRevisionsOfMessages(ctx, ids); err != nil {
		return nil, 0, fmt.Errorf("xóa lịch sử sửa của tin quá hạn: %w", err)
	}
	atts, err := q.DeleteExpiredMessages(ctx, store.DeleteExpiredMessagesParams{Ids: ids, Cutoff: cutoff})
	if err != nil {
		return nil, 0, fmt.Errorf("xóa tin quá hạn giữ: %w", err)
	}
	var out []uuid.UUID
	for _, raw := range atts {
		var items []struct {
			FileID *uuid.UUID `json:"fileId"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			continue // đính kèm không đúng dạng: không có tệp để đánh dấu
		}
		for _, it := range items {
			if it.FileID != nil && !slices.Contains(out, *it.FileID) {
				out = append(out, *it.FileID)
			}
		}
	}
	return out, len(atts), nil
}

// markFiles đánh dấu xóa các tệp ids không còn tin hay mẫu trả lời nào dùng; trả số tệp đã đánh dấu.
func markFiles(ctx context.Context, tx pgx.Tx, q *store.Queries, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	used, err := q.ReferencedFiles(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("tìm tệp còn được dùng: %w", err)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }) // khóa platform.files theo id tăng
	n := 0
	for _, id := range ids {
		if slices.Contains(used, id) {
			continue
		}
		err := files.MarkDeleted(ctx, tx, id, uuid.Nil)
		switch {
		case errors.Is(err, files.ErrNotFound):
			continue // tệp chưa từng ghi (đính kèm chưa tải) hay của shop khác
		case err != nil:
			return n, err //nolint:wrapcheck // files đã bọc việc đang làm
		}
		n++
	}
	return n, nil
}
