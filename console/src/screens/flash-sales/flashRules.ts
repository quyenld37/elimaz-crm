// Luật hiển thị và kiểm form của màn Đợt flash sale (kế hoạch M5 Task 206; mockup docs/mockups/m5/flash-sales.html; HV26–HV31).
// Trạng thái đợt (draft, scheduled, open, closed) do máy chủ giữ; ở đây chỉ dựng chữ, chọn thao tác được phép, kiểm form
// và dựng thân gửi. Máy chủ kiểm lại mọi luật (giá thấp hơn giá thường, suất, giờ, tồn).
import type {
  FlashItem,
  FlashItemInput,
  FlashLive,
  FlashLiveItem,
  FlashSaleInput,
  FlashSaleStatus,
  FlashSaleSummary,
  FlashShortage,
} from "../../api";
import { ApiError } from "../../app/errors";
import { shortTime } from "../../app/time";
import { isoToVnLocal, vnLocalToIso } from "../ma-giam-gia/couponRules";
import { formatMoney } from "../products/productRules";

export const NAME_MAX = 120;
export const SLOT_MAX = 20_000;
export const LIMIT_MAX = 10;
export const HOLD_MIN = 5;
export const HOLD_MAX = 30;
export const MAX_DAYS = 7;
export const DEFAULT_LIMIT = 1;
export const DEFAULT_HOLD = 10;
/** Làm mới số trực tiếp (ms): đợt đang mở 5 s (lượt mua không phát sự kiện), còn lại 20 s (Task 206). */
export const LIVE_OPEN_MS = 5000;
export const IDLE_MS = 20_000;

const MS_MINUTE = 60_000;
const MS_HOUR = 60 * MS_MINUTE;
const MS_DAY = 24 * MS_HOUR;

export const STATUS_FILTERS: readonly (FlashSaleStatus | "")[] = ["", "open", "scheduled", "draft", "closed"];

const CHIPS: Readonly<Record<FlashSaleStatus, { label: string; className: string }>> = {
  draft: { label: "Nháp", className: "chip" },
  scheduled: { label: "Đã lên lịch", className: "chip chip-line" },
  open: { label: "Đang mở", className: "chip chip-dark" },
  closed: { label: "Đã đóng", className: "chip" },
};

export const statusChip = (s: FlashSaleStatus) => CHIPS[s];
export const filterText = (s: FlashSaleStatus | ""): string => (s === "" ? "Tất cả" : CHIPS[s].label);

export const runeCount = (s: string): number => Array.from(s).length;

/** "còn 1 giờ 28 phút", "còn 15 phút", "còn dưới 1 phút", "đã quá giờ đóng". */
export function remainingText(endsAt: string, now: number): string {
  const left = Date.parse(endsAt) - now;
  if (!(left > 0)) return "đã quá giờ đóng";
  const hours = Math.floor(left / MS_HOUR);
  const minutes = Math.floor((left % MS_HOUR) / MS_MINUTE);
  if (hours > 0) return `còn ${String(hours)} giờ ${String(minutes)} phút`;
  return minutes > 0 ? `còn ${String(minutes)} phút` : "còn dưới 1 phút";
}

/** Dòng phụ dưới tên đợt trong danh sách (mockup A). */
export function saleSub(s: FlashSaleSummary, now: number): string {
  const n = `${String(s.itemCount)} món`;
  switch (s.status) {
    case "draft":
      return `${n} · chưa giữ suất nào`;
    case "scheduled":
      return `${n} · tự mở lúc ${shortTime(s.startsAt)}`;
    case "open":
      return `${n} · đang mở, ${remainingText(s.endsAt, now)}`;
    case "closed":
      return `${n} · ${s.closedReason === "manual" ? "đóng sớm" : "hết giờ"}`;
  }
}

/** Tự làm mới (ms) theo trạng thái: đang mở 5 s, còn lại 20 s. */
export const refreshMs = (status: FlashSaleStatus | undefined): number => (status === "open" ? LIVE_OPEN_MS : IDLE_MS);

/** "00:00 12/12/2026" (giờ VN, có năm). Chuỗi không phải giờ → "". */
export function longTime(iso: string): string {
  const l = isoToVnLocal(iso);
  return l === "" ? "" : `${l.slice(11)} ${l.slice(8, 10)}/${l.slice(5, 7)}/${l.slice(0, 4)}`;
}

export const periodText = (s: Pick<FlashSaleSummary, "startsAt" | "endsAt">): string =>
  `${shortTime(s.startsAt)} → ${shortTime(s.endsAt)}`;

export interface SaleActions {
  /** Sửa tên, giờ; thêm, sửa, bỏ món (chỉ nháp, HV29). */
  edit: boolean;
  schedule: boolean;
  addSlots: boolean;
  close: boolean;
}

/** Thao tác ghi được phép theo trạng thái và quyền `promotion.flash.manage`. Sau khi lên lịch chỉ Thêm suất, Đóng sớm. */
export function actionsOf(status: FlashSaleStatus, canManage: boolean): SaleActions {
  const live = status === "scheduled" || status === "open";
  return {
    edit: canManage && status === "draft",
    schedule: canManage && status === "draft",
    addSlots: canManage && live,
    close: canManage && live,
  };
}

// ---- form đợt ----

export interface SaleDraft {
  name: string;
  start: string;
  end: string;
}

export type SaleErrors = Partial<Record<"name" | "start" | "end", string>>;

export const emptySaleDraft = (): SaleDraft => ({ name: "", start: "", end: "" });
export const saleDraftOf = (s: FlashSaleSummary): SaleDraft => ({
  name: s.name,
  start: isoToVnLocal(s.startsAt),
  end: isoToVnLocal(s.endsAt),
});

function lengthText(ms: number): string {
  const days = Math.floor(ms / MS_DAY);
  const hours = Math.floor((ms % MS_DAY) / MS_HOUR);
  return hours === 0 ? `${String(days)} ngày` : `${String(days)} ngày ${String(hours)} giờ`;
}

export function validateSale(d: SaleDraft): SaleErrors {
  const e: SaleErrors = {};
  const n = runeCount(d.name.trim());
  if (n === 0) e.name = "Nhập tên đợt";
  else if (n > NAME_MAX) e.name = "Tên tối đa 120 ký tự";
  const start = vnLocalToIso(d.start);
  const end = vnLocalToIso(d.end);
  if (start === null) e.start = "Chọn giờ mở";
  if (end === null) e.end = "Chọn giờ đóng";
  else if (start !== null) {
    const span = Date.parse(end) - Date.parse(start);
    if (span <= 0) e.end = "Giờ đóng phải sau giờ mở.";
    else if (span > MAX_DAYS * MS_DAY) e.end = `Đợt dài tối đa 7 ngày (đang là ${lengthText(span)}).`;
  }
  return e;
}

/** Thân FlashSaleInput của form hợp lệ; form sai → null. */
export function toSaleInput(d: SaleDraft): FlashSaleInput | null {
  const startsAt = vnLocalToIso(d.start);
  const endsAt = vnLocalToIso(d.end);
  if (Object.keys(validateSale(d)).length > 0 || startsAt === null || endsAt === null) return null;
  return { name: d.name.trim(), startsAt, endsAt };
}

// ---- form món ----

export interface ItemDraft {
  price: string;
  qty: string;
  limit: string;
  hold: string;
}

export type ItemErrors = Partial<Record<"price" | "qty" | "limit" | "hold" | "variant", string>>;

export const emptyItemDraft = (): ItemDraft => ({ price: "", qty: "", limit: "", hold: "" });
export const itemDraftOf = (i: FlashItem): ItemDraft => ({
  price: String(i.salePrice),
  qty: String(i.quantity),
  limit: String(i.perCustomerLimit),
  hold: String(i.holdMinutes),
});

/** "199.000" → 199000 (số nguyên đồng; dấu chấm là ngăn nghìn); sai dạng → null. */
export function parseMoney(s: string): number | null {
  const t = s.trim().replaceAll(".", "");
  return /^\d{1,12}$/.test(t) ? Number(t) : null;
}

const INT = /^\d{1,6}$/;

function intIn(s: string, min: number, max: number): number | null {
  const n = INT.test(s.trim()) ? Number(s.trim()) : Number.NaN;
  return n >= min && n <= max ? n : null;
}

export function validateItem(d: ItemDraft, listPrice: number): ItemErrors {
  const e: ItemErrors = {};
  if (d.price.trim() === "") e.price = "Nhập giá flash";
  else {
    const p = parseMoney(d.price);
    if (p === null) e.price = "Giá flash là số nguyên đồng";
    else if (p <= 0) e.price = "Giá flash phải lớn hơn 0";
    else if (p >= listPrice) e.price = `Giá flash phải thấp hơn giá bán thường ${formatMoney(listPrice)}.`;
  }
  if (intIn(d.qty, 1, SLOT_MAX) === null) e.qty = "Số suất từ 1 đến 20.000";
  if (d.limit.trim() !== "" && intIn(d.limit, 1, LIMIT_MAX) === null) e.limit = "Mỗi khách từ 1 đến 10";
  if (d.hold.trim() !== "" && intIn(d.hold, HOLD_MIN, HOLD_MAX) === null) e.hold = "Hạn giữ từ 5 đến 30 phút";
  return e;
}

/** Trường của form món đã hợp lệ (mặc định mỗi khách 1, giữ 10 phút); form sai → null. */
function itemFields(d: ItemDraft, listPrice: number) {
  const salePrice = parseMoney(d.price);
  const quantity = intIn(d.qty, 1, SLOT_MAX);
  if (Object.keys(validateItem(d, listPrice)).length > 0 || salePrice === null || quantity === null) return null;
  return {
    salePrice,
    quantity,
    perCustomerLimit: d.limit.trim() === "" ? DEFAULT_LIMIT : Number(d.limit.trim()),
    holdMinutes: d.hold.trim() === "" ? DEFAULT_HOLD : Number(d.hold.trim()),
  };
}

export function toItemInput(d: ItemDraft, listPrice: number, variantId: string): FlashItemInput | null {
  const f = itemFields(d, listPrice);
  return f === null ? null : { variantId, ...f };
}

/** Chỉ trường người dùng đổi (luật console: PATCH chỉ gửi trường đổi); không đổi gì → null. */
export function toItemChanges(
  d: ItemDraft,
  item: FlashItem,
): { salePrice?: number; quantity?: number; perCustomerLimit?: number; holdMinutes?: number } | null {
  const f = itemFields(d, item.listPrice);
  if (f === null) return null;
  const out: { salePrice?: number; quantity?: number; perCustomerLimit?: number; holdMinutes?: number } = {};
  if (f.salePrice !== item.salePrice) out.salePrice = f.salePrice;
  if (f.quantity !== item.quantity) out.quantity = f.quantity;
  if (f.perCustomerLimit !== item.perCustomerLimit) out.perCustomerLimit = f.perCustomerLimit;
  if (f.holdMinutes !== item.holdMinutes) out.holdMinutes = f.holdMinutes;
  return Object.keys(out).length === 0 ? null : out;
}

// ---- lên lịch, thiếu hàng ----

export interface ScheduleSummary {
  total: number;
  count: number;
  top: { sku: string; name: string; quantity: number; sellable: number }[];
  /** Các món sau ba món đầu gộp một dòng; null khi không có. */
  rest: { count: number; quantity: number } | null;
}

const TOP_LINES = 3;

const fmt = (n: number) => new Intl.NumberFormat("vi-VN").format(n);

/** Cột "Đã bán / suất" của danh sách (mockup A): đợt nháp chưa giữ suất nên "— / suất". */
export function soldText(s: FlashSaleSummary): string {
  return `${s.status === "draft" ? "—" : fmt(s.sold)} / ${fmt(s.quantity)}`;
}

/** Có thể bán thường của mã hàng trước → sau khi giữ thêm hold suất (mockup D "820 → 520", F "118 → 68"); không đủ
 * hàng → short (Lên lịch, Thêm suất sẽ bị từ chối). */
export function stockAfter(sellable: number, hold: number): { text: string; short: boolean } {
  if (sellable < hold) {
    return { text: `bán thường chỉ còn ${fmt(sellable)}, thiếu ${fmt(hold - sellable)}`, short: true };
  }
  return { text: `bán thường còn ${fmt(sellable)} → ${fmt(sellable - hold)}`, short: false };
}

export function scheduleSummary(items: readonly FlashItem[]): ScheduleSummary {
  const total = items.reduce((n, i) => n + i.quantity, 0);
  const rest = items.slice(TOP_LINES);
  return {
    total,
    count: items.length,
    top: items
      .slice(0, TOP_LINES)
      .map((i) => ({ sku: i.sku, name: i.name, quantity: i.quantity, sellable: i.sellable })),
    rest: rest.length === 0 ? null : { count: rest.length, quantity: rest.reduce((n, i) => n + i.quantity, 0) },
  };
}

export interface Shortage extends FlashShortage {
  missing: number;
}

function isShortage(v: unknown): v is FlashShortage {
  if (typeof v !== "object" || v === null) return false;
  const o = new Map(Object.entries(v));
  return (
    typeof o.get("variantId") === "string" &&
    typeof o.get("sku") === "string" &&
    typeof o.get("need") === "number" &&
    typeof o.get("sellable") === "number"
  );
}

/** Mọi mã hàng thiếu của lỗi insufficient_stock (lên lịch, thêm suất); lỗi khác hay thân lạ → rỗng. */
export function shortagesOf(err: unknown): Shortage[] {
  if (!(err instanceof ApiError) || err.code !== "insufficient_stock") return [];
  const raw = err.details.shortages;
  if (!Array.isArray(raw) || !raw.every(isShortage)) return [];
  return raw.map((s) => ({ ...s, missing: Math.max(s.need - s.sellable, 0) }));
}

// ---- số trực tiếp ----

export interface LiveTotals {
  quantity: number;
  available: number;
  held: number;
  sold: number;
  released: number;
  buyers: number;
}

export function liveTotals(live: FlashLive): LiveTotals {
  const t: LiveTotals = { quantity: 0, available: 0, held: 0, sold: 0, released: 0, buyers: 0 };
  for (const i of live.items) {
    t.quantity += i.quantity;
    t.available += i.available;
    t.held += i.held;
    t.sold += i.sold;
    t.released += i.released;
    t.buyers += i.buyers;
  }
  return t;
}

/** Độ dài hai đoạn đầu của thanh (đã bán, đang giữ) theo % số suất, làm tròn một số lẻ. */
export function barPercents(i: FlashLiveItem): { sold: number; held: number } {
  if (i.quantity <= 0) return { sold: 0, held: 0 };
  const pct = (n: number) => Math.round((n / i.quantity) * 1000) / 10;
  return { sold: pct(i.sold), held: pct(i.held) };
}
