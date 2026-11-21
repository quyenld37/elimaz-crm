// Hộp "Hủy / đổi hãng" của vận đơn (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng thái F; HV12,
// VC11, TD03). Hai việc, lý do bắt buộc: "Đổi hãng khác (giữ đơn)" → cancelShipment (đơn giữ "Đã xác nhận", gửi lại
// bằng hãng khác khi hãng xác nhận hủy); "Hủy đơn" → cancelOrder của M2 (Q-38: đơn "Đang yêu cầu hủy", hàng chỉ về
// sổ khi hãng xác nhận). Vận đơn đang tạo kẹt ("Không rõ"): chỉ đổi hãng, kèm ô "đã tra trang hãng" (carrierChecked).
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { cancelOrder, cancelShipment, type Order, type Shipment } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { reloadOrder } from "../don/orderData";
import { Dialog } from "../staff/staffData";
import { DialogActions, DialogNotes } from "./dialogParts";
import { carrierName } from "./QuoteTable";
import { shipmentKey } from "./useShipment";

export type CancelChoice = "replace" | "order";

interface Body {
  choice: CancelChoice;
  shipmentId: string;
  orderId: string;
  version: number;
  reason: string;
  checked: boolean;
}

/** Tên hãng của vận đơn cho câu chữ. */
export const shipmentCarrier = (s: Shipment) => carrierName(s.carrierKey, s.accountName ?? s.carrierKey);

/**
 * Hộp hủy vận đơn s của đơn o. canReplace: quyền fulfillment.cancel ở kho của đơn; canCancelOrder: quyền order.cancel.
 * needsCheck: vận đơn đang tạo kẹt, đòi tick "đã tra trang hãng" (chỉ đổi hãng). Máy chủ vẫn kiểm.
 */
export function CancelDialog(props: {
  s: Shipment;
  order: Order;
  canReplace: boolean;
  canCancelOrder: boolean;
  needsCheck?: boolean;
  /** Mở từ "Hủy để đổi hãng" (vận đơn đang tạo): chỉ đổi hãng, không có lựa chọn hủy đơn. */
  replaceOnly?: boolean;
  onClose: () => void;
}) {
  const { s, order: o, onClose } = props;
  const needsCheck = props.needsCheck === true;
  const replaceOnly = needsCheck || props.replaceOnly === true;
  const canOrder = props.canCancelOrder && !replaceOnly;
  const qc = useQueryClient();
  const id = useId();
  const [choice, setChoice] = useState<CancelChoice | null>(props.canReplace ? "replace" : canOrder ? "order" : null);
  const [reason, setReason] = useState("");
  const [checked, setChecked] = useState(false);
  const write = useWriteAction(async (key, b: Body): Promise<Shipment | null> => {
    const headers = { "Idempotency-Key": key };
    if (b.choice === "order") {
      await unwrap(
        cancelOrder({
          path: { id: b.orderId },
          body: { version: b.version, reason: "other", note: b.reason },
          headers,
        }),
      );
      return null;
    }
    return unwrap(
      cancelShipment({
        path: { id: b.shipmentId },
        body: { reason: b.reason, ...(b.checked ? { carrierChecked: true } : {}) },
        headers,
      }),
    );
  });
  const carrier = shipmentCarrier(s);
  const r = reason.trim();
  const why =
    choice === null
      ? "Bạn không có quyền hủy vận đơn hay hủy đơn"
      : r === ""
        ? "Ghi lý do"
        : needsCheck && !checked
          ? "Tick ô đã tra trang hãng"
          : null;

  const submit = async () => {
    if (choice === null) return;
    const res = await write.run({
      choice,
      shipmentId: s.id,
      orderId: o.id,
      version: o.version ?? 0,
      reason: r,
      checked: needsCheck && checked,
    });
    if (!res.ok) return;
    if (res.data !== null) qc.setQueryData(shipmentKey(s.id), res.data);
    void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
    reloadOrder(qc, o.id);
    onClose();
  };

  const radio = (c: CancelChoice, text: string, allowed: boolean) => (
    <label className="check">
      <input
        type="radio"
        name={`${id}-viec`}
        checked={choice === c}
        disabled={!allowed || write.pending}
        onChange={() => {
          setChoice(c);
        }}
      />{" "}
      {text}
    </label>
  );

  return (
    <Dialog
      title={`Hủy vận đơn ${s.trackingCode ?? s.code}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Gửi yêu cầu hủy"
          danger
          why={why}
          pending={write.pending}
          onSubmit={() => void submit()}
          onClose={onClose}
        />
      }
    >
      <div className="radios" role="radiogroup" aria-label="Việc cần làm">
        {!replaceOnly && radio("order", "Hủy đơn (khách không lấy nữa)", props.canCancelOrder)}
        {radio("replace", "Đổi hãng khác (giữ đơn)", props.canReplace)}
      </div>
      <label className="field">
        <span>Lý do *</span>
        <textarea
          className="input"
          style={{ minHeight: 64 }}
          maxLength={500}
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      {needsCheck && (
        <label className="check">
          <input
            type="checkbox"
            checked={checked}
            onChange={(e) => {
              setChecked(e.target.checked);
            }}
          />{" "}
          {`Đã tra trang ${carrier}: hãng không có vận đơn này`}
        </label>
      )}
      <p className="small">
        {`Đổi hãng: CRM gửi lệnh hủy cho ${carrier}. Hãng xác nhận thì đơn về "Đã xác nhận", gửi bằng hãng khác được; tin muộn của vận đơn cũ chỉ vào lịch sử của nó.`}
      </p>
      {!replaceOnly && (
        <p className="small">Hủy đơn: đơn thành &quot;Đang yêu cầu hủy&quot;; hàng chỉ về sổ khi hãng xác nhận hủy.</p>
      )}
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
