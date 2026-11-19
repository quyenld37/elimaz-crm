// Hộp thoại Nhận hàng trả (mockup docs/mockups/m2/don-chi-tiet.html trạng thái E; HV20). Chỉ người có
// order.return.receive thấy nút mở (máy chủ vẫn chặn). Một tình trạng cho cả dòng; bấm nhận là nhập kho ngay.
import { useState } from "react";
import type { OrderItem, Return } from "../../api";
import { Dialog } from "../staff/staffData";
import { CONDITION_TEXT, type Condition } from "./labels";

const CONDITIONS: readonly Condition[] = ["sellable", "damaged", "missing"];

export interface ReceiveReturnDialogProps {
  ret: Return;
  items: readonly OrderItem[];
  pending: boolean;
  error: string | null;
  onClose: () => void;
  onReceive: (lines: { orderItemId: string; condition: Condition }[]) => void;
}

export function ReceiveReturnDialog({ ret, items, pending, error, onClose, onReceive }: ReceiveReturnDialogProps) {
  const [cond, setCond] = useState<ReadonlyMap<string, Condition>>(new Map());
  const skuOf = (orderItemId: string) => items.find((i) => i.id === orderItemId)?.sku ?? "";
  return (
    <Dialog
      title={`Nhận hàng trả ${ret.code}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onClose}>
            Quay lại
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={pending}
            onClick={() => {
              onReceive(
                ret.items.map((it) => ({
                  orderItemId: it.orderItemId,
                  condition: cond.get(it.orderItemId) ?? "sellable",
                })),
              );
            }}
          >
            Nhận và nhập kho
          </button>
        </>
      }
    >
      {ret.items.map((it) => (
        <div key={it.id} className="field">
          <span>{`${skuOf(it.orderItemId)} × ${String(it.qty)}`}</span>
          <span className="select-wrap">
            <select
              aria-label={`Tình trạng ${skuOf(it.orderItemId)}`}
              value={cond.get(it.orderItemId) ?? "sellable"}
              onChange={(e) => {
                const c = CONDITIONS.find((x) => x === e.target.value);
                if (c !== undefined) setCond((m) => new Map(m).set(it.orderItemId, c));
              }}
            >
              {CONDITIONS.map((c) => (
                <option key={c} value={c}>
                  {CONDITION_TEXT[c]}
                </option>
              ))}
            </select>
          </span>
        </div>
      ))}
      <p className="small muted">
        Một tình trạng cho cả dòng. Hai món khác tình trạng (một bán được, một hỏng) thì tạo hai phiếu.
      </p>
      <p className="small muted">
        Bấm nhận là nhập kho ngay: món bán lại được tăng &quot;có thể bán&quot;; món hỏng vào hàng lỗi; món thiếu không
        hoàn tiền.
      </p>
      {error !== null && <div className="box box-err">{error}</div>}
    </Dialog>
  );
}
