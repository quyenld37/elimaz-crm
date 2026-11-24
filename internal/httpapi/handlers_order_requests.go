package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/files"
)

// Yêu cầu về đơn hàng — phía nhân viên (kế hoạch M5, Task 195). Đọc cần order.requests.read, ghi cần
// order.requests.manage (bảng route). Audit ghi ở đây, cùng tx (order không nhập identity); nội dung tin không vào
// audit, chỉ tên trường.

// requestPhotoTTL: hạn link ký tới ảnh khách (kế hoạch M5-12, R10).
const requestPhotoTTL = 10 * time.Minute

// ListOrderRequests (GET /api/v1/order-requests, order.requests.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOrderRequests(ctx context.Context, params gen.ListOrderRequestsParams) (gen.ListOrderRequestsRes, error) {
	f := order.RequestFilter{
		Status: string(params.Status.Or("")), Kind: string(params.Kind.Or("")), Q: params.Q.Or(""),
		Limit: int(params.Limit.Or(0)),
	}
	if c, ok := params.Cursor.Get(); ok {
		cur, err := order.ParseCursor(c)
		if err != nil {
			return nil, fmt.Errorf("đọc con trỏ yêu cầu: %w", err)
		}
		f.After = &cur
	}
	page := &gen.OrderRequestPage{Items: []gen.OrderRequestSummary{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return fmt.Errorf("người bán của đơn web: %w", err)
		}
		rows, next, err := order.ListRequests(ctx, tx, seller, f)
		if err != nil {
			return err //nolint:wrapcheck // order đã bọc
		}
		page.Items = page.Items[:0]
		for _, r := range rows {
			page.Items = append(page.Items, gen.OrderRequestSummary{
				ID: r.ID, Ref: r.Ref, OrderId: r.OrderID, OrderCode: r.OrderCode, Kind: gen.OrderRequestKind(r.Kind),
				Status: gen.OrderRequestStatus(r.Status), ReasonCode: r.ReasonCode, CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt, Version: int32(r.Version), //nolint:gosec // G115: version int4 của bảng
			})
		}
		page.NextCursor = gen.OptString{}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách yêu cầu về đơn: %w", err)
	}
	return page, nil
}

// GetOrderRequest (GET /api/v1/order-requests/{id}, order.requests.read): ảnh kèm link ký 10 phút (files.DownloadURL
// của M1, sau khi route đã kiểm quyền).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOrderRequest(ctx context.Context, params gen.GetOrderRequestParams) (gen.GetOrderRequestRes, error) {
	var out *gen.OrderRequest
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = h.readOrderRequest(ctx, tx, params.ID)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc yêu cầu về đơn: %w", err)
	}
	return out, nil
}

// ReplyOrderRequest (POST /api/v1/order-requests/{id}/messages, order.requests.manage): tin cho khách hay ghi chú nội
// bộ; audit order.request.reply chỉ ghi tên trường.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReplyOrderRequest(ctx context.Context, req *gen.ReplyOrderRequestRequest,
	params gen.ReplyOrderRequestParams,
) (gen.ReplyOrderRequestRes, error) {
	w := order.Writer{Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isOrderRequestRefusal, "trả lời yêu cầu về đơn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			m, err := w.ReplyRequest(ctx, tx, params.ID, req.Body, req.VisibleToCustomer,
				order.Actor{TenantID: u.TenantID, UserID: u.UserID}, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			names, err := staffNames(ctx, tx, []uuid.UUID{u.UserID})
			if err != nil {
				return nil, err
			}
			return messageOut(m, names), h.audit(ctx, tx, u, "order.request.reply", "order_request", params.ID, nil,
				map[string]any{
					"changed_fields": []string{"body"}, "visibleToCustomer": m.VisibleToCustomer,
					"messageId": m.ID.String(),
				})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OrderRequestMessage), nil //nolint:forcetypeassert // fn trên trả *gen.OrderRequestMessage
}

// SetOrderRequestStatus (POST /api/v1/order-requests/{id}/status, order.requests.manage): đổi trạng thái theo sơ đồ
// khi version khớp; audit order.request.status (trạng thái, version; tin, hướng dẫn, kết quả chỉ ghi tên trường).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetOrderRequestStatus(ctx context.Context, req *gen.SetOrderRequestStatusRequest,
	params gen.SetOrderRequestStatusParams,
) (gen.SetOrderRequestStatusRes, error) {
	ch := order.StatusChange{Status: string(req.Status), Message: req.Message.Or("")}
	if sb, ok := req.ShipBack.Get(); ok {
		ch.ShipBack = &order.ShipBackInput{Address: sb.Address, PackageCode: sb.PackageCode, Deadline: sb.Deadline}
	}
	if oc, ok := req.Outcome.Get(); ok {
		ch.Outcome = &order.Outcome{TrackingCode: oc.TrackingCode.Or(""), Reason: oc.Reason.Or("")}
	}
	w := order.Writer{Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isOrderRequestRefusal, "đổi trạng thái yêu cầu về đơn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			c, err := w.SetRequestStatus(ctx, tx, params.ID, ch, int(req.Version),
				order.Actor{TenantID: u.TenantID, UserID: u.UserID}, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			after := c.After.AuditFields()
			after["changed_fields"] = statusChangedFields(ch)
			if err := h.audit(ctx, tx, u, "order.request.status", "order_request", params.ID, c.Before.AuditFields(),
				after); err != nil {
				return nil, err
			}
			return h.readOrderRequest(ctx, tx, params.ID)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OrderRequest), nil //nolint:forcetypeassert // fn trên trả *gen.OrderRequest
}

// statusChangedFields: tên các trường kèm theo lần đổi trạng thái (không giá trị: tin là dữ liệu cá nhân).
func statusChangedFields(ch order.StatusChange) []string {
	out := []string{"status"}
	if ch.Message != "" {
		out = append(out, "message")
	}
	if ch.ShipBack != nil {
		out = append(out, "shipBack")
	}
	if ch.Outcome != nil {
		out = append(out, "outcome")
	}
	return out
}

// isOrderRequestRefusal: lỗi nghiệp vụ của yêu cầu về đơn — trả lời, không lưu theo khóa chống trùng (trạng thái có
// thể đổi; gửi lại cùng khóa thì xét lại).
func isOrderRequestRefusal(err error) bool {
	for _, e := range []error{
		order.ErrInvalidInput, order.ErrInvalidTransition, order.ErrVersionConflict, order.ErrReasonRequired,
		order.ErrNotFound, order.ErrRequestNotFound, order.ErrWindowClosed, order.ErrQtyExceeded,
		order.ErrPhotoRequired, order.ErrTooManyFiles, order.ErrDuplicateOpen, order.ErrFileRejected,
		order.ErrFileNotFound, order.ErrRequestNotAllowed, order.ErrRequestLimit,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// readOrderRequest đọc yêu cầu id (đơn web của người bán mặc định, ADR-29) và dựng câu trả lời.
func (h *handler) readOrderRequest(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*gen.OrderRequest, error) {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("người bán của đơn web: %w", err)
	}
	d, err := order.GetRequest(ctx, tx, seller, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của order
	}
	return h.orderRequestOut(ctx, tx, d)
}

// orderRequestOut là yêu cầu của hợp đồng nhân viên: tên nhân viên của tin, ảnh kèm trạng thái và link ký.
func (h *handler) orderRequestOut(ctx context.Context, tx pgx.Tx, d order.RequestDetail) (*gen.OrderRequest, error) {
	out := &gen.OrderRequest{
		ID: d.ID, Ref: d.Ref, OrderId: d.OrderID, OrderCode: d.OrderCode, Kind: gen.OrderRequestKind(d.Kind),
		Status: gen.OrderRequestStatus(d.Status), ReasonCode: d.ReasonCode, ConditionConfirmed: d.ConditionConfirmed,
		Lines: requestLinesOut(d.Lines, true), Messages: []gen.OrderRequestMessage{}, Files: []gen.OrderRequestFile{},
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Version: int32(d.Version), //nolint:gosec // G115: int4
	}
	if d.CustomerNote != "" {
		out.CustomerNote = gen.NewOptString(d.CustomerNote)
	}
	setRequestShipBackOutcome(&out.ShipBack, &out.Outcome, d)
	if d.ResolvedAt != nil {
		out.ResolvedAt = gen.NewOptDateTime(*d.ResolvedAt)
	}
	if d.ResolvedBy != nil {
		out.ResolvedBy = gen.NewOptUUID(*d.ResolvedBy)
	}
	var staff []uuid.UUID
	for _, m := range d.Messages {
		if id, err := uuid.Parse(m.AuthorID); err == nil && m.AuthorType == order.AuthorStaff {
			staff = append(staff, id)
		}
	}
	names, err := staffNames(ctx, tx, staff)
	if err != nil {
		return nil, err
	}
	for _, m := range d.Messages {
		out.Messages = append(out.Messages, *messageOut(m, names))
	}
	for _, f := range d.FileIDs {
		item, err := h.requestFileOut(ctx, tx, f)
		if err != nil {
			return nil, err
		}
		out.Files = append(out.Files, item)
	}
	return out, nil
}

// requestFileOut: ảnh ready → link ký 10 phút tới biến thể JPEG lớn nhất ở bucket riêng tư (M1 chọn, không tự ghép
// bucket, khóa); đang xử lý → processing; imager từ chối → rejected; đã dọn, hết hạn, mất → missing.
func (h *handler) requestFileOut(ctx context.Context, tx pgx.Tx, id uuid.UUID) (gen.OrderRequestFile, error) {
	item := gen.OrderRequestFile{FileId: id}
	if h.d.Files == nil {
		return item, errNoFileStore
	}
	u, err := files.DownloadURL(ctx, tx, h.d.Files, id, requestPhotoTTL, "")
	switch {
	case err == nil:
		item.Status, item.URL = gen.OrderRequestFileStatusReady, gen.NewOptString(u)
	case errors.Is(err, files.ErrNotReady):
		item.Status = gen.OrderRequestFileStatusProcessing
	case errors.Is(err, files.ErrRejected):
		item.Status = gen.OrderRequestFileStatusRejected
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrExpired):
		item.Status = gen.OrderRequestFileStatusMissing
	default:
		return item, fmt.Errorf("ký link ảnh của yêu cầu: %w", err)
	}
	return item, nil
}

func messageOut(m order.RequestMessage, names map[uuid.UUID]string) *gen.OrderRequestMessage {
	out := &gen.OrderRequestMessage{
		ID: m.ID, AuthorType: gen.OrderRequestMessageAuthorType(m.AuthorType), Body: m.Body,
		VisibleToCustomer: m.VisibleToCustomer, CreatedAt: m.CreatedAt,
	}
	if m.AuthorType == order.AuthorStaff {
		out.AuthorId = gen.NewOptString(m.AuthorID)
		if id, err := uuid.Parse(m.AuthorID); err == nil && names[id] != "" {
			out.AuthorName = gen.NewOptString(names[id])
		}
	}
	return out
}

// requestLinesOut đổi dòng yêu cầu sang hợp đồng; withSnapshot: kèm mã, tên chụp lúc đặt (màn nhân viên).
func requestLinesOut(lines []order.RequestLineDetail, withSnapshot bool) []gen.OrderRequestLine {
	out := make([]gen.OrderRequestLine, 0, len(lines))
	for _, l := range lines {
		g := gen.OrderRequestLine{VariantId: l.VariantID, Qty: int32(l.Qty), Reason: l.Reason} //nolint:gosec // ≤ 20
		if l.WantVariantID != nil {
			g.WantVariantId = gen.NewOptUUID(*l.WantVariantID)
		}
		if l.Note != "" {
			g.Note = gen.NewOptString(l.Note)
		}
		if withSnapshot {
			g.Sku, g.Name = gen.NewOptString(l.SKU), gen.NewOptString(l.Name)
		}
		out = append(out, g)
	}
	return out
}

func setRequestShipBackOutcome(sb *gen.OptOrderRequestShipBack, oc *gen.OptOrderRequestOutcome, d order.RequestDetail) {
	if d.ShipBack != nil {
		*sb = gen.NewOptOrderRequestShipBack(gen.OrderRequestShipBack{
			Address: d.ShipBack.Address, PackageCode: d.ShipBack.PackageCode, Deadline: d.ShipBack.Deadline,
		})
	}
	if d.Outcome != nil {
		o := gen.OrderRequestOutcome{}
		if d.Outcome.TrackingCode != "" {
			o.TrackingCode = gen.NewOptString(d.Outcome.TrackingCode)
		}
		if d.Outcome.Reason != "" {
			o.Reason = gen.NewOptString(d.Outcome.Reason)
		}
		*oc = gen.NewOptOrderRequestOutcome(o)
	}
}
