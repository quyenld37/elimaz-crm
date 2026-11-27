// Định dạng số của báo cáo (Task 239, M6-16). Một nguồn cho mọi màn báo cáo. Tiền là số nguyên đồng từ API; ở đây chỉ
// định dạng khi hiện, không cộng trừ. Tỷ lệ là phần vạn (0–10000) máy chủ đã làm tròn xuống (Q-X2): chỉ đổi dấu, không
// tính lại.

import { VN_TIME_ZONE } from "../../app/time";

const GROUP = /\B(?=(\d{3})+(?!\d))/g;

/** Số nguyên có dấu chấm hàng nghìn: 1910000 → "1.910.000". */
export function money(v: number): string {
  return String(Math.trunc(v)).replace(GROUP, ".");
}

/** Tiền kèm đơn vị: "1.910.000 ₫". */
export function vnd(v: number): string {
  return `${money(v)} ₫`;
}

/** Số đếm (đơn, cái): cùng dạng với tiền. */
export function count(v: number): string {
  return money(v);
}

/** Tỷ lệ phần vạn → "66,67%"; null (mẫu số 0) → "—". */
export function rate(bp: number | null): string {
  if (bp === null) return "—";
  const whole = Math.floor(bp / 100);
  const frac = String(bp % 100).padStart(2, "0");
  return `${String(whole)},${frac}%`;
}

/** "2026-11-18" → "18/11/2026"; chuỗi không phải ngày giữ nguyên. */
export function dayLabel(d: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(d);
  return m === null ? d : `${m[3] ?? ""}/${m[2] ?? ""}/${m[1] ?? ""}`;
}

/** Một ngày hay một khoảng: "27/11/2026" hoặc "21/11/2026 – 27/11/2026". */
export function periodText(from: string, to: string): string {
  return from === to ? dayLabel(from) : `${dayLabel(from)} – ${dayLabel(to)}`;
}

const CLOCK = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

/** Giờ trong ngày theo giờ Việt Nam, "14:32:05". */
export function clockTime(d: Date): string {
  return CLOCK.format(d);
}
