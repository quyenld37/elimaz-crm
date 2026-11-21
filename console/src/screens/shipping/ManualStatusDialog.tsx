// Hộp "Cập nhật trạng thái" của vận đơn nhập mã tay / tự giao (kế hoạch M3 Task 134; mockup
// docs/mockups/m3/don-van-chuyen.html trạng thái G; HV16, VC14, Task 106). Trạng thái mới, lúc (giờ Việt Nam), tiền hãng
// đã thu (chỉ khi Thành công), lý do bắt buộc → setShipmentManualStatus kèm version của vận đơn đang xem (409 thì đọc
// lại). "Đã hoàn về kho" không chọn ở đây: chỉ khi kho bấm nhận hàng hoàn. Máy chủ kiểm đường đi của trạng thái.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { setShipmentManualStatus, type SetShipmentManualStatusRequest, type Shipment } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { reloadOrder } from "../don/orderData";
import { parseMoney } from "../products/editRules";
import { Dialog } from "../staff/staffData";
import { DialogActions, DialogNotes } from "./dialogParts";
import { MANUAL_TARGETS, STATUS_TEXT, fromVnLocal, toVnLocal, type ManualTarget } from "./shipmentRules";
import { shipmentKey } from "./useShipment";

const moneyText = new Intl.NumberFormat("vi-VN");

interface Body {
  id: string;
  req: SetShipmentManualStatusRequest;
}

export function ManualStatusDialog({ s, onClose }: { s: Shipment; onClose: () => void }) {
  const qc = useQueryClient();
  const targets = MANUAL_TARGETS.filter((t) => t !== s.status);
  const [to, setTo] = useState<ManualTarget>(targets[0] ?? "delivered");
  // Giờ mở hộp: mặc định của ô "Lúc" và mốc chặn giờ tương lai (cho lệch một phút; máy chủ vẫn kiểm).
  const [openedAt] = useState(() => Date.now());
  const [at, setAt] = useState(() => toVnLocal(openedAt));
  const [cod, setCod] = useState(() => moneyText.format(s.codAmount));
  const [reason, setReason] = useState("");
  const write = useWriteAction((key, b: Body) =>
    unwrap(setShipmentManualStatus({ path: { id: b.id }, body: b.req, headers: { "Idempotency-Key": key } })),
  );
  const iso = fromVnLocal(at);
  const codValue = cod.trim() === "" ? null : parseMoney(cod);
  const r = reason.trim();
  const why =
    iso === null
      ? "Nhập lúc đổi trạng thái"
      : Date.parse(iso) > openedAt + 60_000
        ? "Lúc đổi trạng thái không được ở tương lai"
        : to === "delivered" && cod.trim() !== "" && codValue === null
          ? "Tiền hãng đã thu chưa đúng dạng"
          : r === ""
            ? "Ghi lý do"
            : null;
  const submit = async () => {
    if (iso === null) return;
    const res = await write.run({
      id: s.id,
      req: {
        to,
        at: iso,
        reason: r,
        ...(to === "delivered" && codValue !== null ? { codCollected: codValue } : {}),
        ...(s.version === undefined ? {} : { version: s.version }),
      },
    });
    if (!res.ok) {
      if (res.error instanceof ApiError && res.error.code === "version_conflict") {
        void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
      }
      return;
    }
    qc.setQueryData(shipmentKey(s.id), res.data);
    void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
    reloadOrder(qc, res.data.orderId);
    onClose();
  };
  return (
    <Dialog
      title="Cập nhật trạng thái"
      closeButton
      onClose={onClose}
      footer={
        <DialogActions label="Lưu" why={why} pending={write.pending} onSubmit={() => void submit()} onClose={onClose} />
      }
    >
      <label className="field">
        <span>Trạng thái mới *</span>
        <span className="select-wrap">
          <select
            value={to}
            onChange={(e) => {
              const t = targets.find((x) => x === e.target.value);
              if (t !== undefined) setTo(t);
            }}
          >
            {targets.map((t) => (
              <option key={t} value={t}>
                {STATUS_TEXT[t]}
              </option>
            ))}
          </select>
        </span>
      </label>
      <label className="field">
        <span>Lúc *</span>
        <input
          className="input num"
          type="datetime-local"
          value={at}
          onChange={(e) => {
            setAt(e.target.value);
          }}
        />
      </label>
      {to === "delivered" && (
        <label className="field">
          <span>Tiền hãng đã thu</span>
          <input
            className="input num"
            inputMode="numeric"
            value={cod}
            onChange={(e) => {
              setCod(e.target.value);
            }}
          />
          <span className="hint">Chỉ khi chọn Thành công</span>
        </label>
      )}
      <label className="field">
        <span>Lý do *</span>
        <input
          className="input"
          maxLength={1000}
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      <p className="hint">&quot;Đã hoàn về kho&quot; không chọn ở đây: chỉ khi kho bấm nhận hàng hoàn.</p>
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
