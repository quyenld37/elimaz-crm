// Hộp thoại Hủy đơn (mockup docs/mockups/m2/don-chi-tiet.html trạng thái C; HV18). Đơn chưa gửi hãng: hủy, hàng về
// có thể bán ngay. Đơn đã gửi hãng (có vận đơn hiện hành): máy chủ chuyển "Đang yêu cầu hủy", hàng chưa về kho tới khi
// hãng xác nhận. Gửi version đã đọc; 409 version_conflict hiện câu báo, không tự gửi lại.
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { cancelOrder, type CancelOrderRequest, type Order } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { CANCEL_REASON_TEXT, STAFF_CANCEL_REASONS, type StaffCancelReason } from "./labels";
import { reloadOrder } from "./orderData";

interface CancelBody {
  id: string;
  req: CancelOrderRequest;
}

export function CancelDialog({ order, onClose }: { order: Order; onClose: () => void }) {
  const qc = useQueryClient();
  const id = useId();
  const shipped = order.currentShipmentId !== undefined;
  const [reason, setReason] = useState<StaffCancelReason>("customer_changed_mind");
  const [note, setNote] = useState("");
  const write = useWriteAction((key, b: CancelBody) =>
    unwrap(cancelOrder({ path: { id: b.id }, body: b.req, headers: { "Idempotency-Key": key } })),
  );

  const submit = async () => {
    const n = note.trim();
    const r = await write.run({
      id: order.id,
      req: { version: order.version ?? 0, reason, ...(n === "" ? {} : { note: n }) },
    });
    if (!r.ok) return;
    reloadOrder(qc, order.id);
    onClose();
  };

  const reasons = (
    <div className="radios" role="radiogroup" aria-label="Lý do hủy">
      {STAFF_CANCEL_REASONS.map((r) => (
        <label key={r} className="check">
          <input
            type="radio"
            name={`${id}-ly-do`}
            checked={reason === r}
            onChange={() => {
              setReason(r);
            }}
          />{" "}
          {CANCEL_REASON_TEXT[r]}
        </label>
      ))}
    </div>
  );
  const select = (
    <label className="field">
      <span>Lý do</span>
      <span className="select-wrap">
        <select
          value={reason}
          onChange={(e) => {
            const r = STAFF_CANCEL_REASONS.find((x) => x === e.target.value);
            if (r !== undefined) setReason(r);
          }}
        >
          {STAFF_CANCEL_REASONS.map((r) => (
            <option key={r} value={r}>
              {CANCEL_REASON_TEXT[r]}
            </option>
          ))}
        </select>
      </span>
    </label>
  );

  return (
    <Dialog
      title={shipped ? `Yêu cầu hủy ${order.code}?` : `Hủy đơn ${order.code}?`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onClose}>
            Quay lại
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={write.pending}
            onClick={() => {
              void submit();
            }}
          >
            {shipped ? "Gửi yêu cầu hủy" : "Hủy đơn"}
          </button>
        </>
      }
    >
      {shipped && (
        <div className="box box-soft">
          Đơn đã gửi hãng. Đơn sẽ thành &quot;Đang yêu cầu hủy&quot;; hàng chưa trả về kho cho tới khi hãng xác nhận
          hủy. Hãng từ chối thì đơn quay lại &quot;Đã xác nhận&quot;.
        </div>
      )}
      {shipped ? select : reasons}
      <label className="field">
        <span>Ghi chú</span>
        <textarea
          className="input"
          style={{ minHeight: 72 }}
          placeholder="Không bắt buộc"
          maxLength={500}
          value={note}
          onChange={(e) => {
            setNote(e.target.value);
          }}
        />
      </label>
      {!shipped && (
        <p className="small muted">
          {`Hàng của đơn trả về có thể bán ngay. ${
            order.couponCode === undefined || order.couponCode === ""
              ? "Đơn không dùng mã giảm giá."
              : `Mã ${order.couponCode} được trả lượt nếu mã cho phép.`
          }`}
        </p>
      )}
      {write.message !== null && <div className="box box-err">{write.message}</div>}
    </Dialog>
  );
}
