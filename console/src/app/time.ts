// Giờ hiện trên màn hình (gom từ màn Sự kiện lỗi, Task 330, khi màn Kiểm kho Task 329 cần cùng cách viết). Giờ theo
// múi Việt Nam cố định: shop ở Việt Nam; không phụ thuộc múi của máy đang mở.

/** Múi giờ hiện trên màn hình. */
export const VN_TIME_ZONE = "Asia/Ho_Chi_Minh";

const SHORT = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  hourCycle: "h23",
});

const FULL = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hourCycle: "h23",
});

/** Các phần (giờ, phút, ngày, tháng, năm) của iso theo fmt; chuỗi không phải giờ → null. */
function parts(fmt: Intl.DateTimeFormat, iso: string): Record<string, string> | null {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return null;
  return Object.fromEntries(fmt.formatToParts(ms).map((x) => [x.type, x.value]));
}

/** "06:12 18/11" (giờ:phút ngày/tháng, giờ Việt Nam). Chuỗi không phải giờ → "". */
export function shortTime(iso: string): string {
  const p = parts(SHORT, iso);
  if (p === null) return "";
  return `${p.hour ?? ""}:${p.minute ?? ""} ${p.day ?? ""}/${p.month ?? ""}`;
}

/** "06:12 · 18/11/2026" (có năm, giờ Việt Nam; màn Nhận đơn web, Task 462). Chuỗi không phải giờ → "". */
export function fullTime(iso: string): string {
  const p = parts(FULL, iso);
  if (p === null) return "";
  return `${p.hour ?? ""}:${p.minute ?? ""} · ${p.day ?? ""}/${p.month ?? ""}/${p.year ?? ""}`;
}
