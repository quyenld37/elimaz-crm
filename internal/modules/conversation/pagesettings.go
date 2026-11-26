package conversation

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Cài đặt hộp chat theo Page (Task 160): xoay vòng (tắt mặc định, Q15), nguồn đơn của hộp thư, comment, khung giờ live,
// đọc comment định kỳ, tuổi tối đa sự kiện tự động. Page chưa có dòng: mặc định, version 0; lưu lần đầu chèn dòng.

// EntityPageSettings là loại thực thể của báo hiệu, audit của cài đặt Page.
const EntityPageSettings = "page_settings"

const (
	permSettings identity.Permission = "conversation.settings"

	defaultPollMode = "auto"
	defaultMaxAge   = 15
	minMaxAge       = 5
	maxMaxAge       = 120
	maxRoundRobin   = 100
	maxLiveSlots    = 12
	clockLayout     = "15:04"
)

// LiveSlot là một khung giờ live trong ngày (giờ Việt Nam, "HH:MM", From < To) và nguồn đơn của nó.
type LiveSlot struct {
	From            string    `json:"from"`
	To              string    `json:"to"`
	TrafficSourceID uuid.UUID `json:"trafficSourceId"`
}

// PageSettingsInput là nội dung cài đặt nhân viên lưu.
type PageSettingsInput struct {
	RoundRobinEnabled       bool
	RoundRobinUserIDs       []uuid.UUID
	AskAddressTemplateID    *uuid.UUID
	InboxTrafficSourceID    *uuid.UUID
	CommentTrafficSourceID  *uuid.UUID
	LiveSlots               []LiveSlot
	CommentPollMode         string
	AutomationMaxAgeMinutes int32
}

// PageSettings là cài đặt của một Page; Version 0 khi chưa lưu lần nào.
type PageSettings struct {
	PageID uuid.UUID
	PageSettingsInput
	Version int32
}

func defaultSettings(pageID uuid.UUID) PageSettings {
	return PageSettings{PageID: pageID, PageSettingsInput: PageSettingsInput{
		RoundRobinUserIDs: []uuid.UUID{}, LiveSlots: []LiveSlot{}, CommentPollMode: defaultPollMode,
		AutomationMaxAgeMinutes: defaultMaxAge,
	}}
}

func settingsFrom(r store.GetPageSettingsRow) (PageSettings, error) {
	s := PageSettings{PageID: r.ChannelAccountID, Version: r.Version, PageSettingsInput: PageSettingsInput{
		RoundRobinEnabled: r.RoundRobinEnabled, RoundRobinUserIDs: slices.Clone(r.RoundRobinUserIds),
		AskAddressTemplateID: assign.PtrUUID(r.AskAddressTemplateID), InboxTrafficSourceID: assign.PtrUUID(r.InboxTrafficSourceID),
		CommentTrafficSourceID: assign.PtrUUID(r.CommentTrafficSourceID), CommentPollMode: r.CommentPollMode,
		AutomationMaxAgeMinutes: r.AutomationMaxAgeMinutes,
	}}
	if s.RoundRobinUserIDs == nil {
		s.RoundRobinUserIDs = []uuid.UUID{}
	}
	if err := json.Unmarshal(r.LiveSlots, &s.LiveSlots); err != nil {
		return PageSettings{}, fmt.Errorf("đọc khung giờ live: %w", err)
	}
	if s.LiveSlots == nil {
		s.LiveSlots = []LiveSlot{}
	}
	return s, nil
}

// GetPageSettings trả cài đặt của Page pageID (cần conversation.read trên Page; Page không có →
// channel.ErrAccountNotFound); chưa lưu lần nào → mặc định, Version 0.
func GetPageSettings(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID) (PageSettings, error) {
	if err := access.Require(actor, assign.PermRead, pageID); err != nil {
		return PageSettings{}, err //nolint:wrapcheck // sentinel của module
	}
	if _, err := channel.Account(ctx, tx, pageID); err != nil {
		return PageSettings{}, err //nolint:wrapcheck // channel đã bọc
	}
	return readSettings(ctx, tx, pageID)
}

func readSettings(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) (PageSettings, error) {
	r, err := store.New(tx).GetPageSettings(ctx, pageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultSettings(pageID), nil
	}
	if err != nil {
		return PageSettings{}, fmt.Errorf("đọc cài đặt Page: %w", err)
	}
	return settingsFrom(r)
}

// UpdatePageSettings lưu cài đặt của Page pageID khi expectedVersion khớp (0: chưa có dòng) — cần conversation.settings
// trên Page. Kiểm: khung giờ live đúng dạng, From < To, không chồng nhau; nguồn đơn mới đổi còn bật
// (order.ErrTrafficSourceInactive); người trong xoay vòng active và có conversation.read trên Page; mẫu xin địa chỉ
// phải null cho tới khi có bảng mẫu (Task 161). Sai → ErrInvalidInput. Audit conversation.page_settings_updated (tên
// trường đổi), báo hiệu conversation.changed thực thể page_settings.
func (w Writer) UpdatePageSettings(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID,
	in PageSettingsInput, expectedVersion int32,
) (PageSettings, error) {
	if err := access.Require(actor, permSettings, pageID); err != nil {
		return PageSettings{}, err //nolint:wrapcheck // sentinel của module
	}
	if _, err := channel.Account(ctx, tx, pageID); err != nil {
		return PageSettings{}, err //nolint:wrapcheck // channel đã bọc
	}
	if err := validSettingsShape(in); err != nil {
		return PageSettings{}, err
	}
	q := store.New(tx)
	cur := defaultSettings(pageID)
	switch r, err := q.LockPageSettings(ctx, pageID); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return PageSettings{}, fmt.Errorf("khóa cài đặt Page: %w", err)
	default:
		if cur, err = settingsFrom(store.GetPageSettingsRow(r)); err != nil {
			return PageSettings{}, err
		}
	}
	if cur.Version != expectedVersion {
		return PageSettings{}, ErrVersionConflict
	}
	if err := checkReferences(ctx, tx, pageID, cur.PageSettingsInput, in); err != nil {
		return PageSettings{}, err
	}
	if err := writeSettings(ctx, q, pageID, in, cur.Version); err != nil {
		return PageSettings{}, err
	}
	after, err := readSettings(ctx, tx, pageID)
	if err != nil {
		return PageSettings{}, err
	}
	before, now := settingsAudit(cur.PageSettingsInput), settingsAudit(after.PageSettingsInput)
	if err := w.staffAudit(ctx, tx, actor, identity.AuditEntry{
		Action: "conversation.page_settings_updated", EntityType: EntityPageSettings, EntityID: pageID.String(),
		Before: before, After: now,
	}); err != nil {
		return PageSettings{}, err
	}
	if err := assign.Signal(ctx, tx, w.Realtime, actor.TenantID, pageID, EntityPageSettings, pageID); err != nil {
		return PageSettings{}, err //nolint:wrapcheck // assign đã bọc
	}
	return after, nil
}

func writeSettings(ctx context.Context, q *store.Queries, pageID uuid.UUID, in PageSettingsInput, expected int32) error {
	slots, err := json.Marshal(in.LiveSlots)
	if err != nil {
		return fmt.Errorf("mã hóa khung giờ live: %w", err)
	}
	users := in.RoundRobinUserIDs
	if users == nil {
		users = []uuid.UUID{}
	}
	n, err := q.SavePageSettings(ctx, store.SavePageSettingsParams{
		ChannelAccountID: pageID, RoundRobinEnabled: in.RoundRobinEnabled, RoundRobinUserIds: users,
		AskAddressTemplateID: db.NullUUID(in.AskAddressTemplateID), InboxTrafficSourceID: db.NullUUID(in.InboxTrafficSourceID),
		CommentTrafficSourceID: db.NullUUID(in.CommentTrafficSourceID), LiveSlots: slots, CommentPollMode: in.CommentPollMode,
		AutomationMaxAgeMinutes: in.AutomationMaxAgeMinutes, ExpectedVersion: expected,
	})
	if err != nil {
		return fmt.Errorf("lưu cài đặt Page: %w", err)
	}
	if n == 0 {
		return ErrVersionConflict // lần lưu đầu đồng thời: dòng vừa được người khác chèn
	}
	return nil
}

// validSettingsShape kiểm phần không cần database: giới hạn số, chế độ đọc comment, mẫu xin địa chỉ, khung giờ live.
func validSettingsShape(in PageSettingsInput) error {
	if in.AutomationMaxAgeMinutes < minMaxAge || in.AutomationMaxAgeMinutes > maxMaxAge ||
		!slices.Contains([]string{"auto", "on", "off"}, in.CommentPollMode) ||
		len(in.RoundRobinUserIDs) > maxRoundRobin || len(in.LiveSlots) > maxLiveSlots ||
		in.AskAddressTemplateID != nil { // chưa có bảng mẫu trả lời (Task 161): chưa kiểm được → không nhận
		return ErrInvalidInput
	}
	return ValidLiveSlots(in.LiveSlots)
}

// ValidLiveSlots kiểm mỗi khung "HH:MM", From < To trong cùng ngày; hai khung không chồng nhau (chạm mép thì được). Sai →
// ErrInvalidInput.
func ValidLiveSlots(slots []LiveSlot) error {
	type span struct{ from, to time.Time }
	spans := make([]span, 0, len(slots))
	for _, s := range slots {
		f, err1 := time.Parse(clockLayout, s.From)
		t, err2 := time.Parse(clockLayout, s.To)
		if err1 != nil || err2 != nil || !f.Before(t) || s.TrafficSourceID == uuid.Nil {
			return ErrInvalidInput
		}
		spans = append(spans, span{f, t})
	}
	slices.SortFunc(spans, func(a, b span) int { return a.from.Compare(b.from) })
	for i := 1; i < len(spans); i++ {
		if spans[i].from.Before(spans[i-1].to) {
			return ErrInvalidInput
		}
	}
	return nil
}

// checkReferences kiểm nguồn đơn mới (chưa có trong cur) còn bật và người trong xoay vòng có quyền trên Page.
func checkReferences(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, cur, in PageSettingsInput) error {
	old := map[uuid.UUID]bool{}
	for _, id := range sources(cur) {
		old[id] = true
	}
	for _, id := range sources(in) {
		if old[id] {
			continue
		}
		if err := order.ActiveSource(ctx, tx, id); err != nil {
			return err //nolint:wrapcheck // sentinel của order (traffic_source_inactive)
		}
		old[id] = true
	}
	for _, u := range in.RoundRobinUserIDs {
		ok, err := assign.Eligible(ctx, tx, pageID, u)
		if err != nil {
			return err //nolint:wrapcheck // assign đã bọc
		}
		if !ok {
			return ErrInvalidInput
		}
	}
	return nil
}

func sources(s PageSettingsInput) []uuid.UUID {
	var out []uuid.UUID
	for _, p := range []*uuid.UUID{s.InboxTrafficSourceID, s.CommentTrafficSourceID} {
		if p != nil {
			out = append(out, *p)
		}
	}
	for _, l := range s.LiveSlots {
		out = append(out, l.TrafficSourceID)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return cmp.Compare(a.String(), b.String()) })
	return slices.Compact(out)
}

// settingsAudit: giá trị audit của cài đặt (mã, cờ, số; không dữ liệu cá nhân).
func settingsAudit(s PageSettingsInput) map[string]any {
	return map[string]any{
		"round_robin_enabled": s.RoundRobinEnabled, "round_robin_user_ids": s.RoundRobinUserIDs,
		"ask_address_template_id":   assign.IDOrNil(s.AskAddressTemplateID),
		"inbox_traffic_source_id":   assign.IDOrNil(s.InboxTrafficSourceID),
		"comment_traffic_source_id": assign.IDOrNil(s.CommentTrafficSourceID), "live_slots": s.LiveSlots,
		"comment_poll_mode": s.CommentPollMode, "automation_max_age_minutes": s.AutomationMaxAgeMinutes,
	}
}
