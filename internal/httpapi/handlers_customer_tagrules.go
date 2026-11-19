package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// GetCustomerTagRule (GET /api/v1/customer-tags/{tagId}/rule, customer.read): quy tắc tự gắn nhãn; nhãn chưa có quy tắc
// (hay của tenant khác) → 404 not_found (Task M2-25).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerTagRule(ctx context.Context, params gen.GetCustomerTagRuleParams) (gen.GetCustomerTagRuleRes, error) {
	var r customer.TagRule
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		r, err = customer.GetTagRule(ctx, tx, params.TagId)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc quy tắc nhãn: %w", err)
	}
	out := tagRuleOut(r)
	return &out, nil
}

// PutCustomerTagRule (PUT /api/v1/customer-tags/{tagId}/rule, customer.tags.manage): đặt quy tắc (điều kiện nối bằng
// VÀ); trường hay phép lạ, giá trị sai → 422 invalid_tag_rule; version lệch, thiếu version khi đã có quy tắc → 409
// version_conflict; nhãn không có → 404. Trả quy tắc, 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PutCustomerTagRule(ctx context.Context, req *gen.PutCustomerTagRuleRequest, params gen.PutCustomerTagRuleParams) (gen.PutCustomerTagRuleRes, error) {
	in := customer.TagRuleInput{Enabled: req.Enabled, Conditions: make([]customer.TagCondition, 0, len(req.Conditions))}
	if v, ok := req.Version.Get(); ok {
		in.Version = &v
	}
	for _, c := range req.Conditions {
		in.Conditions = append(in.Conditions, customer.TagCondition{Field: c.Field, Op: c.Op, Value: c.Value})
	}
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isCustomerRefusal, "đặt quy tắc nhãn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
			r, err := w.PutTagRule(ctx, tx, params.TagId, in, a, time.Now().UTC())
			if err != nil {
				return nil, err //nolint:wrapcheck // lỗi của customer đã bọc
			}
			v := tagRuleOut(r)
			return &v, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CustomerTagRule), nil //nolint:forcetypeassert // change trên trả *gen.CustomerTagRule
}

func tagRuleOut(r customer.TagRule) gen.CustomerTagRule {
	out := gen.CustomerTagRule{
		TagId: r.TagID, Enabled: r.Enabled, Version: r.Version, Conditions: make([]gen.TagCondition, 0, len(r.Conditions)),
	}
	for _, c := range r.Conditions {
		out.Conditions = append(out.Conditions, gen.TagCondition{
			Field: gen.TagConditionField(c.Field), Op: gen.TagConditionOp(c.Op), Value: c.Value,
		})
	}
	return out
}
