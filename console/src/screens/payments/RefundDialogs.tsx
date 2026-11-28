// Hộp Chi hoàn và Miễn hoàn của một nghĩa vụ hoàn (kế hoạch M3 Task 138, TI09, TD10; mockup
// docs/mockups/m3/tien-tren-don.html trạng thái B; HV34): chi một phần được, số chi không quá số còn phải hoàn; miễn
// đòi lý do (cash.refunds.waive, vai trò mẫu: Chủ). Máy chủ kiểm lại (409 refund_exceeds_remaining khi người khác vừa
// chi). Ghi qua useWriteAction (khóa chống trùng sinh lúc bấm).
import { useState } from "react";
import { payRefundObligation, waiveRefundObligation, type RefundObligation } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { TEXT_NOT_COUNTED, obligationState } from "./paymentRules";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { formatMoney } from "../products/productRules";
import { parseMoney } from "../products/editRules";

const MAX_REF = 128;

interface DialogProps {
  ob: RefundObligation;
  onClose: () => void;
  onDone: (o: RefundObligation) => void;
}

const codeOf = (ob: RefundObligation) => ob.orderCode ?? "";

/** Chi hoàn (payRefundObligation): số tiền mặc định là số còn phải hoàn. */
export function PayRefundDialog({ ob, onClose, onDone }: DialogProps) {
  const st = obligationState(ob);
  const [amount, setAmount] = useState(String(st.remaining));
  const [reference, setReference] = useState("");
  const write = useWriteAction((key, b: { id: string; amount: number; reference: string }) =>
    unwrap(
      payRefundObligation({
        path: { id: b.id },
        body: { amount: b.amount, ...(b.reference === "" ? {} : { reference: b.reference }) },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const n = parseMoney(amount);
  const why =
    n === null || n <= 0
      ? "Nhập số tiền chi (đồng)"
      : n > st.remaining
        ? `Vượt số còn phải hoàn ${formatMoney(st.remaining)}`
        : null;
  const after =
    n === null || why !== null
      ? null
      : n === st.remaining
        ? `Sau khi chi: đã hoàn ${formatMoney(ob.paidAmount + n)}, đóng nghĩa vụ.`
        : `Sau khi chi: đã hoàn ${formatMoney(ob.paidAmount + n)}, còn ${formatMoney(st.remaining - n)}, nghĩa vụ vẫn mở.`;
  const submit = async () => {
    if (n === null || why !== null) return;
    const r = await write.run({ id: ob.id, amount: n, reference: reference.trim() });
    if (r.ok) onDone(r.data);
  };
  return (
    <Dialog
      title={`Chi hoàn · ${codeOf(ob)}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Ghi phiếu chi"
          why={why}
          pending={write.pending}
          onClose={onClose}
          onSubmit={() => void submit()}
        />
      }
    >
      <p className="small">
        {ob.paidAmount === 0 ? `${TEXT_NOT_COUNTED}. ` : `${st.note}. `}
        Còn phải hoàn <span className="money">{formatMoney(st.remaining)}</span>.
      </p>
      <label className="field">
        <span>Số tiền chi *</span>
        <input
          className="input"
          inputMode="numeric"
          aria-label="Số tiền chi"
          value={amount}
          onChange={(e) => {
            setAmount(e.target.value);
          }}
        />
        <span className="hint">{`Còn phải hoàn ${formatMoney(st.remaining)}; chi một phần được.`}</span>
      </label>
      <label className="field">
        <span>Mã giao dịch</span>
        <input
          className="input"
          maxLength={MAX_REF}
          aria-label="Mã giao dịch"
          value={reference}
          onChange={(e) => {
            setReference(e.target.value);
          }}
        />
      </label>
      {after !== null && <div className="box box-soft small">{after}</div>}
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}

/** Miễn hoàn (waiveRefundObligation): lý do bắt buộc; ghi lịch sử, không xóa nghĩa vụ. */
export function WaiveRefundDialog({ ob, onClose, onDone }: DialogProps) {
  const st = obligationState(ob);
  const [reason, setReason] = useState("");
  const write = useWriteAction((key, b: { id: string; reason: string }) =>
    unwrap(
      waiveRefundObligation({ path: { id: b.id }, body: { reason: b.reason }, headers: { "Idempotency-Key": key } }),
    ),
  );
  const why = reason.trim() === "" ? "Ghi lý do miễn hoàn" : null;
  const submit = async () => {
    if (why !== null) return;
    const r = await write.run({ id: ob.id, reason: reason.trim() });
    if (r.ok) onDone(r.data);
  };
  return (
    <Dialog
      title={`Miễn hoàn · ${codeOf(ob)}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Miễn hoàn"
          danger
          why={why}
          pending={write.pending}
          onClose={onClose}
          onSubmit={() => void submit()}
        />
      }
    >
      <label className="field">
        <span>Lý do *</span>
        <textarea
          className="input"
          maxLength={500}
          aria-label="Lý do"
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      <p className="small">{`Đóng nghĩa vụ "không hoàn" ${formatMoney(st.remaining)}. Ghi lịch sử, không xóa.`}</p>
      <p className="hint">Chính sách mất cọc của shop chưa chốt: CRM không tự miễn; người làm quyết và ghi lý do.</p>
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
