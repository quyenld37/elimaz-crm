// Chữ tiếng Việt của nhãn đơn, trạng thái, lý do hủy, lý do chờ, phiếu trả (kế hoạch M2 Task M2-28; bảng nhãn ở Task
// M2-08, lý do hủy Q18). 17 nhãn (sending của M3 Task M3-28) đúng thứ tự orderview.Labels()
// (internal/queries/orderview/label.go): nguồn duy nhất của nhãn là máy chủ, màn chỉ đổi khóa sang chữ. Câu lý do từ chối mã: COUPON_REASON_MESSAGES (src/app/errors.ts).
import type { CancelReason, OrderLabel, OrderStatus, PendingReason, ReturnKind, ReturnStatus } from "../../api";

/** 17 nhãn theo thứ tự thanh nhãn (orderview.Labels()). */
export const LABEL_ORDER: readonly OrderLabel[] = [
  "draft",
  "new",
  "awaiting_discount_approval",
  "awaiting_customer",
  "out_of_stock",
  "confirmed",
  "packing",
  "packed",
  "sending",
  "awaiting_pickup",
  "shipping",
  "delivery_failed",
  "returning",
  "cancel_requested",
  "cancelled",
  "delivered",
  "returned",
];

export const LABEL_TEXT: Readonly<Record<OrderLabel, string>> = {
  draft: "Nháp",
  new: "Mới",
  awaiting_discount_approval: "Chờ duyệt giảm giá",
  awaiting_customer: "Chờ khách xác nhận",
  out_of_stock: "Hết hàng",
  confirmed: "Đã xác nhận",
  packing: "Đang đóng gói",
  packed: "Đã đóng gói",
  sending: "Đang gửi hãng",
  awaiting_pickup: "Chờ thu gom",
  shipping: "Đang chuyển",
  delivery_failed: "Thất bại",
  returning: "Đang chuyển hoàn",
  cancel_requested: "Đang yêu cầu hủy",
  cancelled: "Đã hủy",
  delivered: "Thành công",
  returned: "Đã hoàn",
};

// Nhãn của vận chuyển (đóng gói, vận đơn) lọc được từ M3 (mockup trạng thái C; máy chủ trả 422 với nhãn vận đơn).
const SHIPPING_LABELS: ReadonlySet<OrderLabel> = new Set([
  "packing",
  "packed",
  "sending",
  "awaiting_pickup",
  "shipping",
  "delivery_failed",
  "returning",
]);

/** Nhãn trên thanh lọc của danh sách (mockup trạng thái A); sau cùng là nút "Tất cả". */
export const LABEL_BAR: readonly OrderLabel[] = LABEL_ORDER.filter((l) => !SHIPPING_LABELS.has(l));

// Nhãn cần người xử lý: viền đỏ trên thanh nhãn, chip đỏ ở dòng (HV14).
const ATTENTION: ReadonlySet<OrderLabel> = new Set(["awaiting_discount_approval", "out_of_stock", "cancel_requested"]);
export const isAttention = (l: OrderLabel): boolean => ATTENTION.has(l);

/** Lớp chip của nhãn: đỏ cho nhãn cần xử lý và Thất bại, xanh cho Thành công. */
export function labelChip(l: OrderLabel): string {
  if (isAttention(l) || l === "delivery_failed") return "chip chip-err";
  return l === "delivered" ? "chip chip-ok" : "chip";
}

export const STATUS_TEXT: Readonly<Record<OrderStatus, string>> = {
  draft: "Nháp",
  new: "Mới",
  confirmed: "Đã xác nhận",
  cancel_requested: "Đang yêu cầu hủy",
  cancelled: "Đã hủy",
  delivered: "Thành công",
  returned: "Đã hoàn",
};

export const PENDING_REASON_TEXT: Readonly<Record<PendingReason, string>> = {
  awaiting_customer: "Chờ khách xác nhận",
  out_of_stock: "Hết hàng",
  discount_approval: "Chờ duyệt giảm giá",
};

export const CANCEL_REASON_TEXT: Readonly<Record<CancelReason, string>> = {
  customer_changed_mind: "Khách đổi ý",
  out_of_stock: "Hết hàng",
  unreachable: "Không liên lạc được",
  wrong_info: "Sai thông tin",
  other: "Lý do khác",
  draft_expired: "Đơn nháp hết hạn",
  duplicate: "Đơn trùng",
};

/** Lý do nhân viên chọn khi hủy (CancelOrderRequest.reason), thứ tự của mockup. */
export const STAFF_CANCEL_REASONS = [
  "customer_changed_mind",
  "out_of_stock",
  "unreachable",
  "wrong_info",
  "other",
] as const;
export type StaffCancelReason = (typeof STAFF_CANCEL_REASONS)[number];

export const RETURN_KIND_TEXT: Readonly<Record<ReturnKind, string>> = {
  carrier_return: "Hãng hoàn",
  customer_return: "Khách trả",
  exchange: "Đổi",
  partial_delivery: "Giao một phần",
};

export const RETURN_STATUS_TEXT: Readonly<Record<ReturnStatus, string>> = {
  requested: "Chờ nhận hàng",
  received: "Đã nhận",
  restocked: "Đã nhập kho",
  closed: "Đã đóng",
  cancelled: "Đã hủy",
};

export type Condition = "sellable" | "damaged" | "missing";
export const CONDITION_TEXT: Readonly<Record<Condition, string>> = {
  sellable: "Bán lại được (vào tồn)",
  damaged: "Hỏng (vào hàng lỗi)",
  missing: "Thiếu (không về kho)",
};

// Lý do máy chủ ghi ở lịch sử trạng thái ngoài lý do hủy (internal/modules/order: discountpolicy.go, confirm.go,
// cancel.go, returns.go).
const SERVER_REASON_TEXT: Readonly<Record<string, string>> = {
  manual_discount_over_threshold: "Giảm tay vượt mức duyệt",
  discount_approved: "Đã duyệt giảm giá",
  carrier_rejected_cancel: "Hãng từ chối hủy",
  carrier_returned: "Hãng hoàn hàng",
};

/** Chữ của lý do ở mục đổi trạng thái; lý do lạ → null (không hiện mã máy). */
export function historyReasonText(reason: string | undefined): string | null {
  if (reason === undefined) return null;
  const all: Readonly<Record<string, string>> = { ...CANCEL_REASON_TEXT, ...SERVER_REASON_TEXT };
  return Object.hasOwn(all, reason) ? (all[reason] ?? null) : null;
}

/** Tên trường đã sửa ở mục edit của lịch sử (EditField* của internal/modules/order/edit.go). */
export const FIELD_TEXT: Readonly<Record<string, string>> = {
  lines: "Sản phẩm",
  orderDiscount: "Giảm cấp đơn",
  couponCode: "Mã giảm giá",
  shippingFee: "Phí ship",
  shipAddress: "Địa chỉ giao",
  carrierNote: "Ghi chú cho hãng",
  careBy: "Người phụ trách",
  trafficSourceId: "Nguồn đơn",
};
