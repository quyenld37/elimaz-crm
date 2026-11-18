package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// MergeCustomer (POST /api/v1/customers/{id}/merge, customer.merge) gộp nhóm của mergedId vào nhóm của khách id (Task
// M2-11): chỉ đổi root_id; đã cùng gốc → 422 merge_same_customer. Trả lần gộp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) MergeCustomer(ctx context.Context, req *gen.MergeCustomerRequest, params gen.MergeCustomerParams) (gen.MergeCustomerRes, error) {
	return h.mergeWrite(ctx, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Merge, error) {
		return w.MergeCustomers(ctx, tx, params.ID, req.MergedId, string(req.MatchedBy), a, now)
	})
}

// UndoCustomerMerge (POST /api/v1/customer-merges/{mergeId}/undo, customer.merge) hoàn tác lần gộp trong 30 ngày khi
// còn là lần gần nhất (Q14): 409 merge_not_latest kèm blockingMergeId, 422 merge_window_expired, 409 version_conflict
// khi đã hoàn tác. Trả lần gộp (có undoneAt).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UndoCustomerMerge(ctx context.Context, params gen.UndoCustomerMergeParams) (gen.UndoCustomerMergeRes, error) {
	return h.mergeWrite(ctx, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Merge, error) {
		if err := w.UndoMerge(ctx, tx, params.MergeId, a, now); err != nil {
			return customer.Merge{}, err //nolint:wrapcheck // sentinel của customer
		}
		return customer.GetMerge(ctx, tx, params.MergeId)
	})
}

// ListCustomerMergeCandidates (GET /api/v1/customers/{id}/merge-candidates, customer.read): nhóm khác có danh tính SĐT
// trùng SĐT trong địa chỉ hay 50 đơn gần nhất của nhóm; chỉ gợi ý, không tự gộp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomerMergeCandidates(ctx context.Context, params gen.ListCustomerMergeCandidatesParams) (gen.ListCustomerMergeCandidatesRes, error) {
	var cs []customer.Candidate
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		cs, err = customer.MergeCandidates(ctx, tx, params.ID)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("gợi ý khách trùng: %w", err)
	}
	out := &gen.MergeCandidateList{Items: make([]gen.MergeCandidate, 0, len(cs))}
	for _, c := range cs {
		phones := make([]string, 0, len(c.MatchedPhones))
		for _, p := range c.MatchedPhones {
			phones = append(phones, string(p))
		}
		out.Items = append(out.Items, gen.MergeCandidate{
			RootId: c.RootID, Code: c.Code, FullName: string(c.FullName), MatchedPhones: phones, Stats: statsOut(c.Stats),
		})
	}
	return out, nil
}

// mergeWrite chạy change trong refusableWrite với một customer.Writer của yêu cầu và trả lần gộp kèm tên người làm.
// Lỗi nghiệp vụ của customer là câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) mergeWrite(ctx context.Context,
	change func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Merge, error),
) (*gen.CustomerMerge, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isCustomerRefusal, "gộp khách", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
		m, err := change(tx, w, a, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return mergeOut(ctx, tx, m)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CustomerMerge), nil //nolint:forcetypeassert // change trên trả *gen.CustomerMerge
}

// mergeOut đổi lần gộp sang CustomerMerge của hợp đồng; tên người làm đọc từ nhân viên (không còn → tên rỗng).
func mergeOut(ctx context.Context, tx pgx.Tx, m customer.Merge) (*gen.CustomerMerge, error) {
	name := ""
	staff, err := identity.GetStaff(ctx, tx, m.ActorID)
	switch {
	case err == nil:
		name = staff.DisplayName
	case !errors.Is(err, identity.ErrStaffNotFound):
		return nil, fmt.Errorf("đọc người gộp: %w", err)
	}
	out := &gen.CustomerMerge{
		ID: m.ID, SurvivorId: m.SurvivorID, MergedId: m.MergedID, MovedIds: m.MovedIDs,
		MatchedBy: gen.CustomerMergeMatchedBy(m.MatchedBy), Actor: gen.Actor{ID: m.ActorID, Name: name},
		CreatedAt: m.CreatedAt,
	}
	if m.UndoneAt != nil {
		out.UndoneAt = gen.NewOptDateTime(*m.UndoneAt)
	}
	return out, nil
}
