package automation_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Biên của dò SĐT: hai SĐT cách nhau đúng một ký tự đều được tìm (không nhảy quá ký tự đầu của SĐT sau); chữ số đứng
// ngay trước 0 làm SĐT không hợp lệ dù phía sau không còn chữ số; một chỗ bắt đầu không khớp mẫu ("08," hay một SĐT
// 11 chữ số) không làm dừng việc dò SĐT phía sau. (Task 182, mutation testing M4)
func TestMutationM4_PhoneScanBoundaries(t *testing.T) {
	t.Parallel()
	extract := []struct {
		text string
		want []string
	}{
		{"0912345678,0987654321", []string{"0912345678", "0987654321"}},
		{"0912345678 0987654321", []string{"0912345678", "0987654321"}},
		{"size 08, sđt 0912345678", []string{"0912345678"}},
		{"09123456789 hoặc 0987654321", []string{"0987654321"}},
	}
	for _, c := range extract {
		if got := automation.ExtractPhones(c.text); !slices.Equal(got, c.want) {
			t.Errorf("ExtractPhones(%q) = %v, muốn %v", c.text, got, c.want)
		}
	}
	contains := []struct {
		text string
		want bool
	}{
		{"10912345678", false},  // chữ số đứng trước 0, sau là hết chuỗi
		{"110912345678", false}, // như trên, hai chữ số đứng trước
		{"đơn 08 sđt 0912345678", true},
		{"09123456789 hoặc 0987654321", true},
	}
	for _, c := range contains {
		if got := automation.ContainsPhone(c.text); got != c.want {
			t.Errorf("ContainsPhone(%q) = %v, muốn %v", c.text, got, c.want)
		}
	}
}

// Biên của cấu hình auto_reply_comment khi lưu: 5 câu, câu đúng 500 ký tự, 200 bài mỗi danh sách, 1 và 72 giờ, tin
// riêng đúng 2 000 ký tự → lưu được; không câu nào, 6 câu, câu 501 ký tự, 201 bài, 0 hay 73 giờ → ErrInvalidRule.
// (Task 182, mutation testing M4)
func TestMutationM4_ReplyConfigLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	posts := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s_%d", pageExt, 800000000000000+i)
		}
		return out
	}
	cases := []struct {
		name string
		edit func(c *automation.ReplyConfig)
		ok   bool
	}{
		{"5 câu", func(c *automation.ReplyConfig) { c.Replies = []string{"a", "b", "c", "d", "e"} }, true},
		{"câu 500 ký tự", func(c *automation.ReplyConfig) { c.Replies = []string{strings.Repeat("ữ", 500)} }, true},
		{"200 bài", func(c *automation.ReplyConfig) { c.PostIDs = posts(200) }, true},
		{"200 bài ngoại lệ", func(c *automation.ReplyConfig) { c.ExceptPostIDs = posts(200) }, true},
		{"1 giờ", func(c *automation.ReplyConfig) { c.PerUserPerPostHours = 1 }, true},
		{"72 giờ", func(c *automation.ReplyConfig) { c.PerUserPerPostHours = 72 }, true},
		{"tin riêng 2000 ký tự", func(c *automation.ReplyConfig) {
			c.PrivateMessage = &automation.PrivateMessageConfig{Text: strings.Repeat("ữ", 2000)}
		}, true},
		{"không câu", func(c *automation.ReplyConfig) { c.Replies = []string{} }, false},
		{"6 câu", func(c *automation.ReplyConfig) { c.Replies = []string{"a", "b", "c", "d", "e", "f"} }, false},
		{"câu 501 ký tự", func(c *automation.ReplyConfig) { c.Replies = []string{strings.Repeat("ữ", 501)} }, false},
		{"201 bài", func(c *automation.ReplyConfig) { c.PostIDs = posts(201) }, false},
		{"201 bài ngoại lệ", func(c *automation.ReplyConfig) { c.ExceptPostIDs = posts(201) }, false},
		{"0 giờ", func(c *automation.ReplyConfig) { c.PerUserPerPostHours = 0 }, false},
		{"73 giờ", func(c *automation.ReplyConfig) { c.PerUserPerPostHours = 73 }, false},
	}
	for _, c := range cases {
		cfg := replyCfg()
		c.edit(&cfg)
		err := e.saveReply(true, cfg)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, automation.ErrInvalidRule)) {
			t.Errorf("%s: lỗi %v", c.name, err)
		}
	}
}

// Quy tắc bỏ tin riêng sau khi comment tới mà trước khi việc chuẩn bị chạy → lượt "pm:" dropped, Meta không nhận tin
// riêng; trả lời công khai vẫn done. (Task 182, mutation testing M4)
func TestMutationM4_PrivateRemovedBeforePrepare(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	c := e.comment(postA, userA, "giá sao chị", time.Now().UTC().Add(-time.Minute))
	e.enqueue(c, webhooks.ViaWebhook)
	release := e.inboundOnly()
	cfg := replyCfg()
	cfg.PrivateMessage = nil
	e.setReply(true, cfg)
	release()
	e.drain()
	if out, n := e.outcomeOf("pm:" + c.CommentID); out != string(automation.OutcomeDropped) || n != 1 {
		t.Fatalf("lượt tin riêng sau khi bỏ tin riêng = %q (%d dòng), muốn dropped", out, n)
	}
	if n := e.privateCalls(c.CommentID); n != 0 {
		t.Fatalf("Meta nhận %d tin riêng, muốn 0", n)
	}
	if out, _ := e.outcomeOf("reply:" + c.CommentID); out != string(automation.OutcomeDone) {
		t.Fatalf("lượt trả lời công khai = %q, muốn done", out)
	}
}

// Biên đúng của tin chào: chữ đúng 640 ký tự, payload nút đúng 1 000 ký tự → lưu được. (Task 182, mutation testing M4)
func TestMutationM4_GreetingExactLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for name, g := range map[string]automation.GreetingConfig{
		"chữ 640 ký tự":      {Kind: automation.KindGreeting, Text: strings.Repeat("ữ", 640), QuickReplies: []automation.GreetingQuickReply{}},
		"payload 1000 ký tự": {Kind: automation.KindGreeting, Text: greetText, QuickReplies: []automation.GreetingQuickReply{{Title: "A", Payload: strings.Repeat("ữ", 1000)}}},
	} {
		cfg, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.storeRule(automation.KindGreeting, true, cfg); err != nil {
			t.Errorf("%s: lỗi %v", name, err)
		}
	}
}

// SaveRule: kind trong cấu hình khác loại trên đường dẫn (JSON đọc được) → ErrInvalidRule; auto_hide 200 bài ngoại lệ
// lưu được, 201 → ErrInvalidRule; dòng đã lưu ghi người sửa (updated_by) là người gọi. (Task 182, mutation testing M4)
func TestMutationM4_SaveRuleGates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	wrong := []byte(`{"kind":"greeting","mode":"off","exceptPostIds":[]}`)
	if err := e.storeRule(automation.KindAutoHide, true, wrong); !errors.Is(err, automation.ErrInvalidRule) {
		t.Fatalf("kind khác loại: lỗi %v, muốn ErrInvalidRule", err)
	}
	except := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s_%d", pageExt, 700000000000000+i)
		}
		return out
	}
	for n, ok := range map[int]bool{200: true, 201: false} {
		cfg, err := json.Marshal(automation.HideConfig{Kind: automation.KindAutoHide, Mode: automation.HideAll, ExceptPostIDs: except(n)})
		if err != nil {
			t.Fatal(err)
		}
		saveErr := e.storeRule(automation.KindAutoHide, true, cfg)
		if ok != (saveErr == nil) || (!ok && !errors.Is(saveErr, automation.ErrInvalidRule)) {
			t.Errorf("%d bài ngoại lệ: lỗi %v", n, saveErr)
		}
	}
	var by *uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT updated_by FROM conversation.automation_rules
			WHERE channel_account_id = $1 AND kind = $2`, e.page, automation.KindAutoHide).Scan(&by)
	})
	if by == nil || *by != e.user {
		t.Fatalf("updated_by = %v, muốn %v", by, e.user)
	}
}

// ListFollowUps nhận afterHours đúng 1 và đúng 23 (người xem không có Page nào: rỗng, không đọc database); 0, 24 →
// ErrInvalidFollowUp. (Task 182, mutation testing M4)
func TestMutationM4_ListFollowUpsHourBounds(t *testing.T) {
	t.Parallel()
	u := readerOf(uuid.New())
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	for h, ok := range map[int]bool{1: true, 23: true, 0: false, 24: false} {
		got, err := automation.ListFollowUps(t.Context(), nil, u, nil, h, 50, now)
		switch {
		case ok && (err != nil || len(got) != 0):
			t.Errorf("afterHours %d: %v, lỗi %v; muốn rỗng, không lỗi", h, got, err)
		case !ok && !errors.Is(err, automation.ErrInvalidFollowUp):
			t.Errorf("afterHours %d: lỗi %v, muốn ErrInvalidFollowUp", h, err)
		}
	}
}

// ListFollowUps với limit 0 → mặc định (đủ hai hội thoại), limit 1 → đúng một. (Task 182, mutation testing M4)
func TestMutationM4_ListFollowUpsLimit(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	f.inbox(f.page, "psid-l1", "Chị Lan", 5*time.Hour, 4*time.Hour)
	f.inbox(f.page, "psid-l2", "Chị Liên", 5*time.Hour, 4*time.Hour)
	u := readerOf(f.tenant, f.page)
	for limit, want := range map[int]int{0: 2, 1: 1} {
		var got []automation.FollowUpItem
		f.staff.Exec(t, f.tenant, func(tx pgx.Tx) (err error) {
			got, err = automation.ListFollowUps(t.Context(), tx, u, nil, 3, limit, f.now())
			return err //nolint:wrapcheck // test
		})
		if len(got) != want {
			t.Errorf("limit %d: %d hội thoại, muốn %d", limit, len(got), want)
		}
	}
}

// saveFollowUpHours lưu quy tắc follow_up với afterHours hours, mẫu của env, bật/tắt enabled; trả lỗi của SaveRule.
func (f *fuEnv) saveFollowUpHours(enabled bool, hours int) error {
	f.t.Helper()
	cfg, err := json.Marshal(automation.FollowUpConfig{Kind: automation.KindFollowUp, AfterHours: hours, TemplateID: f.template})
	if err != nil {
		f.t.Fatal(err)
	}
	return f.storeRule(automation.KindFollowUp, enabled, cfg)
}

// follow_up: lưu afterHours 1 và 23 được (tắt), 0 và 24 → ErrInvalidRule; lịch chạy quy tắc bật afterHours 23 (khách
// im 23 giờ 10 phút) và afterHours 1 (im 1 giờ 10 phút) → mỗi lần đúng 1 lệnh; cấu hình trong database bị sửa thành
// afterHours 0 → lịch coi như tắt (0 lệnh dù có hội thoại vừa im 10 phút). (Task 182, mutation testing M4)
func TestMutationM4_FollowUpHourBounds(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	for h, ok := range map[int]bool{1: true, 23: true, 0: false, 24: false} {
		err := f.saveFollowUpHours(false, h)
		if ok != (err == nil) || (!ok && !errors.Is(err, automation.ErrInvalidRule)) {
			t.Errorf("lưu afterHours %d: lỗi %v", h, err)
		}
	}
	if err := f.saveFollowUpHours(true, 23); err != nil {
		t.Fatal(err)
	}
	f.inbox(f.page, "psid-h23", "Chị Hằng", 23*time.Hour+30*time.Minute, 23*time.Hour+10*time.Minute)
	if n := f.sweep(); n != 1 {
		t.Fatalf("afterHours 23: %d lệnh, muốn 1", n)
	}
	if err := f.saveFollowUpHours(true, 1); err != nil {
		t.Fatal(err)
	}
	f.inbox(f.page, "psid-h1", "Chị Hoa", 2*time.Hour, time.Hour+10*time.Minute)
	if n := f.sweep(); n != 1 {
		t.Fatalf("afterHours 1: %d lệnh, muốn 1", n)
	}
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.automation_rules SET config = jsonb_set(config, '{afterHours}', '0')
			WHERE channel_account_id = $1 AND kind = $2`, f.page, automation.KindFollowUp)
		return err //nolint:wrapcheck // test
	})
	f.inbox(f.page, "psid-h0", "Chị Hạnh", 30*time.Minute, 10*time.Minute)
	if n := f.sweep(); n != 0 {
		t.Fatalf("afterHours 0 trong database: %d lệnh, muốn 0", n)
	}
}
