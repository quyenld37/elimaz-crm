// Hộp thoại Trả/đổi (mockup docs/mockups/m2/don-chi-tiet.html trạng thái D; HV19). Chỉ đơn Thành công. Không cho chọn
// quá số còn trả được (trừ cả phiếu đang mở); tiền hoàn dự kiến tính như máy chủ (returnRules.refundFor), số máy chủ
// trả là số cuối. Quá số ngày đổi trả của shop chỉ cảnh báo (Q26). Tạo phiếu không đổi tồn; đổi hàng tạo đơn thay thế
// cùng khách, giao tới địa chỉ của đơn gốc, nên cần thêm quyền lên đơn ở kho của đơn gốc (Task 436, docs/quyen.md):
// không có order.create ở gán nào thì khóa "Đổi hàng"; có nhưng theo kho khác thì máy chủ trả 403, hộp thoại hiện lỗi.
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import {
  createReturn,
  type CreateReturnRequest,
  type Order,
  type Return,
  type TimelineItem,
  type VariantSearchHit,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { VariantPicker, optionText } from "../len-don/VariantPicker";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { orderKey, timelineKey, useOrderSettings } from "./orderData";
import { daysSince, deliveredAt, returnableLines } from "./returnRules";

const MAX_QTY = 10000;

interface CreateBody {
  orderId: string;
  req: CreateReturnRequest;
}

interface Swap {
  variantId: string;
  sku: string;
  name: string;
  qty: number;
}

const parseQty = (s: string): number | null => (/^\d+$/.test(s.trim()) ? Number(s.trim()) : null);

export interface ReturnDialogProps {
  order: Order;
  returns: readonly Return[];
  timeline: readonly TimelineItem[];
  canReadStock: boolean;
  /** Nhân viên có order.create ở ít nhất một gán (đơn thay thế của phiếu đổi là một lần lên đơn). */
  canExchange: boolean;
  onClose: () => void;
  /** Phiếu máy chủ vừa tạo. */
  onCreated: (r: Return) => void;
}

export function ReturnDialog({
  order,
  returns,
  timeline,
  canReadStock,
  canExchange,
  onClose,
  onCreated,
}: ReturnDialogProps) {
  const qc = useQueryClient();
  const settings = useOrderSettings();
  const productRef = useRef<HTMLInputElement>(null);
  const [kind, setKind] = useState<"customer_return" | "exchange">("customer_return");
  const [picked, setPicked] = useState<ReadonlyMap<string, string>>(new Map());
  const [reason, setReason] = useState("");
  const [swaps, setSwaps] = useState<Swap[]>([]);
  const write = useWriteAction((key, b: CreateBody) =>
    unwrap(createReturn({ path: { id: b.orderId }, body: b.req, headers: { "Idempotency-Key": key } })),
  );

  const lines = returnableLines(order.items, returns);
  const delivered = deliveredAt(timeline);
  const days = delivered === null ? null : daysSince(delivered, new Date());
  const limit = settings.data?.returnWarningDays;
  const late = days !== null && limit !== undefined && days > limit;

  const chosen = lines.flatMap((l) => {
    const t = picked.get(l.item.id);
    return t === undefined ? [] : [{ l, text: t, n: parseQty(t) }];
  });
  const bad = chosen.find((c) => c.n === null || c.n < 1 || c.n > c.l.remaining);
  const total = chosen.reduce(
    (s, c) => s + (c.n !== null && c.n >= 1 && c.n <= c.l.remaining ? c.l.refund(c.n) : 0),
    0,
  );
  const block =
    bad !== undefined
      ? `Dòng ${bad.l.item.sku} chỉ còn trả được ${String(bad.l.remaining)}`
      : chosen.length === 0
        ? "Chọn ít nhất một dòng"
        : kind === "exchange" && swaps.length === 0
          ? "Chọn hàng đổi cho khách"
          : null;

  const setQty = (itemId: string, text: string) => {
    setPicked((m) => new Map(m).set(itemId, text));
  };
  const toggle = (itemId: string, on: boolean) => {
    setPicked((m) => {
      const n = new Map(m);
      if (on) n.set(itemId, "1");
      else n.delete(itemId);
      return n;
    });
  };
  const addSwap = (h: VariantSearchHit) => {
    setSwaps((ss) =>
      ss.some((s) => s.variantId === h.variantId)
        ? ss.map((s) => (s.variantId === h.variantId ? { ...s, qty: Math.min(MAX_QTY, s.qty + 1) } : s))
        : [...ss, { variantId: h.variantId, sku: h.sku, name: `${h.productName} ${optionText(h)}`.trim(), qty: 1 }],
    );
  };

  const submit = async () => {
    if (block !== null || write.pending) return;
    const r = reason.trim();
    const req: CreateReturnRequest = {
      kind,
      lines: chosen.map((c) => ({ orderItemId: c.l.item.id, qty: c.n ?? 0 })),
      ...(r === "" ? {} : { reason: r }),
      ...(kind === "exchange"
        ? { replacement: { items: swaps.map((s) => ({ variantId: s.variantId, qty: s.qty })) } }
        : {}),
    };
    const res = await write.run({ orderId: order.id, req });
    if (!res.ok) return;
    void qc.invalidateQueries({ queryKey: orderKey(order.id) });
    void qc.invalidateQueries({ queryKey: timelineKey(order.id) });
    onCreated(res.data);
    onClose();
  };

  return (
    <Dialog
      title={`Trả/đổi hàng · ${order.code}`}
      className="dlg-wide"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onClose}>
            Quay lại
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={block !== null || write.pending}
            onClick={() => {
              void submit();
            }}
          >
            {kind === "exchange" ? "Tạo phiếu đổi" : "Tạo phiếu trả"}
          </button>
        </>
      }
    >
      <div className="row" role="radiogroup" aria-label="Loại phiếu">
        {(["customer_return", "exchange"] as const).map((k) => (
          <button
            key={k}
            type="button"
            role="radio"
            className="toggle"
            aria-checked={kind === k}
            aria-pressed={kind === k}
            disabled={k === "exchange" && !canExchange}
            onClick={() => {
              setKind(k);
            }}
          >
            {k === "exchange" ? "Đổi hàng" : "Khách trả hàng"}
          </button>
        ))}
      </div>
      {!canExchange && <p className="hint">Đổi hàng cần quyền lên đơn ở kho của đơn này.</p>}
      {late && (
        <div className="box box-soft">
          {`Đơn giao thành công ${String(days)} ngày trước, quá ${String(limit)} ngày đổi trả của shop. Vẫn tạo được phiếu.`}
        </div>
      )}
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              <th>Dòng</th>
              <th className="r">Đã mua</th>
              <th className="r">Đã trả</th>
              <th>Trả lần này</th>
              <th className="r">Hoàn dự kiến</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l) => {
              const t = picked.get(l.item.id);
              const n = t === undefined ? null : parseQty(t);
              const ok = n !== null && n >= 1 && n <= l.remaining;
              const none = l.remaining === 0;
              return (
                <tr key={l.item.id} className={none ? "is-muted" : undefined}>
                  <td className="lead">
                    <label className={none ? "check is-off" : "check"}>
                      <input
                        type="checkbox"
                        disabled={none}
                        checked={t !== undefined}
                        onChange={(e) => {
                          toggle(l.item.id, e.target.checked);
                        }}
                      />{" "}
                      <span>{l.item.sku}</span>
                    </label>
                  </td>
                  <td data-label="Đã mua" className="r num">
                    {l.item.qty}
                  </td>
                  <td data-label="Đã trả" className="r num">
                    {l.returned}
                  </td>
                  <td data-label="Trả lần này" className={none ? "muted" : undefined}>
                    {none ? (
                      "Đã trả hết"
                    ) : t === undefined ? (
                      "—"
                    ) : (
                      <span className="stepper">
                        <button
                          type="button"
                          className="ibtn"
                          aria-label="Bớt 1"
                          disabled={n === null || n <= 1}
                          onClick={() => {
                            setQty(l.item.id, String((n ?? 2) - 1));
                          }}
                        >
                          −
                        </button>
                        <input
                          className={ok ? "input" : "input is-err"}
                          inputMode="numeric"
                          aria-label={`Số lượng trả ${l.item.sku}`}
                          aria-invalid={!ok}
                          value={t}
                          onChange={(e) => {
                            setQty(l.item.id, e.target.value);
                          }}
                        />
                        <button
                          type="button"
                          className="ibtn"
                          aria-label="Thêm 1"
                          disabled={n === null || n >= l.remaining}
                          onClick={() => {
                            setQty(l.item.id, String((n ?? 0) + 1));
                          }}
                        >
                          +
                        </button>
                      </span>
                    )}
                  </td>
                  <td data-label="Hoàn dự kiến" className="r">
                    <span className="money">{formatMoney(ok ? l.refund(n) : 0)}</span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {bad !== undefined && <p className="field-err">{block}</p>}
      <p className="small">
        Hoàn dự kiến chia theo giá sau giảm của từng món; món cuối của dòng nhận phần lẻ (&quot;Đã trả&quot; tính cả
        phiếu đang mở). Tổng: <span className="money">{formatMoney(total)}</span>. Tiền hoàn ghi ở M3.
      </p>
      {kind === "exchange" && (
        <div className="stack-sm">
          <p className="label">Hàng đổi cho khách</p>
          <VariantPicker onPick={addSwap} inputRef={productRef} canReadStock={canReadStock} />
          {swaps.length === 0 ? (
            <p className="reason">Chọn hàng đổi cho khách</p>
          ) : (
            <ul className="mini">
              {swaps.map((s) => (
                <li key={s.variantId}>
                  <span>{`${s.sku} × ${String(s.qty)}`}</span>
                  <button
                    type="button"
                    className="btn btn-quiet"
                    onClick={() => {
                      setSwaps((ss) => ss.filter((x) => x.variantId !== s.variantId));
                    }}
                  >
                    Bỏ
                  </button>
                </li>
              ))}
            </ul>
          )}
          <p className="hint">Đơn đổi cùng khách, giao tới địa chỉ của đơn này, không dùng mã giảm giá.</p>
        </div>
      )}
      <label className="field">
        <span>Lý do trả</span>
        <input
          className="input"
          maxLength={500}
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      {write.message !== null && <div className="box box-err">{write.message}</div>}
    </Dialog>
  );
}
