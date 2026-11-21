package order

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Loại mục của lịch sử thao tác đơn (kind là chuỗi mở trong openapi.yaml TimelineItem: "Đơn: status, edit, note; M3
// thêm shipment, payment, return"). TimelineShipment, TimelinePayment do internal/queries dựng (view của fulfillment,
// cash), lớp HTTP trộn vào rồi gọi SortTimeline.
const (
	TimelineStatus   = "status"
	TimelineEdit     = "edit"
	TimelineNote     = "note"
	TimelineShipment = "shipment"
	TimelinePayment  = "payment"
	TimelineReturn   = "return"
)

// Bước của mục return (Summary["step"]): phiếu được tạo, hàng trả đã nhận.
const (
	returnStepCreated  = "created"
	returnStepReceived = "received"
)

// fieldChanged là giá trị của một trường đã sửa trong Summary của mục edit (màn hình hiện "đã đổi").
const fieldChanged = "changed"

// AuditChange là một dòng audit của đơn mà lớp gọi đọc được (order không nhập identity, 04 mục 3): người làm (nil khi
// không rõ), việc, tên trường đã sửa do bên ghi liệt kê (audit order.update: after.fields = EditResult.Fields), lúc ghi.
type AuditChange struct {
	ID      uuid.UUID
	Action  string
	ActorID *uuid.UUID
	Edited  []string
	At      time.Time
}

// AuditSource đọc audit của đơn orderID trong tx (lớp HTTP nối bằng identity.ListEntityAudit). nil: không có audit.
type AuditSource func(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]AuditChange, error)

// OrderTimelineItem là một mục của lịch sử thao tác đơn. Kind là TimelineStatus (RefID: dòng lịch sử trạng thái),
// TimelineEdit (RefID: dòng audit), TimelineNote (RefID: ghi chú), TimelineReturn (RefID: phiếu trả), TimelineShipment,
// TimelinePayment (lớp HTTP trộn từ internal/queries). ActorID nil khi không rõ người làm; ActorType là staff, system,
// carrier, customer, service (I09: mọi mục đều có nguồn; edit, note là staff). Summary chỉ mang trạng thái, lý do, tên trường đã đổi
// (giá trị "changed") — không giá trị dữ liệu cá nhân; Note là nội dung ghi chú (chỉ ở note).
type OrderTimelineItem struct { //nolint:revive // tên theo Interfaces của kế hoạch M2 (Task M2-24), task sau dùng
	At        time.Time
	Kind      string
	RefID     uuid.UUID
	ActorID   *uuid.UUID
	ActorType string
	Summary   map[string]string
	Note      NoteBody
}

// Timeline trả lịch sử thao tác của đơn id của người bán sellerID (ADR-29), mới nhất trước (SortTimeline): mọi dòng lịch
// sử trạng thái (tạo đơn, đổi trạng thái, đặt bỏ lý do chờ, hủy), mỗi audit có liệt kê trường đã sửa (lần sửa đơn)
// thành một mục edit liệt kê tên trường (không giá trị), mọi ghi chú, mỗi phiếu trả một mục lúc tạo (người tạo; trống →
// system) và một mục lúc nhận hàng (người nhận) — Summary: code, kind, status, step. Audit khác (tạo, xác nhận,
// hủy, xử lý đơn trùng) không thành mục: việc đó đã có ở lịch sử trạng thái. Không có đơn → ErrNotFound. Tên người làm do bên gọi đọc (order không nhập identity).
func Timeline(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, src AuditSource) ([]OrderTimelineItem, error) {
	if err := orderOfSeller(ctx, tx, sellerID, id); err != nil {
		return nil, err
	}
	q := store.New(tx)
	hist, err := q.ListOrderHistory(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử trạng thái đơn: %w", err)
	}
	notes, err := q.ListOrderNotes(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc ghi chú đơn: %w", err)
	}
	var audits []AuditChange
	if src != nil {
		if audits, err = src(ctx, tx, id); err != nil {
			return nil, fmt.Errorf("đọc audit của đơn: %w", err)
		}
	}
	returns, err := q.ListOrderReturnsForTimeline(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc phiếu trả của đơn: %w", err)
	}
	out := make([]OrderTimelineItem, 0, len(hist)+len(notes)+len(audits)+2*len(returns))
	for _, h := range hist {
		out = append(out, OrderTimelineItem{
			At: h.OccurredAt, Kind: TimelineStatus, RefID: h.ID, ActorID: uuidPtr(h.ActorID), ActorType: h.ActorType,
			Summary: nonEmptyMap(map[string]string{
				"from": h.FromStatus, "to": h.ToStatus, "fromPending": h.FromPending, "toPending": h.ToPending,
				"reason": h.Reason,
			}),
		})
	}
	for _, a := range audits {
		if it, ok := editItem(a); ok {
			out = append(out, it)
		}
	}
	for _, n := range notes {
		author := n.AuthorID
		out = append(out, OrderTimelineItem{
			At: n.CreatedAt, Kind: TimelineNote, RefID: n.ID, ActorID: &author, ActorType: inventory.ActorStaff,
			Summary: map[string]string{}, Note: NoteBody(n.Body),
		})
	}
	for _, r := range returns {
		out = append(out, returnItem(r.ID, r.Code, r.Kind, r.Status, returnStepCreated, r.CreatedAt, uuidPtr(r.CreatedBy)))
		if r.ReceivedAt.Valid {
			out = append(out, returnItem(r.ID, r.Code, r.Kind, r.Status, returnStepReceived, r.ReceivedAt.Time,
				uuidPtr(r.ReceivedBy)))
		}
	}
	SortTimeline(out)
	return out, nil
}

// SortTimeline sắp items mới nhất trước; cùng lúc: theo Kind rồi RefID giảm. Lớp HTTP gọi lại sau khi trộn mục vận
// đơn, thu tiền (một quy tắc sắp cho cả dòng thời gian).
func SortTimeline(items []OrderTimelineItem) {
	slices.SortFunc(items, func(x, y OrderTimelineItem) int {
		return cmp.Or(y.At.Compare(x.At), cmp.Compare(y.Kind, x.Kind), slices.Compare(y.RefID[:], x.RefID[:]))
	})
}

// returnItem dựng mục return của phiếu id ở bước step; người làm nil → system.
func returnItem(id uuid.UUID, code, kind, status, step string, at time.Time, actor *uuid.UUID) OrderTimelineItem {
	it := OrderTimelineItem{
		At: at, Kind: TimelineReturn, RefID: id, ActorID: actor, ActorType: inventory.ActorStaff,
		Summary: map[string]string{"code": code, "kind": kind, "status": status, "step": step},
	}
	if actor == nil {
		it.ActorType = inventory.ActorSystem
	}
	return it
}

// editItem dựng mục edit từ audit a: chỉ audit có liệt kê trường đã sửa.
func editItem(a AuditChange) (OrderTimelineItem, bool) {
	sum := map[string]string{}
	for _, f := range a.Edited {
		if f != "" && f != "action" {
			sum[f] = fieldChanged
		}
	}
	if len(sum) == 0 {
		return OrderTimelineItem{}, false
	}
	sum["action"] = a.Action
	it := OrderTimelineItem{
		At: a.At, Kind: TimelineEdit, RefID: a.ID, ActorID: a.ActorID, ActorType: inventory.ActorStaff, Summary: sum,
	}
	if a.ActorID == nil {
		it.ActorType = inventory.ActorSystem
	}
	return it, true
}

// nonEmptyMap bỏ khóa có giá trị rỗng.
func nonEmptyMap(m map[string]string) map[string]string {
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	return m
}
