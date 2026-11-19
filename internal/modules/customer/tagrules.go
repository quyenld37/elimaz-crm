package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Quy tắc tự gắn nhãn theo chỉ số khách (kế hoạch M2, Task M2-25). Một nhãn có tối đa một quy tắc: danh sách 1–10 điều
// kiện nối bằng VÀ trên chỉ số của hồ sơ gốc. Đạt → dòng customer_tag_links source rule trên gốc; không còn đạt (hay
// quy tắc tắt) → gỡ dòng rule. Dòng source staff không bao giờ bị đụng.

// Trường của điều kiện (TagCondition.Field).
const (
	FieldOrderCount         = "order_count"
	FieldDeliveredCount     = "delivered_count"
	FieldReturnedCount      = "returned_count"
	FieldTotalSpent         = "total_spent"
	FieldDaysSinceLastOrder = "days_since_last_order"
	FieldReturnRateBP       = "return_rate_bp"
)

// Phép so sánh của điều kiện (TagCondition.Op): giá trị của khách ≥ Value, ≤ Value.
const (
	OpGte = "gte"
	OpLte = "lte"
)

const (
	maxTagConditions = 10
	// maxConditionValue: số nguyên lớn nhất JavaScript giữ đúng (maximum của TagCondition trong openapi.yaml).
	maxConditionValue = 1<<53 - 1
	aggregateTagRule  = "customer_tag_rule"
	day               = 24 * time.Hour

	// TagRulesNightlySchedule là tên lịch duyệt khách gốc mỗi đêm (platform.schedules), theo tenant.
	TagRulesNightlySchedule = "customer.tag_rules_nightly"
	tagRulesEvery           = 86400 * time.Second
	tagRulesBatch           = 1000
)

// TagCondition là một điều kiện: Field (Field*), Op (OpGte, OpLte), Value (0 … 2^53−1).
type TagCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value int64  `json:"value"`
}

// TagRule là quy tắc của nhãn TagID.
type TagRule struct {
	TagID      uuid.UUID
	Enabled    bool
	Conditions []TagCondition
	Version    int64
}

// TagRuleInput là dữ liệu đặt quy tắc. Version nil khi nhãn chưa có quy tắc; có quy tắc thì phải khớp version hiện tại.
type TagRuleInput struct {
	Version    *int64
	Enabled    bool
	Conditions []TagCondition
}

// GetTagRule trả quy tắc của nhãn tagID; nhãn chưa có quy tắc (hay nhãn của tenant khác) → ErrNotFound.
func GetTagRule(ctx context.Context, tx pgx.Tx, tagID uuid.UUID) (TagRule, error) {
	r, err := store.New(tx).GetTagRule(ctx, tagID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TagRule{}, ErrNotFound
	}
	if err != nil {
		return TagRule{}, fmt.Errorf("đọc quy tắc nhãn: %w", err)
	}
	return ruleOf(r.TagID, r.Conditions, r.Enabled, r.Version)
}

func ruleOf(tagID uuid.UUID, conds []byte, enabled bool, version int64) (TagRule, error) {
	r := TagRule{TagID: tagID, Enabled: enabled, Version: version}
	if err := json.Unmarshal(conds, &r.Conditions); err != nil {
		return TagRule{}, fmt.Errorf("đọc điều kiện của quy tắc nhãn %s: %w", tagID, err)
	}
	return r, nil
}

// PutTagRule đặt quy tắc của nhãn tagID trong tx của bên gọi: in.Version nil → tạo (nhãn đã có quy tắc →
// ErrVersionConflict); có → sửa khi version còn khớp (lệch hay chưa có quy tắc → ErrVersionConflict). Điều kiện rỗng,
// quá 10, trường hay phép lạ, giá trị ngoài 0 … 2^53−1 → ErrInvalidTagRule; nhãn không có → ErrNotFound. Ghi audit
// customer_tag_rule.put (số điều kiện, bật/tắt, version). Khách hiện có được áp quy tắc mới ở sự kiện kế tiếp của họ
// hoặc ở lịch đêm.
func (w Writer) PutTagRule(ctx context.Context, tx pgx.Tx, tagID uuid.UUID, in TagRuleInput, a Actor, now time.Time,
) (TagRule, error) {
	if err := validRule(in.Conditions); err != nil {
		return TagRule{}, err
	}
	if err := tagExists(ctx, tx, tagID); err != nil {
		return TagRule{}, err
	}
	conds, err := json.Marshal(in.Conditions)
	if err != nil {
		return TagRule{}, fmt.Errorf("mã hóa điều kiện của quy tắc nhãn: %w", err)
	}
	q := store.New(tx)
	var tenantID uuid.UUID
	var out TagRule
	if in.Version == nil {
		r, err := q.InsertTagRule(ctx, store.InsertTagRuleParams{TagID: tagID, Conditions: conds, Enabled: in.Enabled, Now: now})
		if errors.Is(err, pgx.ErrNoRows) {
			return TagRule{}, ErrVersionConflict
		}
		if err != nil {
			return TagRule{}, fmt.Errorf("tạo quy tắc nhãn: %w", err)
		}
		tenantID = r.TenantID
		out, err = ruleOf(r.TagID, r.Conditions, r.Enabled, r.Version)
		if err != nil {
			return TagRule{}, err
		}
	} else {
		r, err := q.UpdateTagRule(ctx, store.UpdateTagRuleParams{
			Conditions: conds, Enabled: in.Enabled, Now: now, TagID: tagID, Version: *in.Version,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return TagRule{}, ErrVersionConflict
		}
		if err != nil {
			return TagRule{}, fmt.Errorf("sửa quy tắc nhãn: %w", err)
		}
		tenantID = r.TenantID
		out, err = ruleOf(r.TagID, r.Conditions, r.Enabled, r.Version)
		if err != nil {
			return TagRule{}, err
		}
	}
	after := map[string]any{"enabled": out.Enabled, "conditions": len(out.Conditions), "version": out.Version}
	if err := w.auditEntity(ctx, tx, tenantID, a, "customer_tag_rule.put", aggregateTagRule, tagID, nil, after); err != nil {
		return TagRule{}, err
	}
	return out, nil
}

func validRule(conds []TagCondition) error {
	if len(conds) == 0 || len(conds) > maxTagConditions {
		return ErrInvalidTagRule
	}
	fields := []string{
		FieldOrderCount, FieldDeliveredCount, FieldReturnedCount, FieldTotalSpent, FieldDaysSinceLastOrder, FieldReturnRateBP,
	}
	for _, c := range conds {
		if !slices.Contains(fields, c.Field) || (c.Op != OpGte && c.Op != OpLte) || c.Value < 0 || c.Value > maxConditionValue {
			return ErrInvalidTagRule
		}
	}
	return nil
}

// value trả giá trị của trường field cho chỉ số s ở giờ now; false khi không có giá trị (số ngày từ đơn cuối của khách
// chưa có đơn nào: "lâu không mua" không áp cho khách chưa mua).
func (s Stats) value(field string, now time.Time) (int64, bool) {
	switch field {
	case FieldOrderCount:
		return int64(s.OrderCount), true
	case FieldDeliveredCount:
		return int64(s.DeliveredCount), true
	case FieldReturnedCount:
		return int64(s.ReturnedCount), true
	case FieldTotalSpent:
		return s.TotalSpent, true
	case FieldReturnRateBP:
		return int64(s.ReturnRateBP), true
	case FieldDaysSinceLastOrder:
		if s.LastOrderAt == nil {
			return 0, false
		}
		return max(int64(now.Sub(*s.LastOrderAt)/day), 0), true
	}
	return 0, false
}

// matches: quy tắc bật và mọi điều kiện đạt với chỉ số s ở giờ now.
func (r TagRule) matches(s Stats, now time.Time) bool {
	if !r.Enabled || len(r.Conditions) == 0 {
		return false
	}
	for _, c := range r.Conditions {
		v, ok := s.value(c.Field, now)
		if !ok || (c.Op == OpGte && v < c.Value) || (c.Op == OpLte && v > c.Value) || (c.Op != OpGte && c.Op != OpLte) {
			return false
		}
	}
	return true
}

func loadTagRules(ctx context.Context, tx pgx.Tx) ([]TagRule, error) {
	rows, err := store.New(tx).ListTagRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc quy tắc nhãn: %w", err)
	}
	out := make([]TagRule, 0, len(rows))
	for _, r := range rows {
		rule, err := ruleOf(r.TagID, r.Conditions, r.Enabled, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

// ApplyTagRules áp mọi quy tắc nhãn cho nhóm chứa customerID (gốc hay hồ sơ con; áp cho gốc hiện tại) theo chỉ số đang
// lưu ở gốc, trong tx của bên gọi: thêm dòng source rule trên gốc khi đạt, gỡ dòng source rule (trên mọi hồ sơ của
// nhóm) khi không còn đạt; dòng source staff giữ nguyên. Khóa cả nhóm (ADR-46 nhóm 5). Không có → ErrNotFound.
func (w Writer) ApplyTagRules(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, now time.Time) error {
	g, err := lockGroups(ctx, tx, []uuid.UUID{customerID})
	if err != nil {
		return err
	}
	rules, err := loadTagRules(ctx, tx)
	if err != nil {
		return err
	}
	root := g.roots[0]
	s, err := StatsFor(ctx, tx, root)
	if err != nil {
		return err
	}
	return w.applyRules(ctx, tx, g.tenantID, root, g.members[root], s, rules, now)
}

// applyRules đưa dòng source rule của các hồ sơ scope (bên gọi đã khóa; gồm root) về đúng tập nhãn mà rules đạt với
// chỉ số s của gốc root: chỉ gốc giữ dòng rule. Có đổi thì audit customer.tag_add/tag_remove (người làm system, mã
// nhãn) và báo hiệu gốc.
func (w Writer) applyRules(ctx context.Context, tx pgx.Tx, tenantID, root uuid.UUID, scope []uuid.UUID, s Stats,
	rules []TagRule, now time.Time,
) error {
	want := map[uuid.UUID]bool{}
	for _, r := range rules {
		if r.matches(s, now) {
			want[r.TagID] = true
		}
	}
	removed, kept, err := w.removeStaleRuleLinks(ctx, tx, tenantID, root, scope, want)
	if err != nil {
		return err
	}
	add := make([]uuid.UUID, 0, len(want))
	for id := range want {
		if !kept[id] {
			add = append(add, id)
		}
	}
	added, err := w.addRuleLinks(ctx, tx, tenantID, root, sortIDs(add), now)
	if err != nil {
		return err
	}
	changed := removed || added
	if !changed {
		return nil
	}
	return w.signal(ctx, tx, tenantID, root)
}

// removeStaleRuleLinks gỡ mọi dòng rule của scope trừ dòng của gốc root với nhãn trong want; trả có gỡ dòng nào không
// và các nhãn want mà gốc đã có dòng rule.
func (w Writer) removeStaleRuleLinks(ctx context.Context, tx pgx.Tx, tenantID, root uuid.UUID, scope []uuid.UUID,
	want map[uuid.UUID]bool,
) (bool, map[uuid.UUID]bool, error) {
	q := store.New(tx)
	links, err := q.ListRuleTagLinks(ctx, scope)
	if err != nil {
		return false, nil, fmt.Errorf("đọc nhãn theo quy tắc: %w", err)
	}
	removed, kept := false, map[uuid.UUID]bool{}
	for _, l := range links {
		if l.CustomerID == root && want[l.TagID] {
			kept[l.TagID] = true
			continue
		}
		if err := q.DeleteRuleTagLink(ctx, store.DeleteRuleTagLinkParams(l)); err != nil {
			return false, nil, fmt.Errorf("gỡ nhãn theo quy tắc: %w", err)
		}
		if err := w.audit(ctx, tx, tenantID, Actor{Type: ActorSystem}, "customer.tag_remove", root,
			nil, map[string]any{"tag_id": l.TagID.String(), "source": TagSourceRule}); err != nil {
			return false, nil, err
		}
		removed = true
	}
	return removed, kept, nil
}

// addRuleLinks gắn dòng rule của các nhãn tags lên gốc root; gốc đã có nhãn gắn tay thì giữ dòng staff. Trả có gắn
// dòng nào không.
func (w Writer) addRuleLinks(ctx context.Context, tx pgx.Tx, tenantID, root uuid.UUID, tags []uuid.UUID, now time.Time,
) (bool, error) {
	q := store.New(tx)
	system := Actor{Type: ActorSystem}
	added := false
	for _, tagID := range tags {
		_, err := q.InsertTagLink(ctx, store.InsertTagLinkParams{
			CustomerID: root, TagID: tagID, Source: TagSourceRule, CreatedBy: system.createdBy(), Now: now.UTC(),
		})
		if errors.Is(err, pgx.ErrNoRows) { // gốc đã có nhãn gắn tay: giữ dòng staff
			continue
		}
		if err != nil {
			return false, fmt.Errorf("gắn nhãn theo quy tắc: %w", err)
		}
		if err := w.audit(ctx, tx, tenantID, system, "customer.tag_add", root,
			nil, map[string]any{"tag_id": tagID.String(), "source": TagSourceRule}); err != nil {
			return false, err
		}
		added = true
	}
	return added, nil
}

// ApplyTagRulesBatch áp mọi quy tắc nhãn (quy tắc có days_since_last_order đổi theo ngày dù không có sự kiện; quy tắc
// mới đặt, vừa tắt cũng tới mọi khách) cho tối đa limit hồ sơ gốc có id > after, theo id tăng dần, trong tx của bên gọi.
// Khóa từng gốc trong một câu theo id (ADR-46 nhóm 5); chỉ đụng dòng rule của chính gốc (dòng rule còn sót ở hồ sơ con
// sau gộp do consumer customer_stats dọn khi tính lại nhóm). Trả gốc cuối đã xử lý và số gốc; tenant chưa có quy tắc
// nào → 0. limit ngoài 1 … 2^31−1 → ErrInvalidInput.
func (w Writer) ApplyTagRulesBatch(ctx context.Context, tx pgx.Tx, after uuid.UUID, limit int, now time.Time,
) (uuid.UUID, int, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		return uuid.Nil, 0, ErrInvalidInput
	}
	rules, err := loadTagRules(ctx, tx)
	if err != nil || len(rules) == 0 {
		return uuid.Nil, 0, err
	}
	rows, err := store.New(tx).LockRootsAfter(ctx, store.LockRootsAfterParams{After: after, MaxRows: int32(limit)})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("khóa lô khách gốc: %w", err)
	}
	for _, r := range rows {
		s := statsOf(r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent, r.LastOrderAt)
		if err := w.applyRules(ctx, tx, r.TenantID, r.ID, []uuid.UUID{r.ID}, s, rules, now); err != nil {
			return uuid.Nil, 0, err
		}
	}
	if len(rows) == 0 {
		return uuid.Nil, 0, nil
	}
	return rows[len(rows)-1].ID, len(rows), nil
}

// tagRulesKind: việc của lịch đêm (và các lô nối tiếp); quá một kỳ chưa chạy thì expired (kỳ sau duyệt lại từ đầu).
func tagRulesKind() queue.Kind {
	return queue.Kind{
		Name: "task." + TagRulesNightlySchedule, Policy: queue.Policy{MaxAttempts: 3, ExpiresAfter: tagRulesEvery},
	}
}

func tagRulesSchedule() schedules.Schedule {
	return schedules.Schedule{Name: TagRulesNightlySchedule, Every: tagRulesEvery, PerTenant: true, Kind: tagRulesKind()}
}

// nightlyPayload: kỳ của lịch (do schedules ghi) và gốc cuối của lô trước (nil: lô đầu).
type nightlyPayload struct {
	Slot  time.Time  `json:"slot"`
	After *uuid.UUID `json:"after,omitempty"`
}

// nightlyHandler chạy một lô ApplyTagRulesBatch; lô đầy thì ghi việc nối tiếp (cùng kỳ, after = gốc cuối) trong cùng tx.
type nightlyHandler struct {
	w     Writer
	q     queue.JobQueue
	now   func() time.Time
	batch int
}

func (nightlyHandler) Kind() queue.Kind { return tagRulesKind() }

// Handle áp quy tắc cho một lô gốc; kết quả {"processed": n}.
func (h nightlyHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var p nightlyPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: payload hoặc tenant không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	after := uuid.Nil
	if p.After != nil {
		after = *p.After
	}
	last, n, err := h.w.ApplyTagRulesBatch(ctx, tx, after, h.batch, h.now())
	if err != nil {
		return nil, err
	}
	if n == h.batch {
		next := nightlyPayload{Slot: p.Slot, After: &last}
		if _, _, err := h.q.Enqueue(ctx, tx, tagRulesKind(), queue.NewJob{
			TenantID: j.TenantID, Payload: next,
			DedupeKey: TagRulesNightlySchedule + ":" + p.Slot.UTC().Format(time.RFC3339) + ":" + last.String(),
		}); err != nil {
			return nil, fmt.Errorf("ghi lô nối tiếp của lịch nhãn: %w", err)
		}
	}
	out, err := json.Marshal(map[string]int{"processed": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả lịch nhãn: %w", err)
	}
	return out, nil
}
