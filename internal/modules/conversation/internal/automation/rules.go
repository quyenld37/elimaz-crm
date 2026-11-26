package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
)

// Loại quy tắc (cột automation_rules.kind; AutomationRuleKind của openapi.yaml).
const (
	KindAutoHide         = "auto_hide"
	KindAutoReplyComment = "auto_reply_comment"
	KindGreeting         = "greeting"
	KindFollowUp         = "follow_up"
)

// Kinds là bốn loại quy tắc theo thứ tự trả của API.
func Kinds() []string {
	return []string{KindAutoHide, KindAutoReplyComment, KindGreeting, KindFollowUp}
}

// Chế độ của auto_hide (09 mục E).
const (
	HideOff       = "off"
	HidePhoneOnly = "phone_only"
	HideAll       = "all"
)

// maxExceptPosts là số bài ngoại lệ tối đa của auto_hide (openapi.yaml AutoHideConfig).
const maxExceptPosts = 200

// ErrInvalidRule là lỗi: loại lạ, cấu hình không đọc được hay kind của cấu hình khác loại trên đường dẫn (gói gốc đổi thành ErrInvalidInput, 422).
var ErrInvalidRule = errors.New("conversation: quy tắc tự động không hợp lệ")

// ErrRuleVersionConflict là lỗi: expectedVersion không khớp (409 version_conflict).
var ErrRuleVersionConflict = errors.New("conversation: quy tắc tự động đã được sửa")

// Rule là một quy tắc tự động của Page; ID uuid.Nil và Version 0 khi Page chưa lưu loại này (cấu hình mặc định, tắt).
type Rule struct {
	ID, AccountID uuid.UUID
	Kind          string
	Enabled       bool
	Config        json.RawMessage
	Version       int32
	UpdatedAt     *time.Time
}

// HideConfig là cấu hình auto_hide.
type HideConfig struct {
	Kind          string   `json:"kind"`
	Mode          string   `json:"mode"`
	ExceptPostIDs []string `json:"exceptPostIds"`
}

// defaultConfigs là cấu hình mặc định (tắt) của loại chưa lưu; đúng lược đồ oneOf của openapi.yaml. Chữ mặc định
// chỉ để màn hình có chỗ sửa; quy tắc tắt nên không bao giờ gửi đi. follow_up chưa có mẫu: mã rỗng (uuid.Nil).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var defaultConfigs = map[string]string{
	KindAutoHide: `{"kind":"auto_hide","mode":"off","exceptPostIds":[]}`,
	KindAutoReplyComment: `{"kind":"auto_reply_comment","replies":["Dạ shop đã nhắn tin cho mình, mình xem hộp thư giúp shop nhé ạ"],` +
		`"postIds":[],"exceptPostIds":[],"includeLive":false,"perUserPerPostHours":24}`,
	KindGreeting: `{"kind":"greeting","text":"Dạ shop chào mình, mình cần shop tư vấn mẫu nào ạ?","quickReplies":[]}`,
	KindFollowUp: `{"kind":"follow_up","afterHours":4,"templateId":"00000000-0000-0000-0000-000000000000"}`,
}

func defaultRule(pageID uuid.UUID, kind string) Rule {
	return Rule{AccountID: pageID, Kind: kind, Config: json.RawMessage(defaultConfigs[kind])}
}

func ruleFrom(r store.GetAutomationRuleRow) Rule {
	at := r.UpdatedAt
	return Rule{
		ID: r.ID, AccountID: r.ChannelAccountID, Kind: r.Kind, Enabled: r.Enabled, Config: json.RawMessage(r.Config),
		Version: r.Version, UpdatedAt: &at,
	}
}

// LoadRule đọc quy tắc loại kind của Page pageID (không khóa); chưa lưu → mặc định (tắt, ID uuid.Nil).
func LoadRule(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, kind string) (Rule, error) {
	r, err := store.New(tx).GetAutomationRule(ctx, store.GetAutomationRuleParams{ChannelAccountID: pageID, Kind: kind})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultRule(pageID, kind), nil
	case err != nil:
		return Rule{}, fmt.Errorf("đọc quy tắc tự động: %w", err)
	}
	return ruleFrom(r), nil
}

// ListRules trả đủ bốn loại quy tắc của Page pageID theo thứ tự Kinds(); loại chưa lưu → mặc định.
func ListRules(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) ([]Rule, error) {
	rows, err := store.New(tx).ListAutomationRules(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("đọc quy tắc tự động: %w", err)
	}
	out := make([]Rule, 0, len(Kinds()))
	for _, k := range Kinds() {
		i := slices.IndexFunc(rows, func(r store.ListAutomationRulesRow) bool { return r.Kind == k })
		if i < 0 {
			out = append(out, defaultRule(pageID, k))
			continue
		}
		out = append(out, ruleFrom(store.GetAutomationRuleRow(rows[i])))
	}
	return out, nil
}

// SaveRule lưu quy tắc loại kind của Page pageID (bên gọi đã kiểm quyền, Page có thật) khi expectedVersion khớp (0:
// chưa có dòng). config phải là JSON có "kind" = kind; auto_hide kiểm thêm chế độ và số bài ngoại lệ (các loại khác
// đã được hợp đồng kiểm lược đồ; greeting kiểm thêm giới hạn chữ và nút). Trả bản trước (mặc định khi chưa có) và bản sau.
func SaveRule(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, kind string, enabled bool, config json.RawMessage,
	expectedVersion int32, by uuid.UUID,
) (before, after Rule, err error) {
	if err := validConfig(kind, config); err != nil {
		return Rule{}, Rule{}, err
	}
	q := store.New(tx)
	before = defaultRule(pageID, kind)
	switch r, err := q.LockAutomationRule(ctx, store.LockAutomationRuleParams{ChannelAccountID: pageID, Kind: kind}); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Rule{}, Rule{}, fmt.Errorf("khóa quy tắc tự động: %w", err)
	default:
		before = ruleFrom(store.GetAutomationRuleRow(r))
	}
	if before.Version != expectedVersion {
		return Rule{}, Rule{}, ErrRuleVersionConflict
	}
	r, err := q.SaveAutomationRule(ctx, store.SaveAutomationRuleParams{
		ChannelAccountID: pageID, Kind: kind, Config: config, Enabled: enabled,
		UpdatedBy: pgtype.UUID{Bytes: by, Valid: by != uuid.Nil}, ExpectedVersion: expectedVersion,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows): // lần lưu đầu đồng thời: dòng vừa được người khác chèn
		return Rule{}, Rule{}, ErrRuleVersionConflict
	case err != nil:
		return Rule{}, Rule{}, fmt.Errorf("lưu quy tắc tự động: %w", err)
	}
	return before, ruleFrom(store.GetAutomationRuleRow(r)), nil
}

// validConfig kiểm loại và cấu hình (ErrInvalidRule).
func validConfig(kind string, config json.RawMessage) error {
	if !slices.Contains(Kinds(), kind) {
		return ErrInvalidRule
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(config, &head); err != nil || head.Kind != kind {
		return ErrInvalidRule
	}
	switch kind {
	case KindGreeting:
		var g GreetingConfig
		if err := json.Unmarshal(config, &g); err != nil || !g.valid() {
			return ErrInvalidRule
		}
		return nil
	case KindAutoHide:
	default:
		return nil
	}
	var c HideConfig
	if err := json.Unmarshal(config, &c); err != nil || !slices.Contains([]string{HideOff, HidePhoneOnly, HideAll}, c.Mode) ||
		len(c.ExceptPostIDs) > maxExceptPosts {
		return ErrInvalidRule
	}
	return nil
}
