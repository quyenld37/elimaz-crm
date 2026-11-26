package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng quy tắc tự động theo Page (kế hoạch M4, Task 165): đọc cần conversation.read trên Page, lưu cần
// conversation.settings trên Page (module kiểm; không có → 404). Cấu hình đi qua JSON của AutomationRuleConfig (oneOf
// theo kind, ogen đã kiểm lược đồ).

// ListAutomationRules (GET /api/v1/channels/{id}/automation-rules).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAutomationRules(ctx context.Context, params gen.ListAutomationRulesParams) (gen.ListAutomationRulesRes, error) {
	var rules []conversation.AutomationRule
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		rules, err = conversation.ListAutomationRules(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc quy tắc tự động: %w", err)
	}
	out := &gen.AutomationRuleList{Items: make([]gen.AutomationRule, 0, len(rules))}
	for _, r := range rules {
		o, err := automationRuleOut(r)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *o)
	}
	return out, nil
}

// UpdateAutomationRule (PUT /api/v1/channels/{id}/automation-rules/{kind}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateAutomationRule(ctx context.Context, req *gen.UpdateAutomationRuleRequest,
	params gen.UpdateAutomationRuleParams,
) (gen.UpdateAutomationRuleRes, error) {
	cfg, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("mã hóa cấu hình quy tắc: %w", err)
	}
	out, err := h.conversationChange(ctx, http.StatusOK, "lưu quy tắc tự động", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		r, err := w.UpdateAutomationRule(ctx, tx, u, params.ID, string(params.Kind), req.Enabled, cfg, req.ExpectedVersion)
		if err != nil {
			return nil, err //nolint:wrapcheck // conversation đã bọc
		}
		return automationRuleOut(r)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.AutomationRule), nil //nolint:forcetypeassert // fn trên trả *gen.AutomationRule
}

func automationRuleOut(r conversation.AutomationRule) (*gen.AutomationRule, error) {
	o := &gen.AutomationRule{Kind: gen.AutomationRuleKind(r.Kind), Enabled: r.Enabled, Version: r.Version}
	if err := json.Unmarshal(r.Config, &o.Config); err != nil {
		return nil, fmt.Errorf("đọc cấu hình quy tắc %s: %w", r.Kind, err)
	}
	if r.UpdatedAt != nil {
		o.UpdatedAt = gen.NewOptDateTime(r.UpdatedAt.UTC())
	}
	return o, nil
}
