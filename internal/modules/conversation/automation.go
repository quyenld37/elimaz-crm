package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Quy tắc tự động theo Page (Task 165): bốn loại auto_hide, auto_reply_comment, greeting, follow_up; loại chưa lưu trả
// cấu hình mặc định, tắt, Version 0; lưu lần đầu chèn dòng.

// EntityAutomationRule là loại thực thể của audit, báo hiệu quy tắc tự động (mã thực thể là mã Page).
const EntityAutomationRule = "automation_rule"

// AutomationRule là một quy tắc tự động của Page; Config là JSON đúng lược đồ AutomationRuleConfig của hợp đồng.
// Version 0, UpdatedAt nil: Page chưa lưu loại này.
type AutomationRule struct {
	Kind      string
	Enabled   bool
	Config    json.RawMessage
	Version   int32
	UpdatedAt *time.Time
}

func ruleOut(r automation.Rule) AutomationRule {
	return AutomationRule{Kind: r.Kind, Enabled: r.Enabled, Config: r.Config, Version: r.Version, UpdatedAt: r.UpdatedAt}
}

// ListAutomationRules trả đủ bốn quy tắc tự động của Page pageID (cần conversation.read trên Page; không có quyền hay
// Page không có → ErrNotFound / channel.ErrAccountNotFound).
func ListAutomationRules(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID) ([]AutomationRule, error) {
	if err := requirePage(ctx, tx, actor, assign.PermRead, pageID); err != nil {
		return nil, err
	}
	rules, err := automation.ListRules(ctx, tx, pageID)
	if err != nil {
		return nil, err //nolint:wrapcheck // automation đã bọc
	}
	out := make([]AutomationRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleOut(r))
	}
	return out, nil
}

// UpdateAutomationRule bật, tắt, sửa quy tắc loại kind của Page pageID khi expectedVersion khớp (0: chưa có dòng) —
// cần conversation.settings trên Page. Lỗi: ErrInvalidInput (loại lạ, cấu hình sai, config.kind khác kind), ErrVersionConflict, ErrNotFound,
// channel.ErrAccountNotFound. Audit conversation.automation_rule_updated (loại, cờ bật, chế độ ẩn, số bài ngoại lệ;
// không chép chữ cấu hình), báo hiệu conversation.changed thực thể automation_rule.
func (w Writer) UpdateAutomationRule(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID,
	kind string, enabled bool, config json.RawMessage, expectedVersion int32,
) (AutomationRule, error) {
	if err := requirePage(ctx, tx, actor, permSettings, pageID); err != nil {
		return AutomationRule{}, err
	}
	before, after, err := automation.SaveRule(ctx, tx, pageID, kind, enabled, config, expectedVersion, actor.UserID)
	switch {
	case errors.Is(err, automation.ErrRuleVersionConflict):
		return AutomationRule{}, ErrVersionConflict
	case errors.Is(err, automation.ErrInvalidRule):
		return AutomationRule{}, ErrInvalidInput
	case err != nil:
		return AutomationRule{}, err //nolint:wrapcheck // automation đã bọc
	}
	if err := w.staffAudit(ctx, tx, actor, identity.AuditEntry{
		Action: "conversation.automation_rule_updated", EntityType: EntityAutomationRule, EntityID: pageID.String(),
		Before: ruleAudit(before), After: ruleAudit(after),
	}); err != nil {
		return AutomationRule{}, err
	}
	if err := assign.Signal(ctx, tx, w.Realtime, actor.TenantID, pageID, EntityAutomationRule, pageID); err != nil {
		return AutomationRule{}, err //nolint:wrapcheck // assign đã bọc
	}
	return ruleOut(after), nil
}

// requirePage: actor có quyền perm trên Page pageID (không → ErrNotFound) và Page có trong shop (không →
// channel.ErrAccountNotFound).
func requirePage(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, perm identity.Permission, pageID uuid.UUID) error {
	if err := access.Require(actor, perm, pageID); err != nil {
		return err //nolint:wrapcheck // sentinel của module
	}
	_, err := channel.Account(ctx, tx, pageID)
	return err //nolint:wrapcheck // channel đã bọc
}

// ruleAudit: giá trị audit của quy tắc (loại, cờ, version; auto_hide thêm chế độ và số bài ngoại lệ).
func ruleAudit(r automation.Rule) map[string]any {
	m := map[string]any{"kind": r.Kind, "enabled": r.Enabled, "version": r.Version}
	if r.Kind == automation.KindAutoHide {
		var c automation.HideConfig
		if json.Unmarshal(r.Config, &c) == nil {
			m["mode"], m["except_posts"] = c.Mode, len(c.ExceptPostIDs)
		}
	}
	return m
}
