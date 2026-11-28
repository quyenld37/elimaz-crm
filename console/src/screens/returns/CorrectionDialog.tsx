// Đảo "Thành công" của vận đơn (kế hoạch M3 Task 138, TI08; mockup docs/mockups/m3/tien-tren-don.html trạng thái D;
// HV35): lý do bắt buộc → previewShipmentCorrection (không đổi dữ liệu) hiện "Sẽ xảy ra" (câu của máy chủ) → "Xác nhận
// đảo" gửi previewToken. Bị chặn (blocking khác rỗng, ví dụ hãng đã chuyển tiền) → hiện câu chặn, không có nút xác
// nhận, chỉ có lối sang "Điều chỉnh COD" (người có cash.adjust). 409 preview_stale → tự xem trước lại, hiện bản mới
// kèm câu; 409 correction_blocked lúc xác nhận → xem trước lại để hiện câu chặn. Cần fulfillment.correct ở kho của đơn.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  applyShipmentCorrection,
  previewShipmentCorrection,
  type Order,
  type Shipment,
  type ShipmentCorrectionPreview,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { reloadOrder } from "../don/orderData";
import { TEXT_NEW_PREVIEW } from "../payments/paymentRules";
import { DialogNotes } from "../shipping/dialogParts";
import { shipmentKey } from "../shipping/useShipment";
import { Dialog } from "../staff/staffData";

const RETRY_PREVIEW = new Set(["preview_stale", "correction_blocked"]);

export interface CorrectionDialogProps {
  s: Shipment;
  order: Order;
  canAdjust: boolean;
  onClose: () => void;
  /** Mở Điều chỉnh COD của vận đơn (khi đảo bị chặn vì tiền đã về). */
  onAdjust: () => void;
}

export function CorrectionDialog({ s, order: o, canAdjust, onClose, onAdjust }: CorrectionDialogProps) {
  const qc = useQueryClient();
  const [reason, setReason] = useState("");
  const [preview, setPreview] = useState<{ p: ShipmentCorrectionPreview; reason: string } | null>(null);
  const [fresh, setFresh] = useState(false);
  const look = useWriteAction((key, b: { id: string; reason: string }) =>
    unwrap(
      previewShipmentCorrection({
        path: { id: b.id },
        body: { to: "returning", reason: b.reason },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const apply = useWriteAction((key, b: { id: string; reason: string; token: string }) =>
    unwrap(
      applyShipmentCorrection({
        path: { id: b.id },
        body: { to: "returning", reason: b.reason, previewToken: b.token },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const r = reason.trim();
  const why = r === "" ? "Ghi lý do đảo" : null;

  const runPreview = async (text: string, isFresh: boolean) => {
    const res = await look.run({ id: s.id, reason: text });
    if (!res.ok) return;
    setPreview({ p: res.data, reason: text });
    setFresh(isFresh);
  };
  const confirm = async () => {
    if (preview === null) return;
    const res = await apply.run({ id: s.id, reason: preview.reason, token: preview.p.token });
    if (res.ok) {
      void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
      reloadOrder(qc, o.id);
      onClose();
      return;
    }
    if (res.error instanceof ApiError && RETRY_PREVIEW.has(res.error.code)) {
      apply.reset();
      await runPreview(preview.reason, true);
    }
  };
  const blocked = preview !== null && preview.p.blocking.length > 0;
  const busy = look.pending || apply.pending;
  return (
    <Dialog
      title={`Đảo "Thành công" · ${o.code}`}
      closeButton
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Đóng
          </button>
          {preview === null ? (
            <button
              type="button"
              className="btn btn-dark"
              disabled={why !== null || busy}
              onClick={() => void runPreview(r, false)}
            >
              Xem trước
            </button>
          ) : blocked ? (
            canAdjust && (
              <button type="button" className="btn btn-line" onClick={onAdjust}>
                Điều chỉnh COD
              </button>
            )
          ) : (
            <button type="button" className="btn btn-danger" disabled={busy} onClick={() => void confirm()}>
              Xác nhận đảo
            </button>
          )}
        </>
      }
    >
      <p className="small muted">{`Vận đơn ${s.trackingCode ?? s.code} · Thành công → Đang chuyển hoàn`}</p>
      <label className="field">
        <span>Lý do *</span>
        <input
          className="input"
          maxLength={500}
          aria-label="Lý do"
          value={reason}
          disabled={preview !== null}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      {preview !== null && fresh && (
        <div className="box box-soft small" role="status">
          {TEXT_NEW_PREVIEW}
        </div>
      )}
      {preview !== null &&
        (blocked ? (
          <div className="box box-err small" role="alert">
            {preview.p.blocking.map((b) => (
              <p key={b}>{b}</p>
            ))}
            <p>CRM không tự sửa tiền; kế toán làm điều chỉnh COD có xem trước.</p>
          </div>
        ) : (
          <>
            <p className="label">Sẽ xảy ra</p>
            <ul className="fx small" aria-label="Sẽ xảy ra">
              {preview.p.effects.map((e) => (
                <li key={e}>{e}</li>
              ))}
            </ul>
          </>
        ))}
      <DialogNotes why={preview === null ? why : null} message={apply.message ?? look.message} />
    </Dialog>
  );
}
