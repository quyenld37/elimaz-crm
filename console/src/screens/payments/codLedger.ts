// Chữ của sổ COD một vận đơn (kế hoạch M3 Task 138; mockup docs/mockups/m3/tien-tren-don.html trạng thái D): mỗi bút
// toán một dòng "<loại> <số> · <ngăn đi> → <ngăn đến>"; bút toán đảo kèm lý do và người làm. Sổ chỉ ghi thêm: đảo là
// một bút toán mới trỏ bút toán cũ (reversesTransferId), không xóa dòng nào.
import type { CodTransfer } from "../../api";
import { shortTime } from "../../app/time";
import { formatMoney } from "../products/productRules";
import { ACCOUNT_TEXT } from "./paymentRules";

export const KIND_TEXT: Record<CodTransfer["kind"], string> = {
  expected: "Chờ thu",
  collected: "Hãng thu",
  remitted: "Hãng chuyển tiền",
  returned: "Hoàn",
  adjustment: "Điều chỉnh",
  fee: "Phí hãng",
  reversal: "Đảo bút toán",
};

const signed = (v: number) => (v < 0 ? `−${formatMoney(-v)}` : formatMoney(v));

/** Phần số tiền của bút toán t: hai vế ngược dấu bằng nhau → "<số> · <ngăn đi> → <ngăn đến>", còn lại kê từng ngăn. */
export function entriesText(t: Pick<CodTransfer, "entries">): string {
  const [a, b] = t.entries;
  if (t.entries.length === 2 && a !== undefined && b !== undefined && a.amount === -b.amount && a.amount !== 0) {
    const [out, into] = a.amount < 0 ? [a, b] : [b, a];
    return `${formatMoney(into.amount)} · ${ACCOUNT_TEXT[out.account]} → ${ACCOUNT_TEXT[into.account]}`;
  }
  return t.entries.map((e) => `${ACCOUNT_TEXT[e.account]} ${signed(e.amount)}`).join(", ");
}

/** Một dòng của bút toán t: loại và số tiền. */
export function transferText(t: CodTransfer): string {
  return `${KIND_TEXT[t.kind]} ${entriesText(t)} · ${shortTime(t.occurredAt)}`;
}

/** Sổ mới nhất trước (không đổi mảng gốc). */
export function newestFirst(ts: readonly CodTransfer[]): CodTransfer[] {
  return [...ts].sort((x, y) => y.occurredAt.localeCompare(x.occurredAt));
}

/** Bút toán còn đảo được: không phải bút toán đảo, chưa bị bút toán đảo nào trỏ tới. */
export function reversible(ts: readonly CodTransfer[]): CodTransfer[] {
  const reversed = new Set(ts.flatMap((t) => (t.reversesTransferId === undefined ? [] : [t.reversesTransferId])));
  return newestFirst(ts).filter((t) => t.kind !== "reversal" && !reversed.has(t.id));
}
