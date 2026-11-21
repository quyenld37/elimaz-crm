// Nhận hàng trả/hoàn theo món (mockup docs/mockups/m3/kho.html trạng thái D, E; HV26, chủ shop OK 21/11; thay hộp một
// tình trạng cả dòng của M2): mỗi dòng ba ô Tốt / Hỏng / Thiếu; "Nhận vào kho" khóa tới khi tổng mỗi dòng bằng số trên
// phiếu, kèm câu "Còn thiếu n món chưa phân loại". Hai cách hiện: hộp thoại (chi tiết đơn, ReturnsCard) và thẻ trên
// trang Nhận hàng hoàn của kho (ReceiveReturnCard). Chỉ người có order.return.receive thấy (máy chủ vẫn chặn).
import { useState, type ReactNode } from "react";
import type { OrderItem, ReceiveReturnRequest, Return } from "../../api";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import {
  EMPTY_SPLIT,
  QTY_KEYS,
  QTY_TEXT,
  checkReceive,
  fromCarrier,
  receiveEffects,
  receiveLines,
  type QtyKey,
  type Split,
} from "./receiveRules";

export interface ReceiveReturnProps {
  ret: Return;
  items: readonly OrderItem[];
  pending: boolean;
  error: string | null;
  onReceive: (lines: ReceiveReturnRequest["lines"]) => void;
}

/** Trạng thái ba số của các dòng và phần thân (các dòng, câu báo, câu "nhận xong thì…"). */
function useReceiveForm({ ret, items }: Pick<ReceiveReturnProps, "ret" | "items">) {
  const [splits, setSplits] = useState<ReadonlyMap<string, Split>>(new Map());
  const item = (id: string) => items.find((i) => i.id === id);
  const skuOf = (id: string) => item(id)?.sku ?? "";
  const check = checkReceive(ret, splits, skuOf);
  const set = (id: string, k: QtyKey, v: string) => {
    setSplits((m) => new Map(m).set(id, { ...(m.get(id) ?? EMPTY_SPLIT), [k]: v }));
  };
  const sent = fromCarrier(ret) ? "Gửi đi" : "Khách trả";
  const body = (
    <>
      {ret.items.map((it) => {
        const sp = splits.get(it.orderItemId) ?? EMPTY_SPLIT;
        const sku = skuOf(it.orderItemId);
        return (
          <div key={it.id} className={check.badLines.has(it.orderItemId) ? "rline is-err" : "rline"}>
            <div className="sp">
              <span className="code">{sku}</span>
              <span className="nm clamp2">{item(it.orderItemId)?.name ?? ""}</span>
              <span className="xs">{`${sent} ${String(it.qty)}`}</span>
            </div>
            <div className="q3">
              {QTY_KEYS.map((k) => (
                <label key={k} className="field">
                  <span>{QTY_TEXT[k]}</span>
                  <input
                    className="input"
                    inputMode="numeric"
                    aria-label={`${QTY_TEXT[k]} ${sku}`}
                    value={sp[k]}
                    onChange={(e) => {
                      set(it.orderItemId, k, e.target.value);
                    }}
                  />
                </label>
              ))}
            </div>
          </div>
        );
      })}
      {check.message !== null ? (
        <p className="field-err" style={{ marginTop: 8 }}>
          {check.message}
        </p>
      ) : (
        <ul className="fx small" style={{ marginTop: 8 }} aria-label="Nhận xong">
          {receiveEffects(ret, check.totals).map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ul>
      )}
    </>
  );
  return { body, ok: check.ok, lines: () => receiveLines(ret, splits) };
}

export interface ReceiveReturnDialogProps extends ReceiveReturnProps {
  onClose: () => void;
}

export function ReceiveReturnDialog({ ret, items, pending, error, onClose, onReceive }: ReceiveReturnDialogProps) {
  const form = useReceiveForm({ ret, items });
  return (
    <Dialog
      title={`Nhận hàng trả · ${ret.code}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Đóng
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={pending || !form.ok}
            onClick={() => {
              onReceive(form.lines());
            }}
          >
            Nhận vào kho
          </button>
        </>
      }
    >
      {!fromCarrier(ret) && (
        <p className="small muted">
          Khách trả · hoàn dự kiến <span className="money">{formatMoney(ret.refundAmount)}</span>
        </p>
      )}
      {form.body}
      {!fromCarrier(ret) && (
        <p className="hint">
          Nhận xong thì tồn tăng và mở nghĩa vụ hoàn <span className="money">{formatMoney(ret.refundAmount)}</span> cho
          kế toán chi (món thiếu không hoàn).
        </p>
      )}
      {error !== null && <div className="box box-err">{error}</div>}
    </Dialog>
  );
}

export interface ReceiveReturnCardProps extends ReceiveReturnProps {
  /** Dòng phụ dưới tiêu đề (đơn, hãng, mã hãng, số món). */
  sub: ReactNode;
}

/** Thẻ nhận hàng hoàn của trang kho (trạng thái D). */
export function ReceiveReturnCard({ ret, items, pending, error, onReceive, sub }: ReceiveReturnCardProps) {
  const form = useReceiveForm({ ret, items });
  return (
    <section className="card" aria-label={`Nhận hàng hoàn · ${ret.code}`}>
      <div className="row-between">
        <h2>{`Nhận hàng hoàn · ${ret.code}`}</h2>
        <span className="chip">Chờ nhận</span>
      </div>
      <p className="small muted">{sub}</p>
      {form.body}
      {error !== null && <div className="box box-err">{error}</div>}
      <button
        type="button"
        className="btn btn-dark btn-wide"
        style={{ marginTop: 8 }}
        disabled={pending || !form.ok}
        onClick={() => {
          onReceive(form.lines());
        }}
      >
        Nhận vào kho
      </button>
    </section>
  );
}
