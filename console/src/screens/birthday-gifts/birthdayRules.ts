// Luật hiển thị của màn Quà sinh nhật (kế hoạch M5 Task 209 / M5-26, mockup docs/mockups/m5/birthday-gifts.html): chữ
// và màu tám trạng thái (Task 468), che SĐT, liên kết Zalo, tin chép (đúng giftMessage của website cũ,
// apps/storefront-birthday/src/server/birthday/rules.ts), ngày theo giờ Việt Nam, form cài đặt theo giới hạn API.
import type { BirthdayCustomerState, BirthdaySettings, BirthdaySettingsFields, Coupon } from "../../api";
import { VN_TIME_ZONE } from "../../app/time";
import { moneyInput, parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { WEBSITE_ORIGIN } from "../web-campaigns/campaignRules";

/** Tám trạng thái theo thứ tự ưu tiên của máy chủ (promotion.BirthdayStates). */
export const STATES: readonly BirthdayCustomerState[] = [
  "manual",
  "due",
  "unconfirmed",
  "given",
  "waiting",
  "used",
  "expired",
  "stopped",
];

export const STATE_TEXT: Readonly<Record<BirthdayCustomerState, string>> = {
  manual: "Cần nhắn",
  due: "Đến hạn, chờ tạo mã",
  unconfirmed: "Chờ đơn giao thành công",
  given: "Đã gửi mã",
  waiting: "Chờ ngày gửi",
  used: "Đã dùng",
  expired: "Hết hạn, chưa dùng",
  stopped: "Đã hủy nhận",
};

const STATE_CLASS: Readonly<Record<BirthdayCustomerState, string>> = {
  manual: "chip chip-err",
  due: "chip chip-err",
  unconfirmed: "chip",
  given: "chip chip-ok",
  waiting: "chip",
  used: "chip chip-ok",
  expired: "chip",
  stopped: "chip chip-dark",
};

/** Nhãn trạng thái; quà đang tắt thì "Đến hạn" ghi rõ là sẽ không được tạo mã (việc phát hiện của Task 468). */
export function stateChip(s: BirthdayCustomerState, enabled: boolean): { label: string; className: string } {
  if (s === "due" && !enabled) return { label: "Đến hạn · quà đang tắt", className: "chip" };
  return { label: STATE_TEXT[s], className: STATE_CLASS[s] };
}

/** Số trong nước (0…) của SĐT đã lưu dạng +84…; dạng khác giữ nguyên. */
export const domesticPhone = (p: string): string => (p.startsWith("+84") ? `0${p.slice(3)}` : p);

/** "0912 *** 678": bảng chỉ hiện số che giữa. */
export function maskPhone(p: string): string {
  const d = domesticPhone(p);
  return `${d.slice(0, 4)} *** ${d.slice(-3)}`;
}

/** Mở cuộc trò chuyện Zalo với số đầy đủ của khách. */
export const zaloLink = (p: string): string => `https://zalo.me/${domesticPhone(p)}`;

export function birthdayInText(n: number): string {
  if (n === 0) return "hôm nay";
  return n > 0 ? `còn ${String(n)} ngày` : `đã qua ${String(-n)} ngày`;
}

const PARTS = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});

function vnParts(ms: number): { d: string; m: string; y: string } {
  const p = Object.fromEntries(PARTS.formatToParts(ms).map((x) => [x.type, x.value]));
  return { d: p.day ?? "", m: p.month ?? "", y: p.year ?? "" };
}

/** "24/11" (giờ VN). */
export function dayMonth(iso: string): string {
  const { d, m } = vnParts(Date.parse(iso));
  return `${d}/${m}`;
}

/** "24/11/2026" (giờ VN). */
export function fullDate(iso: string): string {
  const { d, m, y } = vnParts(Date.parse(iso));
  return `${d}/${m}/${y}`;
}

/** Cùng ngày theo giờ VN. */
export const sameVnDay = (iso: string, now: number): boolean => fullDate(iso) === fullDate(new Date(now).toISOString());

/** Ngày dùng cuối của mã: validTo là 0:00 giờ VN của ngày sau ngày cuối. */
export const lastDay = (validTo: string): string => fullDate(new Date(Date.parse(validTo) - 1).toISOString());

/** "1/12": ngày/tháng không số 0 đầu, như tin của website cũ. */
function shortDay(ms: number): string {
  const { d, m } = vnParts(ms);
  return `${String(Number(d))}/${String(Number(m))}`;
}

/** Tên gọi: chữ cuối của họ tên. */
export const callName = (name: string): string => name.trim().split(/\s+/).pop() ?? "";

/** Tin gửi khách (cùng một câu cho Zalo, email và nhân viên nhắn tay) — đúng giftMessage của website cũ. */
export function giftMessage(g: {
  fullName: string;
  phone: string;
  code: string;
  amount: number;
  minOrder: number;
  validFrom: string;
  validTo: string;
}): string {
  const who = callName(g.fullName);
  const min = g.minOrder > 0 ? ` cho đơn từ ${formatMoney(g.minOrder)}` : "";
  const link = `${WEBSITE_ORIGIN}/?qua=${encodeURIComponent(g.code)}`;
  return [
    `Mừng sinh nhật${who === "" ? "" : ` ${who}`}! ELIMAZ tặng bạn mã ${g.code} giảm ${formatMoney(g.amount)}${min}.`,
    `Dùng từ ${shortDay(Date.parse(g.validFrom))} đến ${shortDay(Date.parse(g.validTo) - 1)}, cho đơn giao tới số ${maskPhone(g.phone)}.`,
    `Mua sắm với quà sinh nhật: ${link}`,
  ].join("\n");
}

/** Câu khách thấy trên trang cảm ơn của website (mockup trạng thái C). */
export function thanksText(s: Pick<BirthdaySettingsFields, "amount" | "daysBefore" | "validDays">): string {
  const when = s.daysBefore === 0 ? "Đúng ngày sinh nhật" : `Trước sinh nhật ${String(s.daysBefore)} ngày`;
  return `${when}, bạn nhận mã giảm ${formatMoney(s.amount)}, dùng trong ${String(s.validDays)} ngày.`;
}

/** Trạng thái một mã trong đợt (Đợt mã): đã dùng, hết hạn chưa dùng, đã tặng (ngày), chưa tặng. */
export function codeState(
  c: Pick<Coupon, "usedCount" | "validTo" | "givenAt">,
  now: number,
): { label: string; className: string; muted?: boolean } {
  if (c.usedCount > 0) return { label: "Đã dùng", className: "chip chip-ok" };
  if (Date.parse(c.validTo) <= now) return { label: "Hết hạn, chưa dùng", className: "chip", muted: true };
  if (c.givenAt !== undefined) return { label: `Đã tặng ${dayMonth(c.givenAt)}`, className: "chip" };
  return { label: "Chưa tặng", className: "chip chip-err" };
}

/** Bản nháp form cài đặt (chữ của các ô). */
export interface SettingsDraft {
  enabled: boolean;
  amount: string;
  minOrder: string;
  daysBefore: string;
  validDays: string;
  minLeadDays: string;
}

export type SettingsErrors = Partial<Record<Exclude<keyof SettingsDraft, "enabled">, string>>;

export function settingsDraft(s: BirthdaySettings): SettingsDraft {
  return {
    enabled: s.enabled,
    amount: moneyInput(s.amount),
    minOrder: moneyInput(s.minOrder),
    daysBefore: String(s.daysBefore),
    validDays: String(s.validDays),
    minLeadDays: String(s.minLeadDays),
  };
}

function intIn(s: string, lo: number, hi: number): number | null {
  const t = s.trim();
  if (!/^\d{1,4}$/.test(t)) return null;
  const n = Number(t);
  return n >= lo && n <= hi ? n : null;
}

/** Kiểm form theo giới hạn của saveBirthdaySettings (máy chủ vẫn kiểm, 422 invalid_gift_settings). */
export function readSettings(d: SettingsDraft): { fields: BirthdaySettingsFields | null; errors: SettingsErrors } {
  const errors: SettingsErrors = {};
  const amount = parseMoney(d.amount);
  if (amount === null || amount < 10_000 || amount > 1_000_000 || amount % 1000 !== 0)
    errors.amount = "Mức giảm từ 10.000 tới 1.000.000 ₫, bội số của 1.000.";
  const minOrder = d.minOrder.trim() === "" ? 0 : parseMoney(d.minOrder);
  if (minOrder === null || minOrder > 100_000_000) errors.minOrder = "Đơn tối thiểu từ 0 tới 100.000.000 ₫.";
  const daysBefore = intIn(d.daysBefore, 0, 30);
  if (daysBefore === null) errors.daysBefore = "Nhập số nguyên 0–30.";
  const validDays = intIn(d.validDays, 1, 60);
  if (validDays === null) errors.validDays = "Nhập số nguyên 1–60.";
  const minLeadDays = intIn(d.minLeadDays, 0, 120);
  if (minLeadDays === null) errors.minLeadDays = "Nhập số nguyên 0–120.";
  if (
    amount === null ||
    minOrder === null ||
    daysBefore === null ||
    validDays === null ||
    minLeadDays === null ||
    Object.keys(errors).length > 0
  )
    return { fields: null, errors };
  return { fields: { enabled: d.enabled, amount, minOrder, daysBefore, validDays, minLeadDays }, errors };
}
