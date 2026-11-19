// Luật của hộp thoại Trả/đổi (kế hoạch M2 Task M2-28; máy chủ: pricing.RefundFor và order.priceReturnLines của Task
// M2-22). Tiền hoàn ở đây chỉ để hiện trước; máy chủ tính lại và số của máy chủ là số cuối.
import type { OrderItem, Return, TimelineItem } from "../../api";

// floor(a × b / c) không tràn: nhân bằng BigInt như mulDiv (128 bit) của Go.
const mulDiv = (a: number, b: number, c: number): number => Number((BigInt(a) * BigInt(b)) / BigInt(c));

/**
 * Bản TypeScript của pricing.RefundFor: tiền hoàn cho returnNow đơn vị của một dòng có net, qty, khi đã trả
 * returnedBefore đơn vị và đã hoàn refundedBefore. Đơn vị cuối của dòng nhận phần còn lại. Đầu vào sai → 0.
 */
export function refundFor(
  net: number,
  qty: number,
  returnedBefore: number,
  returnNow: number,
  refundedBefore: number,
): number {
  if (
    net < 0 ||
    qty <= 0 ||
    returnedBefore < 0 ||
    returnNow <= 0 ||
    returnNow > qty - returnedBefore ||
    refundedBefore < 0 ||
    refundedBefore > net
  ) {
    return 0;
  }
  const left = net - refundedBefore;
  if (returnedBefore + returnNow === qty) return left;
  return Math.min(mulDiv(net, returnNow, qty), left);
}

export interface ReturnableLine {
  item: OrderItem;
  /** Đã trả (mọi phiếu chưa hủy, kể cả phiếu đang mở). */
  returned: number;
  remaining: number;
  /** Tiền hoàn dự kiến khi trả thêm n đơn vị. */
  refund(n: number): number;
}

/** Số đã trả, còn trả được và tiền hoàn của từng dòng đơn, tính như priceReturnLines của máy chủ. */
export function returnableLines(items: readonly OrderItem[], returns: readonly Return[]): ReturnableLine[] {
  const prior = returns.filter((r) => r.status !== "cancelled").flatMap((r) => r.items);
  return items.map((item) => {
    let returned = 0;
    let refunded = 0;
    for (const p of prior) {
      if (p.orderItemId !== item.id) continue;
      returned += p.qty;
      // Dòng "thiếu": tiền hoàn đã về 0 khi kho nhận; tính phần lẽ ra của nó để món cuối không nhận tiền món thiếu.
      refunded += p.condition === "missing" ? refundFor(item.netAmount, item.qty, 0, p.qty, 0) : p.refundAmount;
    }
    return {
      item,
      returned,
      remaining: Math.max(0, item.qty - returned),
      refund: (n: number) => refundFor(item.netAmount, item.qty, returned, n, refunded),
    };
  });
}

/** Lúc đơn chuyển sang "Thành công" theo lịch sử thao tác (mục status, to = delivered); không có → null. */
export function deliveredAt(timeline: readonly TimelineItem[]): string | null {
  return timeline.find((t) => t.kind === "status" && t.summary?.to === "delivered")?.at ?? null;
}

const DAY_MS = 86_400_000;
/** Số ngày trọn đã qua từ iso tới now. */
export function daysSince(iso: string, now: Date): number {
  return Math.floor((now.getTime() - new Date(iso).getTime()) / DAY_MS);
}
