// Quy tắc nhận hàng trả/hoàn theo món (mockup docs/mockups/m3/kho.html trạng thái D, E; HV26; kế hoạch M3 Task 136):
// mỗi dòng ba số Tốt / Hỏng / Thiếu, tổng phải bằng số trên dòng phiếu mới gửi được. Máy chủ kiểm lại (422
// qty_mismatch); hàm ở đây chỉ để khóa nút và nói rõ còn thiếu gì.
import type { ReceiveReturnRequest, Return } from "../../api";

export type QtyKey = "sellable" | "damaged" | "missing";
export const QTY_KEYS: readonly QtyKey[] = ["sellable", "damaged", "missing"];
export const QTY_TEXT: Readonly<Record<QtyKey, string>> = { sellable: "Tốt", damaged: "Hỏng", missing: "Thiếu" };

/** Ba số của một dòng (chuỗi người nhập; "" là 0). */
export type Split = Readonly<Record<QtyKey, string>>;
export const EMPTY_SPLIT: Split = { sellable: "0", damaged: "0", missing: "0" };

/** Số nguyên không âm từ ô nhập; "" → 0; chữ, số âm, số lẻ → null. */
export function parseQty(s: string): number | null {
  const t = s.trim();
  if (t === "") return 0;
  return /^\d{1,5}$/.test(t) ? Number(t) : null;
}

export interface LineCheck {
  orderItemId: string;
  qty: number;
  /** null khi có ô không phải số. */
  counted: number | null;
}

export interface ReceiveCheck {
  ok: boolean;
  /** Câu báo dưới các dòng (null khi đủ). */
  message: string | null;
  /** Dòng chưa khớp (để tô đỏ). */
  badLines: ReadonlySet<string>;
  totals: Readonly<Record<QtyKey, number>>;
}

/** Kiểm các dòng của phiếu với ba số đã nhập; skuOf cho tên dòng trong câu báo. */
export function checkReceive(
  ret: Return,
  splits: ReadonlyMap<string, Split>,
  skuOf: (orderItemId: string) => string,
): ReceiveCheck {
  const totals = { sellable: 0, damaged: 0, missing: 0 };
  const bad = new Set<string>();
  const invalid: string[] = [];
  const over: string[] = [];
  const short: string[] = [];
  let shortQty = 0;
  for (const it of ret.items) {
    const sp = splits.get(it.orderItemId) ?? EMPTY_SPLIT;
    const nums = QTY_KEYS.map((k) => parseQty(sp[k]));
    if (nums.some((n) => n === null)) {
      bad.add(it.orderItemId);
      invalid.push(skuOf(it.orderItemId));
      continue;
    }
    const [s = 0, d = 0, m = 0] = nums.map((n) => n ?? 0);
    totals.sellable += s;
    totals.damaged += d;
    totals.missing += m;
    const sum = s + d + m;
    if (sum > it.qty) {
      bad.add(it.orderItemId);
      over.push(`${skuOf(it.orderItemId)} thừa ${String(sum - it.qty)}`);
    } else if (sum < it.qty) {
      bad.add(it.orderItemId);
      shortQty += it.qty - sum;
      short.push(skuOf(it.orderItemId));
    }
  }
  let message: string | null = null;
  if (invalid.length > 0) message = `Chỉ nhập số nguyên không âm (${invalid.join(", ")}).`;
  else if (over.length > 0) message = `Tổng vượt số trên phiếu (${over.join(", ")}).`;
  else if (shortQty > 0) message = `Còn thiếu ${String(shortQty)} món chưa phân loại (${short.join(", ")}).`;
  return { ok: message === null && ret.items.length > 0, message, badLines: bad, totals };
}

/** Thân receiveReturn: mỗi dòng đủ ba số (máy chủ bỏ qua condition khi có đủ ba số; condition vẫn bắt buộc). */
export function receiveLines(ret: Return, splits: ReadonlyMap<string, Split>): ReceiveReturnRequest["lines"] {
  return ret.items.map((it) => {
    const sp = splits.get(it.orderItemId) ?? EMPTY_SPLIT;
    const [sellable = 0, damaged = 0, missing = 0] = QTY_KEYS.map((k) => parseQty(sp[k]) ?? 0);
    const condition: QtyKey = sellable > 0 ? "sellable" : damaged > 0 ? "damaged" : "missing";
    return { orderItemId: it.orderItemId, condition, sellable, damaged, missing };
  });
}

/** Phiếu của hãng (hoàn, giao một phần): hàng "gửi đi"; phiếu của khách: hàng "khách trả". */
export const fromCarrier = (ret: Return) => ret.kind === "carrier_return" || ret.kind === "partial_delivery";

/** Các câu "nhận xong thì…" theo tổng ba số (chỉ dòng có số > 0). */
export function receiveEffects(ret: Return, totals: Readonly<Record<QtyKey, number>>): string[] {
  const out: string[] = [];
  if (totals.sellable > 0) out.push(`${String(totals.sellable)} món tốt về "có thể bán"`);
  if (totals.damaged > 0) out.push(`${String(totals.damaged)} món hỏng vào "lỗi"`);
  if (totals.missing > 0) {
    out.push(
      fromCarrier(ret)
        ? `${String(totals.missing)} món thiếu ghi thất lạc (trừ tồn) và vào "Cần xem" để khiếu nại hãng`
        : `${String(totals.missing)} món thiếu không nhập kho, không hoàn tiền`,
    );
  }
  return out;
}
