package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng cod_adjust (kế hoạch M3, Task M3-33): điều chỉnh COD có xem trước (TI08), kết quả đối chiếu sổ hằng đêm (TI03).
// Task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// isAdjustRefusal: lỗi nghiệp vụ của điều chỉnh (không lưu theo khóa chống trùng: gửi lại cùng khóa thì xét lại).
func isAdjustRefusal(err error) bool {
	_, _, ok := cashError(err)
	return ok
}

// adjustInput đổi thân yêu cầu (chung cho xem trước và ghi) sang đầu vào của cash.
func adjustInput(shipment uuid.UUID, mode string, reverses gen.OptUUID, from, to gen.OptCodAccount, amount gen.OptMoney,
	reason string, actor uuid.UUID,
) cash.CODAdjustmentInput {
	return cash.CODAdjustmentInput{
		ShipmentID: shipment, Mode: mode, ReversesTransferID: optPtr[uuid.UUID](reverses), From: string(from.Or("")),
		To: string(to.Or("")), Amount: int64(amount.Or(0)), Reason: reason, Actor: actor,
	}
}

// PreviewCodAdjustment (POST /api/v1/cod/adjustments/preview, cash.adjust): bản xem trước — số dư trước, sau, bút toán
// sẽ ghi, cảnh báo, token; không đổi sổ. Vận đơn, bút toán cần đảo không có → 404; yêu cầu sai, thiếu lý do → 422;
// bút toán không được đảo → 409 invalid_transition.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PreviewCodAdjustment(ctx context.Context, req *gen.CodAdjustmentRequest, _ gen.PreviewCodAdjustmentParams) (gen.PreviewCodAdjustmentRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isAdjustRefusal, "xem trước điều chỉnh COD",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			if err := sellerShipmentCOD(ctx, tx, req.ShipmentId); err != nil {
				return nil, err
			}
			p, err := h.d.Cash.PreviewCODAdjustment(ctx, tx, adjustInput(req.ShipmentId, string(req.Mode),
				req.ReversesTransferId, req.From, req.To, req.Amount, req.Reason, u.UserID))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			body := &gen.CodAdjustmentPreview{
				Before: codBalancesOut(p.Before), After: codBalancesOut(p.After), Warnings: p.Warnings, Token: p.Token,
				Transfer: gen.CodAdjustmentTransfer{
					Kind: gen.CodAdjustmentTransferKind(p.Kind), Entries: make([]gen.CodEntry, 0, len(p.Entries)),
				},
			}
			if body.Warnings == nil {
				body.Warnings = []string{}
			}
			for _, e := range p.Entries {
				body.Transfer.Entries = append(body.Transfer.Entries,
					gen.CodEntry{Account: gen.CodAccount(e.Account), Amount: gen.SignedMoney(e.Amount)})
			}
			return body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CodAdjustmentPreview), nil //nolint:forcetypeassert // fn trên trả *gen.CodAdjustmentPreview
}

// CreateCodAdjustment (POST /api/v1/cod/adjustments, cash.adjust): ghi điều chỉnh theo bản xem trước (tính lại trong
// transaction; token khác → 409 preview_stale), audit cash.cod.adjust (không ghi lý do: chữ tự do) → 201 bút toán vừa
// ghi. Lỗi khác như previewCodAdjustment.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCodAdjustment(ctx context.Context, req *gen.CreateCodAdjustmentRequest, _ gen.CreateCodAdjustmentParams) (gen.CreateCodAdjustmentRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isAdjustRefusal, "ghi điều chỉnh COD",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			if err := sellerShipmentCOD(ctx, tx, req.ShipmentId); err != nil {
				return nil, err
			}
			in := adjustInput(req.ShipmentId, string(req.Mode), req.ReversesTransferId, req.From, req.To, req.Amount,
				req.Reason, u.UserID)
			t, err := h.d.Cash.CreateCODAdjustment(ctx, tx, in, req.Token)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			names, err := staffNames(ctx, tx, []uuid.UUID{u.UserID})
			if err != nil {
				return nil, err
			}
			after := map[string]any{"mode": in.Mode, "shipment_id": in.ShipmentID.String(), "kind": t.Kind}
			if in.ReversesTransferID != nil {
				after["reverses_transfer_id"] = in.ReversesTransferID.String()
			}
			for _, e := range t.Entries {
				after[e.Account] = e.Amount
			}
			if err := h.audit(ctx, tx, u, "cash.cod.adjust", "cash.cod_transfer", t.ID, nil, after); err != nil {
				return nil, err
			}
			body := shipmentCodOut(cash.ShipmentCOD{Transfers: []cash.CODTransfer{t}}, names).Transfers[0]
			return &body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CodTransfer), nil //nolint:forcetypeassert // fn trên trả *gen.CodTransfer
}

// ListReconcileFindings (GET /api/v1/cod/reconcile-findings, cash.cod.read): phát hiện của lần đối chiếu runId (mặc
// định lần gần nhất) theo mã tăng dần; con trỏ là mã phát hiện cuối trang trước (sai dạng → 400). Lần chạy không có
// → 404; chưa chạy lần nào → items rỗng, không runId.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListReconcileFindings(ctx context.Context, params gen.ListReconcileFindingsParams) (gen.ListReconcileFindingsRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	var after *uuid.UUID
	if c, ok := params.Cursor.Get(); ok {
		id, err := uuid.Parse(c)
		if err != nil {
			return nil, fmt.Errorf("con trỏ trang phát hiện đối chiếu: %w", cash.ErrInvalidInput)
		}
		after = &id
	}
	out := &gen.ReconcileFindingPage{Items: []gen.ReconcileFinding{}}
	err := h.sellerRead(ctx, func(tx pgx.Tx, seller uuid.UUID) error {
		p, err := h.d.Cash.ListReconcileFindings(ctx, tx, seller, optPtr[uuid.UUID](params.RunId), after,
			int(params.Limit.Or(defaultStatementLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		out = &gen.ReconcileFindingPage{Items: make([]gen.ReconcileFinding, 0, len(p.Items))}
		if p.RunID != nil {
			out.RunId, out.RunAt = gen.NewOptUUID(*p.RunID), gen.NewOptDateTime(p.RunAt)
		}
		if p.Next != nil {
			out.NextCursor = gen.NewOptString(p.Next.String())
		}
		for _, f := range p.Items {
			item := gen.ReconcileFinding{
				ID: f.ID, RunId: f.RunID, Kind: f.Kind, CreatedAt: f.CreatedAt,
				Detail: gen.NewOptReconcileFindingDetail(gen.ReconcileFindingDetail(f.Detail)),
			}
			if f.ShipmentID != nil {
				item.ShipmentId = gen.NewOptUUID(*f.ShipmentID)
			}
			out.Items = append(out.Items, item)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc kết quả đối chiếu: %w", err)
	}
	return out, nil
}

// codBalancesOut dựng số dư năm ngăn như getShipmentCod.
func codBalancesOut(b cash.Balances) gen.CodBalances {
	return shipmentCodOut(cash.ShipmentCOD{Balances: b}, nil).Balances
}
