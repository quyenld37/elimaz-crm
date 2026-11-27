package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Hoàn tất đơn nháp flash sale (kế hoạch M5, Task 202 / M5-19; QĐ3, QĐ6, QĐ7): khách điền địa chỉ, chọn hãng trong hạn
// giữ; đơn draft → new (luồng đơn web thường), đơn vị suất held → sold. COD, không mã giảm.

// ErrHoldExpired báo: đơn nháp flash sale đã quá hạn giữ. CompleteFlashDraft đã hủy đơn (draft_expired) trong tx của
// bên gọi: đây là kết quả cuối, bên gọi commit (không lùi) rồi trả 422 hold_expired.
var ErrHoldExpired = errors.New("order: đã hết hạn giữ suất flash sale")

// CompleteFlashInput là lần hoàn tất đơn nháp flash sale mã ngoài ExternalRef: SĐT khách nhập (phải là SĐT của khách
// của đơn), địa chỉ giao (đủ tỉnh, xã), phí ship khách trả, hãng khách chọn (nil: không chọn), ghi chú của khách.
type CompleteFlashInput struct {
	ExternalRef string
	Phone       logging.Phone
	ShipAddress ShipAddress
	ShippingFee int64
	Shipping    *WebShipChoice
	Note        string
}

// CompleteFlashDraft hoàn tất đơn nháp flash sale của in.ExternalRef (người bán catalog.DefaultSeller) trong tx của
// bên gọi, actor a là khóa dịch vụ website, giờ now:
//
//  1. Kiểm đầu vào (ErrInvalidInput: mã ngoài, SĐT sai dạng, ghi chú quá 2000 ký tự, phí, lựa chọn giao hàng).
//  2. Không có đơn web của mã ngoài, không phải đơn flash, hay SĐT không thuộc nhóm của khách (customer.HasPhone, A15)
//     → ErrNotFound (không lộ đơn có tồn tại). Khóa đơn FOR UPDATE.
//  3. Đơn đã rời draft: cancelled vì hết hạn → ErrHoldExpired; hủy lý do khác → ErrInvalidTransition; còn lại (đã
//     hoàn tất) → kết quả hiện tại, Replayed, không ghi gì.
//  4. draft đã tới hạn (draft_expires_at ≤ now) → Cancel(draft_expired) trong cùng tx (nhả suất như hết hạn) và
//     ErrHoldExpired.
//  5. Lựa chọn giao hàng có vấn đề → ErrShipCarrierUnavailable, ErrShipFeeMismatch, ErrShipUnverified; địa chỉ thiếu
//     tỉnh, xã → ErrAddressIncomplete (CheckShipAddress: customer.ValidateUnits).
//  6. Edit (địa chỉ, phí ship; tổng tính lại bằng pricing, COD = tổng; order.updated), cột web (hãng khách chọn,
//     submitted_via sync), ghi chú; draft → new qua applyTransition (lịch sử, order.new); inventory.MarkFlashSold.
//
// Không giữ trạng thái giữa các lần chạy lại; không gọi mạng. Audit (nếu cần) do bên gọi ghi.
func (w Writer) CompleteFlashDraft(ctx context.Context, tx pgx.Tx, in CompleteFlashInput, a Actor, now time.Time,
) (WebOrderResult, error) {
	now = now.UTC()
	in.ExternalRef = strings.TrimSpace(in.ExternalRef)
	if in.ExternalRef == "" || !optText(in.ExternalRef, maxExternalRefRunes) || !money(in.ShippingFee) ||
		utf8.RuneCountInString(in.Note) > maxWebNoteRunes || !validShipChoice(in.Shipping) {
		return WebOrderResult{}, fmt.Errorf("%w: hoàn tất đơn flash sale", ErrInvalidInput)
	}
	if _, err := customer.NormalizePhone(string(in.Phone)); err != nil {
		return WebOrderResult{}, fmt.Errorf("%w: SĐT", ErrInvalidInput)
	}
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("người bán của đơn web: %w", err)
	}
	q := store.New(tx)
	ref, err := q.FlashOrderByExternalRef(ctx, store.FlashOrderByExternalRefParams{
		SellerID: seller, ExternalRef: pgtype.Text{String: in.ExternalRef, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebOrderResult{}, ErrNotFound
	}
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("đọc đơn flash theo mã ngoài: %w", err)
	}
	locked, err := lockOrder(ctx, q, seller, ref.ID)
	if err != nil {
		return WebOrderResult{}, err
	}
	lines, err := q.FlashLinesOfOrder(ctx, store.FlashLinesOfOrderParams{SellerID: seller, OrderID: ref.ID})
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("đọc dòng flash của đơn: %w", err)
	}
	if len(lines) == 0 {
		return WebOrderResult{}, ErrNotFound
	}
	owned, err := customer.HasPhone(ctx, tx, locked.CustomerID, in.Phone)
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("kiểm SĐT của khách: %w", err)
	}
	if !owned {
		return WebOrderResult{}, ErrNotFound
	}
	cur, err := Get(ctx, tx, seller, ref.ID)
	if err != nil {
		return WebOrderResult{}, err
	}
	if res, done, err := completedResult(cur); done {
		return res, err
	}
	if len(lines) != 1 {
		return WebOrderResult{}, fmt.Errorf("%w: đơn có %d dòng flash sale", ErrInvalidTransition, len(lines))
	}
	a.TenantID = locked.TenantID
	if cur.DraftExpiresAt != nil && !now.Before(*cur.DraftExpiresAt) {
		if _, err := w.Cancel(ctx, tx, seller, cur.ID, cur.Version, CancelDraftExpired, "", a, now); err != nil {
			return WebOrderResult{}, fmt.Errorf("hủy đơn nháp flash sale quá hạn: %w", err)
		}
		return WebOrderResult{}, ErrHoldExpired
	}
	return w.completeDraft(ctx, tx, cur, lines[0], in, a, now)
}

// completedResult: đơn cur đã rời draft → (kết quả, true): hủy vì hết hạn → ErrHoldExpired; hủy lý do khác →
// ErrInvalidTransition; trạng thái khác (đã hoàn tất) → kết quả hiện tại, Replayed.
func completedResult(cur Order) (WebOrderResult, bool, error) {
	switch {
	case cur.Status == StatusDraft:
		return WebOrderResult{}, false, nil
	case cur.Status == StatusCancelled && cur.CancelReason != nil && *cur.CancelReason == CancelDraftExpired:
		return WebOrderResult{}, true, ErrHoldExpired
	case cur.Status == StatusCancelled:
		return WebOrderResult{}, true, fmt.Errorf("%w: đơn flash sale đã hủy", ErrInvalidTransition)
	}
	return WebOrderResult{
		OrderID: cur.ID, Code: cur.Code, Status: cur.Status, GrandTotal: cur.GrandTotal, Replayed: true,
	}, true, nil
}

// completeDraft là bước 5–6 của CompleteFlashDraft cho đơn draft cur (đã khóa, còn hạn) có dòng flash line.
func (w Writer) completeDraft(ctx context.Context, tx pgx.Tx, cur Order, line store.FlashLinesOfOrderRow,
	in CompleteFlashInput, a Actor, now time.Time,
) (WebOrderResult, error) {
	problem := shipProblem(in.Shipping, in.ShippingFee)
	if problem != "" {
		return WebOrderResult{}, fmt.Errorf("hoàn tất đơn flash sale: %w", shipError(problem))
	}
	addr, err := CheckShipAddress(ctx, tx, in.ShipAddress)
	if err != nil {
		return WebOrderResult{}, err
	}
	fee := in.ShippingFee
	ed, err := w.Edit(ctx, tx, cur.SellerID, cur.ID, EditInput{Version: cur.Version, ShippingFee: &fee, ShipAddress: &addr},
		a, now)
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("ghi địa chỉ, phí ship của đơn flash sale: %w", err)
	}
	ship, err := shipChoiceJSON(in.Shipping, "")
	if err != nil {
		return WebOrderResult{}, err
	}
	n, err := store.New(tx).SetOrderWebFields(ctx, store.SetOrderWebFieldsParams{
		SubmittedVia: pgtype.Text{String: SubmittedSync, Valid: true}, WebShipping: ship, SellerID: cur.SellerID, ID: cur.ID,
	})
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("ghi cột web của đơn flash sale: %w", err)
	}
	if n != 1 {
		return WebOrderResult{}, errWebRowsMoved
	}
	if strings.TrimSpace(in.Note) != "" {
		if _, err := AddNote(ctx, tx, cur.SellerID, cur.ID, in.Note, a, now); err != nil {
			return WebOrderResult{}, err
		}
	}
	o := ed.Order
	if err := w.applyTransition(ctx, tx, &o, state.New, "", "", a, now); err != nil {
		return WebOrderResult{}, err
	}
	if err := inventory.MarkFlashSold(ctx, tx, line.ID); err != nil {
		return WebOrderResult{}, fmt.Errorf("bán suất flash sale: %w", err)
	}
	return WebOrderResult{OrderID: o.ID, Code: o.Code, Status: o.Status, GrandTotal: o.GrandTotal}, nil
}
