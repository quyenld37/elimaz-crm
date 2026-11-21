// Luật thuần của khối Vận chuyển sau khi gửi (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng
// thái D–G): nguồn và lý do bỏ qua của sự kiện, nhãn chip, giờ kẹt, câu lỗi đồng bộ, việc làm được theo trạng thái, giờ
// nhập tay theo múi Việt Nam. Máy chủ vẫn kiểm mọi việc; ở đây chỉ để vẽ nút và câu.
import type { Shipment, ShipmentEvent, ShipmentStatus } from "../../api";

/** Nguồn của sự kiện: chữ trên chip và chip đậm cho việc của nhân viên. */
export function sourceText(source: ShipmentEvent["source"]): { text: string; staff: boolean } {
  switch (source) {
    case "staff":
      return { text: "Nhân viên", staff: true };
    case "system":
      return { text: "Hệ thống", staff: false };
    case "carrier_webhook":
      return { text: "Hãng (webhook)", staff: false };
    default:
      return { text: "Hãng (API)", staff: false };
  }
}

const IGNORED_TEXT: Readonly<Record<string, string>> = {
  older: "cũ hơn trạng thái hiện tại",
  duplicate_state: "trùng trạng thái hiện tại",
  not_current: "vận đơn không còn là vận đơn hiện hành của đơn",
  invalid_edge: "không đi được từ trạng thái hiện tại",
  unmapped: "trạng thái lạ của hãng, chờ người xem",
  review: "chờ người xem",
};

/** Sự kiện bị bỏ qua (applied false): câu "Bỏ qua: …"; sự kiện đã áp hoặc chưa xét → null. */
export function ignoredText(e: ShipmentEvent): string | null {
  if (e.applied !== false) return null;
  const r = e.ignoredReason ?? "";
  return `Bỏ qua: ${Object.hasOwn(IGNORED_TEXT, r) ? (IGNORED_TEXT[r] ?? r) : "không đổi trạng thái"}`;
}

/** Lớp chip của nhãn trạng thái vận đơn. */
export function statusChip(status: ShipmentStatus): string {
  switch (status) {
    case "delivered":
      return "chip chip-ok";
    case "cancel_requested":
    case "delivery_failed":
    case "returning":
      return "chip chip-err";
    case "cancelled":
    case "returned":
      return "chip";
    default:
      return "chip chip-dark";
  }
}

/** Nhãn trạng thái khi máy chủ không gửi label (nhãn chính của máy chủ luôn ưu tiên). */
export const STATUS_TEXT: Readonly<Record<ShipmentStatus, string>> = {
  creating: "Đang gửi hãng",
  awaiting_pickup: "Chờ thu gom",
  shipping: "Đang chuyển",
  delivery_failed: "Thất bại",
  delivered: "Thành công",
  returning: "Đang chuyển hoàn",
  returned: "Đã hoàn về kho",
  cancel_requested: "Đang yêu cầu hủy",
  cancelled: "Đã hủy",
};

export const statusText = (s: Pick<Shipment, "status" | "label">) =>
  s.label === undefined || s.label === "" ? STATUS_TEXT[s.status] : s.label;

const SYNC_ERROR_TEXT: Readonly<Record<string, string>> = {
  timeout: "hãng không trả lời",
  rate_limited: "hãng đang bận, quá giới hạn yêu cầu",
  auth: "tài khoản hãng cần cấp quyền lại",
  not_supported: "hãng không cho tra trạng thái",
  carrier_inconclusive: "hãng không trả lời chắc chắn",
  shipment_not_found: "hãng không tìm thấy vận đơn",
  tracking_conflict: "mã của hãng khác mã đã lưu",
  stock_short: "tồn trong kho không khớp, chờ người xem",
  apply_failed: "CRM chưa áp được kết quả, chờ người xem",
};

/** Câu của mã lỗi đồng bộ (Shipment.lastSyncError); mã lạ → câu chung, không bao giờ chữ thô của hãng. */
export function syncErrorText(code: string): string {
  return Object.hasOwn(SYNC_ERROR_TEXT, code) ? (SYNC_ERROR_TEXT[code] ?? code) : "hãng báo lỗi";
}

const HOUR = 3_600_000;

/** Số giờ hãng chưa cập nhật (từ sự kiện hãng gần nhất, không có thì từ lúc đổi trạng thái, lúc tạo), làm tròn xuống. */
export function hoursSilent(s: Shipment, now: number): number {
  const from = Date.parse(s.lastCarrierEventAt ?? s.lastStatusAt ?? s.createdAt);
  return Number.isNaN(from) ? 0 : Math.max(Math.floor((now - from) / HOUR), 0);
}

/** Trạng thái còn đổi được địa chỉ ở hãng (máy chủ: requestAddressChange). */
export const ADDRESS_CHANGE_STATUSES: readonly ShipmentStatus[] = ["awaiting_pickup", "shipping", "delivery_failed"];

/** Trạng thái đã kết thúc: không đồng bộ, không hủy, không đổi địa chỉ. */
export const isClosed = (status: ShipmentStatus) =>
  status === "delivered" || status === "returned" || status === "cancelled";

/** Trạng thái nhân viên chọn được ở "Cập nhật trạng thái" (returned chỉ qua nhận hàng hoàn). */
export const MANUAL_TARGETS = ["delivered", "shipping", "delivery_failed", "returning", "cancelled"] as const;
export type ManualTarget = (typeof MANUAL_TARGETS)[number];

const VN_OFFSET_MS = 7 * HOUR;

/** Giờ ms → "yyyy-MM-ddTHH:mm" theo giờ Việt Nam (giá trị của ô datetime-local). */
export function toVnLocal(ms: number): string {
  return new Date(ms + VN_OFFSET_MS).toISOString().slice(0, 16);
}

/** "yyyy-MM-ddTHH:mm" là giờ Việt Nam → ISO UTC; sai dạng → null. */
export function fromVnLocal(v: string): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(v);
  if (m === null) return null;
  const [, y, mo, d, h, mi] = m.map(Number);
  if (y === undefined || mo === undefined || d === undefined || h === undefined || mi === undefined) return null;
  const ms = Date.UTC(y, mo - 1, d, h, mi) - VN_OFFSET_MS;
  const back = new Date(ms + VN_OFFSET_MS);
  if (back.getUTCMonth() !== mo - 1 || back.getUTCDate() !== d || h > 23 || mi > 59) return null;
  return new Date(ms).toISOString();
}

const CANCEL_KIND_TEXT: Readonly<Record<string, string>> = {
  replace: "đổi hãng",
  order_cancel: "hủy đơn",
  create_rejected: "hãng từ chối",
  carrier_cancelled: "hãng hủy",
  manual: "cập nhật tay",
  staff_after_unknown: "hãng không có vận đơn",
};

/** "Đã hủy (đổi hãng)" của vận đơn đã hủy; lý do lạ → "Đã hủy". */
export function cancelledText(kind: string | undefined): string {
  const k = kind ?? "";
  return Object.hasOwn(CANCEL_KIND_TEXT, k) ? `Đã hủy (${CANCEL_KIND_TEXT[k] ?? k})` : "Đã hủy";
}
