// Hộp Ghi phiếu thu của đơn (kế hoạch M3 Task 138, TD10; mockup docs/mockups/m3/tien-tren-don.html trạng thái A; HV33):
// chuyển khoản / cọc / tiền mặt, số tiền (không quá số còn phải thu), mã giao dịch, lúc nhận (bỏ trống: lúc ghi); hiện
// COD của đơn trước → sau (order.ApplyPaymentTotals). Máy chủ chặn đơn đã gửi hãng (409 cod_locked_by_shipment) và
// tổng vượt tổng đơn (409 payment_exceeds_total); câu lỗi hiện trong hộp, hộp không đóng.
import { useState } from "react";
import { recordPayment, type Order, type RecordPaymentRequest } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { Dialog } from "../staff/staffData";
import { METHOD_TEXT, RECORD_METHODS, codAfter } from "./paymentRules";

const MAX_REF = 128;

export interface RecordPaymentDialogProps {
  order: Order;
  onClose: () => void;
  onDone: () => void;
}

export function RecordPaymentDialog({ order: o, onClose, onDone }: RecordPaymentDialogProps) {
  const [method, setMethod] = useState<RecordPaymentRequest["method"]>("bank_transfer");
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [at, setAt] = useState("");
  const write = useWriteAction((key, b: { id: string; req: RecordPaymentRequest }) =>
    unwrap(recordPayment({ path: { id: b.id }, body: b.req, headers: { "Idempotency-Key": key } })),
  );
  const prepaid = o.prepaidTotal ?? 0;
  const left = Math.max(o.grandTotal - prepaid, 0);
  const n = parseMoney(amount);
  const atDate = at === "" ? null : new Date(at);
  const why =
    n === null || n <= 0
      ? "Nhập số tiền (đồng)"
      : n > left
        ? `Vượt số còn phải thu ${formatMoney(left)}`
        : atDate !== null && Number.isNaN(atDate.getTime())
          ? "Lúc nhận chưa đúng"
          : null;
  const submit = async () => {
    if (n === null || why !== null) return;
    const ref = reference.trim();
    const req: RecordPaymentRequest = {
      method,
      amount: n,
      ...(ref === "" ? {} : { reference: ref }),
      ...(atDate === null ? {} : { receivedAt: atDate.toISOString() }),
    };
    const r = await write.run({ id: o.id, req });
    if (r.ok) onDone();
  };
  return (
    <Dialog
      title={`Ghi phiếu thu · ${o.code}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Ghi phiếu thu"
          why={why}
          pending={write.pending}
          onClose={onClose}
          onSubmit={() => void submit()}
        />
      }
    >
      <div className="row" role="radiogroup" aria-label="Cách thu">
        {RECORD_METHODS.map((m) => (
          <button
            key={m}
            type="button"
            role="radio"
            className="toggle"
            aria-checked={method === m}
            aria-pressed={method === m}
            onClick={() => {
              setMethod(m);
            }}
          >
            {METHOD_TEXT[m]}
          </button>
        ))}
      </div>
      <label className="field">
        <span>Số tiền *</span>
        <input
          className="input"
          inputMode="numeric"
          aria-label="Số tiền"
          value={amount}
          onChange={(e) => {
            setAmount(e.target.value);
          }}
        />
        <span className="hint">{`Còn phải thu ${formatMoney(left)}.`}</span>
      </label>
      <label className="field">
        <span>Mã giao dịch / ghi chú</span>
        <input
          className="input"
          maxLength={MAX_REF}
          aria-label="Mã giao dịch / ghi chú"
          value={reference}
          onChange={(e) => {
            setReference(e.target.value);
          }}
        />
      </label>
      <label className="field">
        <span>Lúc nhận</span>
        <input
          className="input"
          type="datetime-local"
          aria-label="Lúc nhận"
          value={at}
          onChange={(e) => {
            setAt(e.target.value);
          }}
        />
        <span className="hint">Bỏ trống là lúc ghi.</span>
      </label>
      {n !== null && why === null && (
        <div className="box box-soft small">
          {`COD của đơn tự tính lại: ${formatMoney(o.codAmount)} → ${formatMoney(codAfter(o.grandTotal, prepaid, n))}.`}
        </div>
      )}
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
