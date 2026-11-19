// Luật chữ và dựng thân yêu cầu của màn Lên đơn (kế hoạch M2 Task M2-27; mockup docs/mockups/m2/len-don.html). Không
// gọi API; OrderForm dùng các hàm này để quyết định nút mờ, câu nhắc và thân createOrder.
import type {
  Address,
  CreateOrderRequest,
  EditScope,
  Order,
  OrderWarning,
  PendingReason,
  ShipAddress,
  UpdateOrderRequest,
} from "../../api";
import { COUPON_REASON_MESSAGES, ERROR_MESSAGES } from "../../app/errors";
import { shortTime } from "../../app/time";

export type Intent = "draft" | "new" | "confirmed";

export const TEXT_NEED_CUSTOMER_AND_LINES = "Chưa lưu được: chọn khách và thêm ít nhất một sản phẩm.";
export const TEXT_NEED_ADDRESS = "Lưu và xác nhận cần đủ người nhận, SĐT, tỉnh, xã/phường, số nhà.";
export const TEXT_PARTIAL_ADDRESS = "Địa chỉ giao đang dở: điền đủ người nhận, SĐT, số nhà hoặc xóa hết rồi lưu.";
export const TEXT_NEW_CUSTOMER_NAME = "Khách mới: điền tên người nhận để làm tên khách.";

/** Kết quả phân loại chữ gõ ở ô tìm khách theo luật của customer.Search (Task M2-10). */
export type CustomerQuery = { kind: "empty" } | { kind: "short"; text: string } | { kind: "query"; q: string };

const MIN_TAIL_DIGITS = 4;
const MIN_NAME_CHARS = 2;

/** Chữ số (đã bỏ khoảng trắng, chấm, gạch, "+") dưới 4 → nhắc 4 số cuối; tên dưới 2 ký tự → nhắc; còn lại → tìm. */
export function customerQuery(text: string): CustomerQuery {
  const t = text.trim();
  if (t === "") return { kind: "empty" };
  if (/^[\d\s.+-]+$/.test(t)) {
    return t.replace(/\D/g, "").length < MIN_TAIL_DIGITS
      ? { kind: "short", text: "Gõ ít nhất 4 số cuối" }
      : { kind: "query", q: t };
  }
  if (Array.from(t).length < MIN_NAME_CHARS) return { kind: "short", text: "Gõ ít nhất 2 ký tự của tên" };
  return { kind: "query", q: t };
}

/** SĐT trong gợi ý tìm khách: chỉ hiện 4 số cuối (HV05: che giữa số tới khi chọn khách). */
export function maskPhone(phone: string): string {
  const d = phone.replace(/\D/g, "");
  return d.length < MIN_TAIL_DIGITS ? "••••" : `•••• •• ${d.slice(-MIN_TAIL_DIGITS)}`;
}

/** SĐT đầy đủ của khách đã chọn: "+84912345678" → "0912 345 678"; dạng khác giữ nguyên. */
export function displayPhone(phone: string): string {
  const d = phone.replace(/\D/g, "");
  const local = d.startsWith("84") && d.length === 11 ? `0${d.slice(2)}` : d;
  return /^0\d{9}$/.test(local) ? `${local.slice(0, 4)} ${local.slice(4, 7)} ${local.slice(7)}` : phone;
}

/** Ngày/tháng theo giờ Việt Nam ("18/11"); chuỗi không phải giờ → "". */
export function shortDate(iso: string): string {
  return shortTime(iso).split(" ")[1] ?? "";
}

/** SĐT nhập tay → dạng ShipAddress.phone (^\+?[0-9]{8,15}$): bỏ khoảng trắng, chấm, gạch. */
export function cleanPhone(s: string): string {
  return s.trim().replace(/[\s.-]/g, "");
}

/** Câu cho lý do từ chối mã; lý do lạ hay thiếu → câu chung của coupon_rejected. */
export function couponReasonText(reason: string | undefined): string {
  const generic = ERROR_MESSAGES.coupon_rejected ?? "";
  if (reason === undefined) return generic;
  return Object.hasOwn(COUPON_REASON_MESSAGES, reason) ? (COUPON_REASON_MESSAGES[reason] ?? generic) : generic;
}

/** Câu sau khi tạo cho lý do chờ của đơn mới (PendingReason). */
export const PENDING_TEXT: Readonly<Record<PendingReason, string>> = {
  discount_approval: "Đơn đang chờ duyệt giảm giá.",
  awaiting_customer: "Đơn đang chờ khách xác nhận.",
  out_of_stock: "Đơn đang chờ hàng.",
};

/** Câu cho cảnh báo kèm đơn vừa tạo (OrderWarning.code là chuỗi mở; mã lạ thì bỏ qua). */
export function warningText(w: OrderWarning): string | null {
  switch (w.code) {
    case "phone_belongs_to_other_customer":
      return "SĐT người nhận thuộc hồ sơ khách khác (không tự gộp).";
    case "possible_duplicate":
      return 'Có thể trùng với đơn khác (dấu "Nghi trùng" hiện sau vài giây).';
    default:
      return null;
  }
}

export interface AddressInput {
  name: string;
  phone: string;
  line: string;
  provinceCode: string;
  wardCode: string;
  /** Tên xã đã chọn (chỉ để hiện trong ô; không gửi). */
  wardName?: string;
}

export const EMPTY_ADDRESS: AddressInput = { name: "", phone: "", line: "", provinceCode: "", wardCode: "" };

/** complete: đủ người nhận, SĐT, số nhà, tỉnh, xã; empty: chưa điền gì; partial: còn lại. */
export function addressState(a: AddressInput): "complete" | "empty" | "partial" {
  const filled = [a.name, a.phone, a.line, a.provinceCode, a.wardCode].map((v) => v.trim() !== "");
  if (filled.every(Boolean)) return "complete";
  if (!filled.some(Boolean)) return "empty";
  return "partial";
}

// Thân shipAddress gửi được khi có đủ ba trường bắt buộc (name, phone, line) của ShipAddress.
const hasBaseAddress = (a: AddressInput) => [a.name, a.phone, a.line].every((v) => v.trim() !== "");

export type PickedCustomer = { kind: "existing"; id: string } | { kind: "new" };

export interface FormLine {
  /** Dòng đang có của đơn đang sửa (giữ đơn giá cũ); không có: dòng mới. */
  itemId?: string;
  variantId: string;
  qty: number;
  lineDiscount: number;
}

export interface FormState {
  customer: PickedCustomer | null;
  lines: FormLine[];
  address: AddressInput;
  phoneIsCustomers: boolean;
  orderDiscount: number;
  /** Mã đã áp (rỗng: không dùng mã). */
  couponCode: string;
  shippingFee: number;
  trafficSourceId: string;
  carrierNote: string;
}

/** Lý do nút lưu theo ý định bị mờ (HV04); null khi lưu được. Máy chủ vẫn kiểm lại. */
export function saveBlock(f: FormState, intent: Intent): string | null {
  if (f.customer === null || f.lines.length === 0) return TEXT_NEED_CUSTOMER_AND_LINES;
  const st = addressState(f.address);
  // Ý định confirmed cần địa chỉ đủ tỉnh, xã (máy chủ: address_incomplete).
  if (intent === "confirmed" && st !== "complete") return TEXT_NEED_ADDRESS;
  if (st === "partial" && !hasBaseAddress(f.address)) {
    return intent === "confirmed" ? TEXT_NEED_ADDRESS : TEXT_PARTIAL_ADDRESS;
  }
  if (f.customer.kind === "new" && f.address.name.trim() === "") return TEXT_NEW_CUSTOMER_NAME;
  return null;
}

// Thân ShipAddress khi có đủ ba trường bắt buộc; mã tỉnh, xã chỉ gửi khi có (xã cần tỉnh).
function shipAddressOf(a: AddressInput): ShipAddress | undefined {
  if (!hasBaseAddress(a)) return undefined;
  return {
    name: a.name.trim(),
    phone: cleanPhone(a.phone),
    line: a.line.trim(),
    ...(a.provinceCode === "" ? {} : { provinceCode: a.provinceCode }),
    ...(a.provinceCode === "" || a.wardCode === "" ? {} : { wardCode: a.wardCode }),
  };
}

/** Thân createOrder theo ý định; chỉ gửi trường có giá trị. Kênh "manual" (đơn nhập tay, DH04). */
export function buildRequest(f: FormState, intent: Intent): CreateOrderRequest {
  const a = f.address;
  const shipAddress = shipAddressOf(a);
  const code = f.couponCode.trim().toUpperCase();
  const note = f.carrierNote.trim();
  const body: CreateOrderRequest = {
    ...(f.customer?.kind === "existing" ? { customerId: f.customer.id } : {}),
    ...(f.customer?.kind === "new"
      ? {
          customer: {
            fullName: a.name.trim(),
            ...(a.phone.trim() === "" ? {} : { phone: cleanPhone(a.phone), phoneIsCustomers: f.phoneIsCustomers }),
          },
        }
      : {}),
    intent,
    items: f.lines.map((l) => ({
      variantId: l.variantId,
      qty: l.qty,
      ...(l.lineDiscount > 0 ? { lineDiscount: l.lineDiscount } : {}),
    })),
    ...(shipAddress === undefined ? {} : { shipAddress }),
    shippingFee: f.shippingFee,
    ...(f.orderDiscount > 0 ? { orderDiscount: f.orderDiscount } : {}),
    ...(code === "" ? {} : { couponCode: code }),
    ...(f.trafficSourceId === "" ? {} : { trafficSourceId: f.trafficSourceId }),
    ...(note === "" ? {} : { carrierNote: note }),
    channel: "manual",
  };
  return body;
}

/** Địa chỉ giao của đơn thành ô nhập của form (sửa đơn); SĐT dạng đọc được. */
export function addressOf(a: Address | undefined): AddressInput {
  if (a === undefined) return EMPTY_ADDRESS;
  return {
    name: a.name,
    phone: displayPhone(a.phone),
    line: a.line,
    provinceCode: a.provinceCode ?? "",
    wardCode: a.wardCode ?? "",
    ...(a.wardName === undefined ? {} : { wardName: a.wardName }),
  };
}

// SĐT so sánh được: chỉ chữ số, đầu 84 (11 số) đổi thành 0.
const phoneKey = (p: string) => {
  const d = p.replace(/\D/g, "");
  return d.startsWith("84") && d.length === 11 ? `0${d.slice(2)}` : d;
};

const sameAddress = (a: ShipAddress | undefined, b: Address | undefined): boolean =>
  a === undefined || b === undefined
    ? a === b
    : a.name === b.name.trim() &&
      phoneKey(a.phone) === phoneKey(b.phone) &&
      a.line === b.line.trim() &&
      (a.provinceCode ?? "") === (b.provinceCode ?? "") &&
      (a.wardCode ?? "") === (b.wardCode ?? "");

/**
 * Thân updateOrder của form sửa đơn o (Task 85): version đã đọc và chỉ những trường khác giá trị hiện tại và thuộc
 * phạm vi được sửa (editScope; máy chủ vẫn kiểm). Dòng gửi đủ danh sách mong muốn (dòng không có là bị bỏ). null khi
 * không có gì để gửi.
 */
export function buildUpdate(f: FormState, o: Order, scope: EditScope): UpdateOrderRequest | null {
  const body: UpdateOrderRequest = { version: o.version ?? 0 };
  const put = <K extends keyof UpdateOrderRequest>(k: K, v: UpdateOrderRequest[K]) => {
    body[k] = v;
  };
  const lines = f.lines.map((l) => ({
    ...(l.itemId === undefined ? {} : { itemId: l.itemId }),
    variantId: l.variantId,
    qty: l.qty,
    ...(l.lineDiscount > 0 ? { lineDiscount: l.lineDiscount } : {}),
  }));
  const curLines = o.items.map((it) => ({
    itemId: it.id,
    variantId: it.variantId,
    qty: it.qty,
    ...((it.lineDiscount ?? 0) > 0 ? { lineDiscount: it.lineDiscount } : {}),
  }));
  if (scope.lines && JSON.stringify(lines) !== JSON.stringify(curLines)) put("lines", lines);
  if (scope.discounts && f.orderDiscount !== (o.orderDiscount ?? 0)) put("orderDiscount", f.orderDiscount);
  const code = f.couponCode.trim().toUpperCase();
  if (scope.coupon && code !== (o.couponCode ?? "")) put("couponCode", code);
  if (scope.shipping && f.shippingFee !== o.shippingFee) put("shippingFee", f.shippingFee);
  const addr = shipAddressOf(f.address);
  if (scope.address && addr !== undefined && !sameAddress(addr, o.shipAddress)) put("shipAddress", addr);
  const note = f.carrierNote.trim();
  if (scope.carrierNote && note !== (o.carrierNote ?? "")) put("carrierNote", note);
  if (scope.trafficSource && f.trafficSourceId !== "" && f.trafficSourceId !== (o.trafficSourceId ?? "")) {
    put("trafficSourceId", f.trafficSourceId);
  }
  // Chỉ còn version: không có gì để sửa.
  return Object.keys(body).length > 1 ? body : null;
}
