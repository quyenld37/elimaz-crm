package customer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Sự kiện của gộp khách (docs/views.md mục "Sự kiện công bố"); payload là MergeEvent, chỉ mang mã.
const (
	// TopicCustomerMerged phát khi gộp (aggregate customer_merge, Seq 1).
	TopicCustomerMerged = "customer.merged"
	// TopicCustomerMergeUndone phát khi hoàn tác gộp (aggregate customer_merge, Seq 2).
	TopicCustomerMergeUndone = "customer.merge_undone"

	aggregateMerge = "customer_merge"
)

// MatchedByStaff là lần gộp do nhân viên tự nhận ra trùng (cột customer_merges.matched_by là MatchedByPhone hay
// MatchedByStaff; MatchedByPhone khai ở search.go, cùng giá trị "phone").
const MatchedByStaff = "staff"

const (
	// mergeUndoWindow: hoàn tác được trong 30 ngày kể từ lần gộp (05 mục 1, Q14).
	mergeUndoWindow = 30 * 24 * time.Hour
	// mergeAttempts: số lần khóa lại khi gốc đổi giữa lúc đọc và lúc khóa (gộp, hoàn tác chạy song song).
	mergeAttempts = 3
)

// errMergeRetry chỉ dùng để lùi savepoint (nhả khóa dòng) khi gốc hay nhóm đổi giữa lúc đọc và lúc khóa; không ra khỏi
// gói.
var errMergeRetry = errors.New("customer: gốc đổi khi đang gộp")

// Merge là một lần gộp: nhóm của MergedID (gốc bị gộp) vào nhóm của SurvivorID (gốc giữ lại); MovedIDs là các hồ sơ đổi
// root_id trong lần này (theo mã), hoàn tác trả đúng các hồ sơ đó về MergedID. UndoneAt nil khi chưa hoàn tác.
type Merge struct {
	ID, SurvivorID, MergedID uuid.UUID
	MovedIDs                 []uuid.UUID
	MatchedBy                string
	ActorID                  uuid.UUID
	CreatedAt                time.Time
	UndoneAt                 *time.Time
}

// MergeEvent là payload của customer.merged và customer.merge_undone.
type MergeEvent struct {
	MergeID        uuid.UUID `json:"mergeId"`
	SurvivorRootID uuid.UUID `json:"survivorRootId"`
	MergedRootID   uuid.UUID `json:"mergedRootId"`
}

// MergeCustomers gộp nhóm của mergedID vào nhóm của survivorID (mỗi mã là gốc hay hồ sơ con) trong tx của bên gọi: chỉ
// đổi root_id của mọi hồ sơ thuộc gốc bị gộp sang gốc giữ lại (đơn, địa chỉ, ghi chú, danh tính giữ customer_id), ghi
// customer_merges, audit customer.merge, sự kiện customer.merged (Seq 1), báo hiệu cả hai gốc (05 mục 1, ADR-23).
// Hai mã đã chung gốc → ErrMergeSameCustomer; không có (hay tenant khác) → ErrNotFound; matchedBy lạ → ErrInvalidInput.
//
// Khóa (ADR-46 nhóm 5): mọi hồ sơ cần chạm ({gốc giữ lại} ∪ nhóm bị gộp) trong một câu theo id tăng dần, rồi đọc lại gốc
// và nhóm dưới khóa; lệch (gộp, hoàn tác song song vừa commit) → lùi savepoint để nhả khóa rồi làm lại, tối đa 3 lần.
func (w Writer) MergeCustomers(ctx context.Context, tx pgx.Tx, survivorID, mergedID uuid.UUID, matchedBy string, a Actor,
	now time.Time,
) (Merge, error) {
	if err := w.check(); err != nil {
		return Merge{}, err
	}
	if matchedBy != MatchedByPhone && matchedBy != MatchedByStaff {
		return Merge{}, ErrInvalidInput
	}
	for range mergeAttempts {
		var out Merge
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			var err error
			out, err = w.mergeOnce(ctx, sp, survivorID, mergedID, matchedBy, a, now)
			return err
		})
		switch {
		case errors.Is(err, errMergeRetry):
			continue
		case err != nil:
			return Merge{}, err //nolint:wrapcheck // lỗi của mergeOnce đã bọc; lỗi savepoint do db bọc
		}
		return out, nil
	}
	return Merge{}, errRootChurn
}

func (w Writer) mergeOnce(ctx context.Context, tx pgx.Tx, survivorID, mergedID uuid.UUID, matchedBy string, a Actor,
	now time.Time,
) (Merge, error) {
	s, err := RootOf(ctx, tx, survivorID)
	if err != nil {
		return Merge{}, err
	}
	m, err := RootOf(ctx, tx, mergedID)
	if err != nil {
		return Merge{}, err
	}
	if s == m {
		return Merge{}, ErrMergeSameCustomer
	}
	members, err := GroupMembers(ctx, tx, m)
	if err != nil {
		return Merge{}, err
	}
	q := store.New(tx)
	locked, err := q.LockCustomers(ctx, append(slices.Clone(members), s))
	if err != nil {
		return Merge{}, fmt.Errorf("khóa các hồ sơ gộp: %w", err)
	}
	if !stableGroups(locked, s, m, members) {
		return Merge{}, errMergeRetry
	}
	// Dưới khóa: gốc của hai mã đầu vào và thành viên của nhóm bị gộp phải như lúc đọc.
	if s2, err := RootOf(ctx, tx, survivorID); err != nil || s2 != s {
		return Merge{}, retryOr(err)
	}
	if m2, err := RootOf(ctx, tx, mergedID); err != nil || m2 != m {
		return Merge{}, retryOr(err)
	}
	if again, err := GroupMembers(ctx, tx, m); err != nil || !slices.Equal(again, members) {
		return Merge{}, retryOr(err)
	}
	moved := sortIDs(members)
	if err := q.SetRootID(ctx, store.SetRootIDParams{RootID: s, Now: now, Ids: moved}); err != nil {
		return Merge{}, fmt.Errorf("đổi gốc khi gộp: %w", err)
	}
	row, err := q.InsertMerge(ctx, store.InsertMergeParams{
		SurvivorID: s, MergedID: m, MovedIds: moved, MatchedBy: matchedBy, ActorID: a.ID, Now: now,
	})
	if err != nil {
		return Merge{}, fmt.Errorf("ghi lần gộp: %w", err)
	}
	after := map[string]any{
		"survivor_id": s.String(), "merged_id": m.String(), "moved_count": len(moved), "matched_by": matchedBy,
	}
	if err := w.auditEntity(ctx, tx, row.TenantID, a, "customer.merge", aggregateMerge, row.ID, nil, after); err != nil {
		return Merge{}, err
	}
	if err := w.emitMerge(ctx, tx, row.TenantID, TopicCustomerMerged, 1, row.ID, s, m); err != nil {
		return Merge{}, err
	}
	if err := w.signalBoth(ctx, tx, row.TenantID, s, m); err != nil {
		return Merge{}, err
	}
	return Merge{
		ID: row.ID, SurvivorID: s, MergedID: m, MovedIDs: moved, MatchedBy: matchedBy, ActorID: a.ID,
		CreatedAt: row.CreatedAt,
	}, nil
}

// stableGroups: dưới khóa, s vẫn là gốc và mọi hồ sơ của nhóm bị gộp vẫn trỏ m.
func stableGroups(locked []store.LockCustomersRow, s, m uuid.UUID, members []uuid.UUID) bool {
	if len(locked) != len(members)+1 {
		return false
	}
	for _, r := range locked {
		want := m
		if r.ID == s {
			want = s
		}
		if r.RootID != want {
			return false
		}
	}
	return true
}

// retryOr: lỗi đọc thật thì trả lỗi đó, không thì làm lại (gốc đổi).
func retryOr(err error) error {
	if err != nil {
		return err
	}
	return errMergeRetry
}

// UndoMerge hoàn tác lần gộp mergeID trong tx của bên gọi (Q14): đặt lại root_id = merged_id cho đúng các hồ sơ trong
// moved_ids, ghi undone_at, undone_by, audit customer.merge_undo, sự kiện customer.merge_undone (Seq 2), báo hiệu hai
// gốc. Không có (hay tenant khác) → ErrNotFound; đã hoàn tác (kể cả bởi yêu cầu song song) → ErrVersionConflict; quá 30
// ngày kể từ lần gộp → ErrMergeWindowExpired; còn lần gộp chưa hoàn tác khác đã dời gốc giữ lại (tạo sau lần này) →
// *MergeNotLatestError với lần phải hoàn tác trước. Khóa moved_ids theo id tăng dần rồi mới khóa dòng gộp (ADR-46).
func (w Writer) UndoMerge(ctx context.Context, tx pgx.Tx, mergeID uuid.UUID, a Actor, now time.Time) error {
	if err := w.check(); err != nil {
		return err
	}
	mg, err := GetMerge(ctx, tx, mergeID)
	if err != nil {
		return err
	}
	if mg.UndoneAt != nil {
		return ErrVersionConflict
	}
	if now.Sub(mg.CreatedAt) > mergeUndoWindow {
		return ErrMergeWindowExpired
	}
	q := store.New(tx)
	locked, err := q.LockCustomers(ctx, mg.MovedIDs)
	if err != nil {
		return fmt.Errorf("khóa các hồ sơ của lần gộp: %w", err)
	}
	undone, err := q.LockMerge(ctx, mergeID)
	if err != nil {
		return fmt.Errorf("khóa lần gộp: %w", err)
	}
	if undone.Valid {
		return ErrVersionConflict
	}
	blocking, err := q.BlockingMerge(ctx, store.BlockingMergeParams{SurvivorID: mg.SurvivorID, MergeID: mergeID})
	switch {
	case err == nil:
		return &MergeNotLatestError{BlockingMergeID: blocking}
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("tìm lần gộp chặn hoàn tác: %w", err)
	}
	// Không lần gộp nào dời gốc giữ lại thì mọi hồ sơ đã dời phải còn trỏ về nó (gốc chỉ đổi qua gộp, hoàn tác).
	if len(locked) != len(mg.MovedIDs) || slices.ContainsFunc(locked, func(r store.LockCustomersRow) bool {
		return r.RootID != mg.SurvivorID
	}) {
		return fmt.Errorf("hoàn tác gộp %s: hồ sơ đã dời không còn trỏ gốc giữ lại: %w", mergeID, errRootChurn)
	}
	tenantID := locked[0].TenantID
	if err := q.SetRootID(ctx, store.SetRootIDParams{RootID: mg.MergedID, Now: now, Ids: mg.MovedIDs}); err != nil {
		return fmt.Errorf("trả gốc khi hoàn tác gộp: %w", err)
	}
	if err := q.MarkMergeUndone(ctx, store.MarkMergeUndoneParams{Now: now, UndoneBy: a.createdBy(), ID: mergeID}); err != nil {
		return fmt.Errorf("ghi hoàn tác gộp: %w", err)
	}
	after := map[string]any{
		"survivor_id": mg.SurvivorID.String(), "merged_id": mg.MergedID.String(), "moved_count": len(mg.MovedIDs),
	}
	if err := w.auditEntity(ctx, tx, tenantID, a, "customer.merge_undo", aggregateMerge, mergeID, nil, after); err != nil {
		return err
	}
	if err := w.emitMerge(ctx, tx, tenantID, TopicCustomerMergeUndone, 2, mergeID, mg.SurvivorID, mg.MergedID); err != nil {
		return err
	}
	return w.signalBoth(ctx, tx, tenantID, mg.SurvivorID, mg.MergedID)
}

// GetMerge đọc một lần gộp; không có (hay tenant khác) → ErrNotFound.
func GetMerge(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Merge, error) {
	r, err := store.New(tx).GetMerge(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Merge{}, ErrNotFound
	}
	if err != nil {
		return Merge{}, fmt.Errorf("đọc lần gộp: %w", err)
	}
	return Merge{
		ID: r.ID, SurvivorID: r.SurvivorID, MergedID: r.MergedID, MovedIDs: r.MovedIds, MatchedBy: r.MatchedBy,
		ActorID: r.ActorID, CreatedAt: r.CreatedAt, UndoneAt: timeOf(r.UndoneAt),
	}, nil
}

func (w Writer) emitMerge(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, topic string, seq int64,
	mergeID, survivor, merged uuid.UUID,
) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenantID, AggregateType: aggregateMerge, AggregateID: mergeID.String(), Seq: seq, Topic: topic,
		Payload: MergeEvent{MergeID: mergeID, SurvivorRootID: survivor, MergedRootID: merged},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", topic, err)
	}
	return nil
}

func (w Writer) signalBoth(ctx context.Context, tx pgx.Tx, tenantID, survivor, merged uuid.UUID) error {
	if err := w.signal(ctx, tx, tenantID, survivor); err != nil {
		return err
	}
	return w.signal(ctx, tx, tenantID, merged)
}

func sortIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	return out
}

func timeOf(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
