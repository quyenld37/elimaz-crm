// Luật chọn kỳ của báo cáo (Task 239, M6-16; mockup tong-quan.html trạng thái A–C). Ngày là chuỗi "YYYY-MM-DD" theo
// giờ Việt Nam (API tính trọn ngày VN, Task 225); khoảng tối đa 366 ngày tính cả hai đầu, không chọn ngày tương lai.
// Kỳ nằm trên địa chỉ (?ky=7d hay ?ky=custom&tu=…&den=…) để nút quay lại của trình duyệt về đúng báo cáo.
import { dayLabel } from "./format";
import { VN_TIME_ZONE } from "../../app/time";

export type PeriodPreset = "today" | "yesterday" | "7d" | "month" | "custom";

export interface Period {
  preset: PeriodPreset;
  from: string;
  to: string;
}

export const MAX_DAYS = 366;
const DAY_MS = 86_400_000;
const PRESETS: readonly PeriodPreset[] = ["today", "yesterday", "7d", "month", "custom"];
const ISO = /^(\d{4})-(\d{2})-(\d{2})$/;

const VN_DAY = new Intl.DateTimeFormat("en-CA", {
  timeZone: VN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

/** Ngày hôm nay theo giờ Việt Nam (không theo UTC, không theo múi của máy). */
export function vnToday(now: Date): string {
  const p = Object.fromEntries(VN_DAY.formatToParts(now).map((x) => [x.type, x.value]));
  return `${p.year ?? ""}-${p.month ?? ""}-${p.day ?? ""}`;
}

function utc(iso: string): number | null {
  const m = ISO.exec(iso);
  if (m === null) return null;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const ms = Date.UTC(y, mo - 1, d);
  const back = new Date(ms);
  // Ngày không có thật (30/02, tháng 13) bị Date đẩy sang ngày khác.
  return back.getUTCFullYear() === y && back.getUTCMonth() === mo - 1 && back.getUTCDate() === d ? ms : null;
}

const isoOf = (ms: number) => new Date(ms).toISOString().slice(0, 10);

/** Ngày hợp lệ dạng YYYY-MM-DD (có thật trên lịch). */
export function isDay(s: string): boolean {
  return utc(s) !== null;
}

/** Cộng n ngày (âm là lùi) vào một ngày hợp lệ. */
export function addDays(iso: string, n: number): string {
  const ms = utc(iso);
  return ms === null ? iso : isoOf(ms + n * DAY_MS);
}

/** Số ngày từ from đến to, tính cả hai đầu (cùng ngày = 1). Ngày không hợp lệ → 0. */
export function daysBetween(from: string, to: string): number {
  const a = utc(from);
  const b = utc(to);
  return a === null || b === null ? 0 : Math.round((b - a) / DAY_MS) + 1;
}

/** Kỳ mẫu tính từ ngày hôm nay (today). "custom" ở đây là chính hôm nay (người dùng sửa sau). */
export function presetPeriod(preset: PeriodPreset, today: string): Period {
  switch (preset) {
    case "yesterday": {
      const y = addDays(today, -1);
      return { preset, from: y, to: y };
    }
    case "7d":
      return { preset, from: addDays(today, -6), to: today };
    case "month":
      return { preset, from: `${today.slice(0, 7)}-01`, to: today };
    default:
      return { preset, from: today, to: today };
  }
}

/** Lý do kỳ không xem được (null = xem được). Thứ tự: thiếu → đảo ngược → quá dài → tương lai. */
export function periodError(p: { from: string; to: string }, today: string): string | null {
  if (!isDay(p.from) || !isDay(p.to)) return "Chọn đủ ngày đầu và ngày cuối.";
  if (p.from > p.to) return "Ngày đầu phải trước hoặc bằng ngày cuối.";
  const n = daysBetween(p.from, p.to);
  if (n > MAX_DAYS)
    return `Khoảng tối đa ${String(MAX_DAYS)} ngày; đang chọn ${String(n)} ngày. Thu hẹp khoảng ngày để xem.`;
  if (p.to > today) return `Không chọn được ngày sau hôm nay (${dayLabel(today)}).`;
  return null;
}

const isPreset = (v: string | null): v is PeriodPreset => PRESETS.some((p) => p === v);

/** Kỳ trên địa chỉ; thiếu hay sai dạng → Hôm nay. Kỳ mẫu luôn tính lại theo hôm nay. */
export function parsePeriod(sp: URLSearchParams, today: string): Period {
  const ky = sp.get("ky");
  if (!isPreset(ky)) return presetPeriod("today", today);
  if (ky !== "custom") return presetPeriod(ky, today);
  const from = sp.get("tu") ?? "";
  const to = sp.get("den") ?? "";
  return isDay(from) && isDay(to) ? { preset: "custom", from, to } : presetPeriod("today", today);
}

/** Ghi kỳ vào địa chỉ (xóa các tham số kỳ cũ trước). Hôm nay là mặc định: ghi "ky=today" để rõ ràng. */
export function writePeriod(sp: URLSearchParams, p: Period): void {
  sp.delete("ky");
  sp.delete("tu");
  sp.delete("den");
  sp.set("ky", p.preset);
  if (p.preset === "custom") {
    sp.set("tu", p.from);
    sp.set("den", p.to);
  }
}

const WEEKDAY = ["CN", "T2", "T3", "T4", "T5", "T6", "T7"] as const;

/** Thứ của một ngày: "T6", "CN". Ngày không hợp lệ → "". */
export function weekday(iso: string): string {
  const ms = utc(iso);
  return ms === null ? "" : (WEEKDAY[new Date(ms).getUTCDay()] ?? "");
}
