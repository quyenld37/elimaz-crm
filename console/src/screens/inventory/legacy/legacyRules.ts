// Luật thuần của màn Hàng hoàn đơn cũ (Task 273, M7-28; mockup docs/mockups/m7/hang-hoan-don-cu.html, HV10, HV11).
// Ngày luôn theo giờ Việt Nam (app/time.ts), không theo múi của máy.
import type { LegacyReturn, LegacyReturnCondition } from "../../../api";
import { VN_TIME_ZONE } from "../../../app/time";

export const REF_MIN = 3;
export const REF_MAX = 64;
export const QTY_MAX = 1000;
export const NOTE_MAX = 500;

export const TEXT_REF_LENGTH = "Mã tham chiếu từ 3 đến 64 ký tự.";
export const TEXT_QTY = "Số lượng từ 1 đến 1.000. Không còn cái nào thì bỏ dòng.";
export const TEXT_INCOMPLETE = "Thêm ít nhất một món và mã tham chiếu thì nút mở.";

/** Bỏ mọi khoảng trắng, viết hoa: so sánh trùng của máy chủ cũng chuẩn hóa như vậy. */
export function normalizeReference(s: string): string {
  return s.replace(/\s+/g, "").toUpperCase();
}

/** Câu lỗi của mã tham chiếu đã chuẩn hóa; rỗng chưa báo (người dùng chưa gõ). */
export function referenceError(ref: string): string | null {
  if (ref === "") return null;
  return ref.length < REF_MIN || ref.length > REF_MAX ? TEXT_REF_LENGTH : null;
}

export function qtyError(n: number): string | null {
  return Number.isInteger(n) && n >= 1 && n <= QTY_MAX ? null : TEXT_QTY;
}

/** Một dòng của kiện đang nhập. */
export interface FormLine {
  id: string;
  variantId: string;
  sku: string;
  name: string;
  qty: number;
  condition: LegacyReturnCondition;
}

interface ScanHit {
  variantId: string;
  sku: string;
  productName: string;
  color: string;
  size: string;
}

/** Quét một món: cộng 1 vào dòng "Bán lại được" của biến thể, chưa có thì tạo dòng mới (HV11). */
export function addScan(lines: readonly FormLine[], hit: ScanHit, newId: () => string): FormLine[] {
  const i = lines.findIndex((l) => l.variantId === hit.variantId && l.condition === "sellable");
  if (i >= 0) return lines.map((l, j) => (j === i ? { ...l, qty: Math.min(QTY_MAX, l.qty + 1) } : l));
  const name = [hit.productName, hit.color, hit.size].filter((x) => x !== "").join(" · ");
  return [...lines, { id: newId(), variantId: hit.variantId, sku: hit.sku, name, qty: 1, condition: "sellable" }];
}

/** "3 cái: 2 bán lại được, 1 hỏng". */
export function summary(lines: readonly FormLine[]): string {
  if (lines.length === 0) return "Chưa có món nào";
  const bad = lines.filter((l) => l.condition === "damaged").reduce((a, l) => a + l.qty, 0);
  const total = lines.reduce((a, l) => a + l.qty, 0);
  return `${String(total)} cái: ${String(total - bad)} bán lại được, ${String(bad)} hỏng`;
}

/** Lý do nút "Nhập kho" khóa; null khi gửi được. `ref` đã chuẩn hóa. */
export function blockedReason(ref: string, lines: readonly FormLine[]): string | null {
  if (ref === "" || lines.length === 0) return TEXT_INCOMPLETE;
  const re = referenceError(ref);
  if (re !== null) return re;
  return lines.some((l) => qtyError(l.qty) !== null) ? TEXT_QTY : null;
}

const DAY = new Intl.DateTimeFormat("en-CA", {
  timeZone: VN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

/** "2026-12-03": ngày Việt Nam của một mốc giờ. */
export function dayKey(iso: string): string {
  return DAY.format(Date.parse(iso));
}

const WEEKDAY = ["Chủ nhật", "Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy"] as const;

/** "Thứ Năm 03/12 · 2 kiện". */
export function dayTitle(day: string, count: number): string {
  const [y, m, d] = day.split("-").map(Number);
  const wd = WEEKDAY[new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1)).getUTCDay()] ?? "";
  return `${wd} ${String(d).padStart(2, "0")}/${String(m).padStart(2, "0")} · ${String(count)} kiện`;
}

/** "dd/mm/yyyy" (1–2 chữ số ngày, tháng) → "yyyy-mm-dd"; ngày không có thật → null. */
export function parseVnDate(text: string): string | null {
  const m = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/.exec(text.trim());
  if (m === null) return null;
  const [d, mo, y] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const t = new Date(Date.UTC(y, mo - 1, d));
  if (t.getUTCFullYear() !== y || t.getUTCMonth() !== mo - 1 || t.getUTCDate() !== d) return null;
  return `${String(y)}-${String(mo).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

/** "yyyy-mm-dd" → "dd/mm/yyyy". */
export function formatVnDate(day: string): string {
  const [y, m, d] = day.split("-");
  return `${d ?? ""}/${m ?? ""}/${y ?? ""}`;
}

/** Ngày Việt Nam hôm nay dịch `back` ngày. */
export function vnToday(back = 0, now = Date.now()): string {
  return dayKey(new Date(now - back * 86_400_000).toISOString());
}

export interface Counts {
  sellable: number;
  damaged: number;
  total: number;
}

/** Số cái bán lại được, hỏng của một kiện hoặc của cả danh sách. */
export function returnCounts(r: LegacyReturn | readonly LegacyReturn[]): Counts {
  const rows: readonly LegacyReturn[] = "lines" in r ? [r] : r;
  let sellable = 0;
  let damaged = 0;
  for (const row of rows)
    for (const l of row.lines) {
      if (l.condition === "damaged") damaged += l.qty;
      else sellable += l.qty;
    }
  return { sellable, damaged, total: sellable + damaged };
}

export interface DayGroup {
  day: string;
  items: LegacyReturn[];
}

/** Nhóm theo ngày Việt Nam, ngày mới trước; trong ngày giữ thứ tự máy chủ (mới nhất trước). */
export function groupByDay(items: readonly LegacyReturn[]): DayGroup[] {
  const out: DayGroup[] = [];
  for (const it of items) {
    const day = dayKey(it.createdAt);
    const g = out.find((x) => x.day === day);
    if (g === undefined) out.push({ day, items: [it] });
    else g.items.push(it);
  }
  return out.sort((a, b) => (a.day < b.day ? 1 : a.day > b.day ? -1 : 0));
}
