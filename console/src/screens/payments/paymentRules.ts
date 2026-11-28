// Luật tiền của màn (kế hoạch M3 Task 138; mockup docs/mockups/m3/tien-tren-don.html, tra-doi.html): chữ của phương
// thức, nguồn nghĩa vụ, ngăn COD; COD sau khi thu; lúc ghi phiếu thu bị chặn (cash.RecordPayment); trạng thái nghĩa vụ
// hoàn; tiền cấn trừ ước tính của đơn đổi (cash refunds.creditAmount với đơn mới chưa thu). Máy chủ tính lại, số của
// máy chủ là số cuối.
import type { CodAccount, Order, Payment, RefundObligation } from "../../api";
import { formatMoney } from "../products/productRules";

export const METHOD_TEXT: Record<Payment["method"], string> = {
  bank_transfer: "Chuyển khoản",
  deposit: "Cọc",
  cash: "Tiền mặt",
  exchange_credit: "Cấn trừ món trả",
  refund: "Hoàn tiền cho khách",
};

/** Phương thức nhân viên ghi được ở "Ghi phiếu thu" (RecordPaymentRequest.method). */
export const RECORD_METHODS = ["bank_transfer", "deposit", "cash"] as const;

export const SOURCE_TEXT: Record<RefundObligation["source"], string> = {
  order_cancel: "Hủy đơn có cọc",
  customer_return: "Khách trả hàng",
  exchange: "Đổi hàng (dư tiền món trả)",
  partial_delivery: "Giao một phần",
};

export const ACCOUNT_TEXT: Record<CodAccount, string> = {
  to_collect: "Đang đi thu",
  carrier_holding: "Hãng đang giữ",
  shop_received: "Đã về",
  carrier_fee: "Phí hãng",
  sales_clearing: "Khoản trừ doanh thu",
};

export const ACCOUNTS: readonly CodAccount[] = [
  "to_collect",
  "carrier_holding",
  "shop_received",
  "carrier_fee",
  "sales_clearing",
];

export const TEXT_COD_LOCKED = "Đơn đã gửi hãng, không đổi được tiền thu hộ. Muốn đổi COD: hủy vận đơn rồi gửi lại.";
export const TEXT_NOT_COUNTED = "Chưa chi — chưa tính là đã hoàn";
export const TEXT_NEW_PREVIEW = "Số liệu vừa đổi, đây là bản xem trước mới.";

/** COD của đơn sau khi thu thêm amount: tổng − đã thu − amount, không âm (order.ApplyPaymentTotals). */
export function codAfter(grandTotal: number, prepaid: number, amount: number): number {
  return Math.max(grandTotal - prepaid - amount, 0);
}

/** Lý do không ghi được phiếu thu cho đơn o (null: ghi được), như cash.RecordPayment. */
export function paymentBlock(
  o: Pick<Order, "status" | "currentShipmentId" | "grandTotal" | "prepaidTotal">,
): string | null {
  if (o.currentShipmentId !== undefined) return TEXT_COD_LOCKED;
  if (o.status !== "draft" && o.status !== "new" && o.status !== "confirmed") {
    return "Đơn ở trạng thái này không ghi phiếu thu được";
  }
  if (o.grandTotal - (o.prepaidTotal ?? 0) <= 0) return "Đơn đã thu đủ tổng đơn";
  return null;
}

export interface ObligationState {
  chip: string;
  note: string;
  /** Còn phải hoàn (0 khi đã đóng). */
  remaining: number;
}

/** Chữ trạng thái của một nghĩa vụ hoàn (HV34): Chưa chi / Còn … / Đã chi đủ / Miễn. */
export function obligationState(o: RefundObligation): ObligationState {
  if (o.status === "waived") {
    return { chip: "Miễn", note: `Miễn · ${o.waiveReason ?? ""}`.trim(), remaining: 0 };
  }
  if (o.status === "paid") return { chip: "Đã chi đủ", note: `Đã chi ${formatMoney(o.paidAmount)}`, remaining: 0 };
  const remaining = Math.max(o.amount - o.paidAmount, 0);
  if (o.paidAmount === 0) return { chip: "Chưa chi", note: TEXT_NOT_COUNTED, remaining };
  return {
    chip: `Còn ${formatMoney(remaining)}`,
    note: `Đã chi ${formatMoney(o.paidAmount)}, còn nợ khách ${formatMoney(remaining)}`,
    remaining,
  };
}

export interface ExchangeMoney {
  /** Cấn trừ vào đơn mới. */
  credit: number;
  /** COD của đơn mới. */
  cod: number;
  /** Phần tiền món trả dư, thành nghĩa vụ hoàn khi kho nhận. */
  surplus: number;
}

/**
 * Tiền đơn đổi ước tính lúc tạo phiếu: tiền hoàn của món trả refund, tổng đơn mới total (phí ship 0, chưa thu): cấn
 * trừ = min(refund, total) (refunds.creditAmount), COD = total − cấn trừ, dư = refund − cấn trừ; không âm.
 */
export function exchangeMoney(refund: number, total: number): ExchangeMoney {
  const credit = Math.max(Math.min(refund, total), 0);
  return { credit, cod: Math.max(total - credit, 0), surplus: Math.max(refund - credit, 0) };
}

/** Phần đã cấn trừ vượt tiền hoàn cuối của phiếu đổi (kho nhận thiếu, Task 116); 0 khi không vượt. */
export function shortCredit(credited: number, finalRefund: number): number {
  return Math.max(credited - finalRefund, 0);
}
