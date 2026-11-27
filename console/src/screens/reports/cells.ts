// Ô số của báo cáo bán hàng bấm được (Task 239, M6-16; hành vi (a)): một ô là cột số (metric), cơ sở ngày và tùy chọn
// một dòng của bảng (chiều + khóa). Ô nằm trên địa chỉ (?o=…&d=…&k=…&b=…) nên tải lại trang hay nút quay lại của trình
// duyệt (điện thoại: ngăn là trang riêng) đều về đúng ô. Giá trị lạ trên địa chỉ bị bỏ, không gửi lên máy chủ.
import type { ReportDimension, ReportMetric } from "../../api";
import { dayLabel } from "./format";

export type Basis = "placed" | "completed";

export interface Cell {
  metric: ReportMetric;
  /** Cơ sở ngày; bỏ trống = theo ngày lên đơn (mặc định của máy chủ). */
  basis?: Basis;
  dimension?: ReportDimension;
  key?: string;
}

const METRICS: readonly ReportMetric[] = [
  "placed",
  "placed_active",
  "cancelled",
  "delivered",
  "returned",
  "customer_returned",
  "pending_confirm",
  "pending_ship",
  "cancel_requested",
];
const DIMENSIONS: readonly ReportDimension[] = [
  "status",
  "source",
  "care_by",
  "created_by",
  "channel",
  "page",
  "carrier",
  "day",
];

export const METRIC_TITLE: Readonly<Record<ReportMetric, string>> = {
  placed: "Đơn lên",
  placed_active: "Đơn chưa hủy",
  cancelled: "Đơn hủy",
  delivered: "Thành công",
  returned: "Hoàn",
  customer_returned: "Hàng khách trả",
  pending_confirm: "Chờ xác nhận",
  pending_ship: "Chờ gửi hãng",
  cancel_requested: "Đang yêu cầu hủy",
};

/** Ba ô chờ xử lý: số hiện tại, không theo kỳ. */
export const isCurrentMetric = (m: ReportMetric) =>
  m === "pending_confirm" || m === "pending_ship" || m === "cancel_requested";

const CHANNEL_NAME: Readonly<Record<string, string>> = {
  facebook: "Facebook",
  web: "Website",
  manual: "Nhập tay",
  unknown: "Không xác định",
};

/** Tên kênh hiện trên màn; kênh lạ → "Không xác định" (không tự gán vào Facebook). */
export function channelName(channel: string): string {
  return Object.hasOwn(CHANNEL_NAME, channel) ? (CHANNEL_NAME[channel] ?? "Không xác định") : "Không xác định";
}

/** Câu "theo ngày …" của ô: cho dòng phụ ở đầu ngăn chi tiết. */
export function basisText(c: Cell): string {
  if (isCurrentMetric(c.metric)) return "số hiện tại, không theo kỳ đã chọn";
  if (c.metric === "customer_returned") return "theo ngày nhận trả";
  return c.basis === "completed" ? "theo ngày hoàn tất" : "theo ngày lên đơn";
}

/** Tiêu đề đầu ngăn, không kèm số đơn: "Đơn lên" hay "Đơn lên · Facebook" / "Đơn chưa hủy · 21/11/2026". */
export function cellTitle(c: Cell): string {
  const base = METRIC_TITLE[c.metric];
  if (c.dimension === "channel" && c.key !== undefined) return `${base} · ${channelName(c.key)}`;
  if (c.dimension === "day" && c.key !== undefined) return `${base} · ${dayLabel(c.key)}`;
  return base;
}

/** Ghi ô vào địa chỉ (xóa ô cũ trước). */
export function writeCell(sp: URLSearchParams, c: Cell | null): void {
  for (const k of ["o", "b", "d", "k"]) sp.delete(k);
  if (c === null) return;
  sp.set("o", c.metric);
  if (c.basis !== undefined) sp.set("b", c.basis);
  if (c.dimension !== undefined && c.key !== undefined) {
    sp.set("d", c.dimension);
    sp.set("k", c.key);
  }
}

/** Ô đang mở theo địa chỉ; metric lạ → không có ô. */
export function parseCell(sp: URLSearchParams): Cell | null {
  const o = sp.get("o");
  const metric = METRICS.find((m) => m === o);
  if (metric === undefined) return null;
  const cell: Cell = { metric };
  const b = sp.get("b");
  if (b === "placed" || b === "completed") cell.basis = b;
  const dimension = DIMENSIONS.find((d) => d === sp.get("d"));
  const key = sp.get("k");
  if (dimension !== undefined && key !== null && key.length > 0 && key.length <= 64) {
    cell.dimension = dimension;
    cell.key = key;
  }
  return cell;
}

export const sameCell = (a: Cell | null, b: Cell | null): boolean =>
  a !== null &&
  b !== null &&
  a.metric === b.metric &&
  a.basis === b.basis &&
  a.dimension === b.dimension &&
  a.key === b.key;
