// Luật thuần của màn Mã giảm giá (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/ma-giam-gia.html). Mọi luật nghiệp vụ
// của mã (khoảng giá trị, trường khóa sau khi dùng, lượt) ở máy chủ (promotion, Task 71); ở đây chỉ đổi đơn vị ô nhập,
// dựng chữ và chặn ô sai dạng trước khi gửi.
import type { Coupon, DiscountKind } from "../../api";
import { shortDate } from "../len-don/orderRules";
import { parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";

/** Phần vạn của 100% (hợp đồng: percent → value 1–10000). */
const PERCENT_SCALE = 10_000;
const MAX_INT32 = 2_147_483_647;
const VN_OFFSET_MS = 7 * 3_600_000;

/** Ô % ("10", "12,5", "0,01") → phần vạn (1000, 1250, 1). Ngoài 0,01–100% hay quá 2 số lẻ → null. */
export function parsePercent(s: string): number | null {
  const n = percentOrZero(s);
  return n === null || n < 1 ? null : n;
}

/** Như parsePercent nhưng nhận 0 (ô cài đặt "0 đến 100%"). */
export function percentOrZero(s: string): number | null {
  const t = s.trim().replace(",", ".");
  if (!/^\d{1,3}(\.\d{1,2})?$/.test(t)) return null;
  const n = Math.round(Number(t) * 100);
  return n > PERCENT_SCALE ? null : n;
}

/** Phần vạn → chữ của ô % (1250 → "12,5"). */
export function percentInput(bp: number): string {
  return String(bp / 100).replace(".", ",");
}

/** Ô datetime-local ("2027-01-20T00:00", giờ Việt Nam) → ISO UTC; sai dạng hay trống → null. Không theo múi của máy. */
export function vnLocalToIso(s: string): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(s);
  if (m === null) return null;
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  if (y === undefined || mo === undefined || d === undefined || h === undefined || mi === undefined) return null;
  const ms = Date.UTC(y, mo - 1, d, h, mi) - VN_OFFSET_MS;
  return isoToVnLocal(new Date(ms).toISOString()) === s ? new Date(ms).toISOString() : null;
}

/** ISO → chữ của ô datetime-local theo giờ Việt Nam. */
export function isoToVnLocal(iso: string): string {
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? "" : new Date(ms + VN_OFFSET_MS).toISOString().slice(0, 16);
}

export type CouponState = "active" | "upcoming" | "expired" | "exhausted" | "disabled";

/** Trạng thái hiện của mã: máy chủ chỉ có active/disabled; hết hạn, chưa tới ngày, hết lượt suy từ hạn và lượt. */
export function couponState(c: Coupon, now: number): CouponState {
  if (c.status === "disabled") return "disabled";
  if (Date.parse(c.validTo) <= now) return "expired";
  if (Date.parse(c.validFrom) > now) return "upcoming";
  if (c.maxUses !== undefined && c.usedCount >= c.maxUses) return "exhausted";
  return "active";
}

export const STATE_CHIP: Readonly<Record<CouponState, { label: string; className: string }>> = {
  active: { label: "Đang chạy", className: "chip chip-ok" },
  upcoming: { label: "Chưa tới ngày", className: "chip" },
  exhausted: { label: "Hết lượt", className: "chip chip-err" },
  expired: { label: "Hết hạn", className: "chip" },
  disabled: { label: "Đã tắt", className: "chip" },
};

/** Dòng mờ trên danh sách: mã không còn dùng được nữa (trừ khi sửa). */
export const isMuted = (s: CouponState) => s === "expired" || s === "disabled";

/** Kênh của đơn (OrderChannel; máy chủ nhận đúng ba giá trị này cho mã). */
export const CHANNELS: readonly { key: string; label: string }[] = [
  { key: "manual", label: "Nhập tay" },
  { key: "facebook", label: "Facebook" },
  { key: "web", label: "Website" },
];
const channelLabel = (k: string) => CHANNELS.find((c) => c.key === k)?.label ?? k;

/** "10%, tối đa 150.000 ₫" hay "50.000 ₫". */
export function discountText(c: Pick<Coupon, "discountKind" | "value" | "maxDiscount">): string {
  if (c.discountKind === "fixed") return formatMoney(c.value);
  const pct = `${percentInput(c.value)}%`;
  return c.maxDiscount === undefined ? pct : `${pct}, tối đa ${formatMoney(c.maxDiscount)}`;
}

/** Điều kiện dùng: đơn tối thiểu, kênh, gắn khách, lượt mỗi khách. */
export function conditionText(c: Coupon): string {
  const parts: string[] = [];
  if (c.minSubtotal > 0) parts.push(`Đơn từ ${formatMoney(c.minSubtotal)}`);
  if (c.channels.length > 0) parts.push(`Chỉ ${c.channels.map(channelLabel).join(", ")}`);
  if (c.bound) parts.push("Dành riêng một khách");
  if (parts.length === 0) parts.push("Mọi đơn");
  if (c.perCustomerLimit !== undefined) parts.push(`${String(c.perCustomerLimit)} lượt mỗi khách`);
  return parts.join(" · ");
}

/** "01/11–30/11" theo giờ Việt Nam. */
export const periodText = (c: Coupon) => `${shortDate(c.validFrom)}–${shortDate(c.validTo)}`;

/** "87 / 200", hay "87" khi không giới hạn lượt. */
export const usesText = (c: Coupon) =>
  c.maxUses === undefined ? String(c.usedCount) : `${String(c.usedCount)} / ${String(c.maxUses)}`;

/** Ô điều khoản của mã (chữ như người dùng gõ). */
export interface TermsDraft {
  kind: DiscountKind;
  value: string;
  maxDiscount: string;
  minSubtotal: string;
  from: string;
  to: string;
  maxUses: string;
  perCustomer: string;
  channels: string[];
  releaseOnCancel: boolean;
}

export const EMPTY_TERMS: TermsDraft = {
  kind: "percent",
  value: "",
  maxDiscount: "",
  minSubtotal: "",
  from: "",
  to: "",
  maxUses: "",
  perCustomer: "",
  channels: [],
  releaseOnCancel: true,
};

/** Điều khoản đã đọc, đúng tên trường của hợp đồng (CreateCouponRequest, UpdateCouponRequest). */
export interface Terms {
  discountKind: DiscountKind;
  value: number;
  maxDiscount?: number;
  minSubtotal: number;
  validFrom: string;
  validTo: string;
  maxUses?: number;
  perCustomerLimit?: number;
  channels: string[];
  releaseOnCancel: boolean;
}

export type TermsErrors = Partial<Record<keyof TermsDraft, string>>;

export const TEXT_BAD_MONEY = "Số tiền không đúng, ví dụ 200.000";
export const TEXT_BAD_COUNT = "Nhập số nguyên từ 1";

/** Ô số lượt: số nguyên 1…int32; trống → undefined; sai → null. */
export function parseCount(s: string): number | undefined | null {
  const t = s.trim().replace(/\./g, "");
  if (t === "") return undefined;
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 && n <= MAX_INT32 ? n : null;
}

/** Kiểm ô điều khoản; đủ đúng thì terms là thân gửi (ô tùy chọn để trống không gửi), không thì null kèm lỗi theo ô. */
export function readTerms(d: TermsDraft): { terms: Terms | null; errors: TermsErrors } {
  const errors: TermsErrors = {};
  const value = d.kind === "percent" ? parsePercent(d.value) : parseMoney(d.value);
  if (value === null || value < 1) errors.value = d.kind === "percent" ? "Từ 0,01 đến 100%" : "Nhập số tiền lớn hơn 0";
  const maxDiscount = d.kind === "percent" && d.maxDiscount.trim() !== "" ? parseMoney(d.maxDiscount) : undefined;
  if (maxDiscount === null || maxDiscount === 0) errors.maxDiscount = TEXT_BAD_MONEY;
  const minSubtotal = d.minSubtotal.trim() === "" ? 0 : parseMoney(d.minSubtotal);
  if (minSubtotal === null) errors.minSubtotal = TEXT_BAD_MONEY;
  const validFrom = vnLocalToIso(d.from);
  if (validFrom === null) errors.from = "Chọn ngày giờ bắt đầu";
  const validTo = vnLocalToIso(d.to);
  if (validTo === null) errors.to = "Chọn ngày giờ kết thúc";
  else if (validFrom !== null && Date.parse(validTo) <= Date.parse(validFrom)) errors.to = "Phải sau lúc bắt đầu";
  const maxUses = parseCount(d.maxUses);
  if (maxUses === null) errors.maxUses = TEXT_BAD_COUNT;
  const perCustomer = parseCount(d.perCustomer);
  if (perCustomer === null) errors.perCustomer = TEXT_BAD_COUNT;
  if (
    Object.keys(errors).length > 0 ||
    value === null ||
    minSubtotal === null ||
    validFrom === null ||
    validTo === null ||
    maxDiscount === null ||
    maxUses === null ||
    perCustomer === null
  )
    return { terms: null, errors };
  return {
    errors,
    terms: {
      discountKind: d.kind,
      value,
      ...(maxDiscount === undefined ? {} : { maxDiscount }),
      minSubtotal,
      validFrom,
      validTo,
      ...(maxUses === undefined ? {} : { maxUses }),
      ...(perCustomer === undefined ? {} : { perCustomerLimit: perCustomer }),
      channels: d.channels,
      releaseOnCancel: d.releaseOnCancel,
    },
  };
}

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });
const moneyText = (v: number | undefined) => (v === undefined ? "" : NUM.format(v));

/** Ô điều khoản dựng từ một mã có sẵn (màn chi tiết). */
export function termsOf(c: Coupon): TermsDraft {
  return {
    kind: c.discountKind,
    value: c.discountKind === "percent" ? percentInput(c.value) : moneyText(c.value),
    maxDiscount: moneyText(c.maxDiscount),
    minSubtotal: c.minSubtotal === 0 ? "" : moneyText(c.minSubtotal),
    from: isoToVnLocal(c.validFrom),
    to: isoToVnLocal(c.validTo),
    maxUses: c.maxUses === undefined ? "" : String(c.maxUses),
    perCustomer: c.perCustomerLimit === undefined ? "" : String(c.perCustomerLimit),
    channels: [...c.channels],
    releaseOnCancel: c.releaseOnCancel,
  };
}

/** Mã do người dùng đặt (hợp đồng: ^[A-Za-z0-9-]{3,32}$). */
export const CODE_PATTERN = /^[A-Za-z0-9-]{3,32}$/;

/** Tiền tố đợt mã: chữ hoa, bỏ dấu - cuối (máy chủ tự nối "-"); hợp lệ khi ^[A-Z0-9]{0,8}$. */
export function cleanPrefix(s: string): string {
  return s.trim().toUpperCase().replace(/-+$/, "");
}
export const PREFIX_PATTERN = /^[A-Z0-9]{0,8}$/;
export const MAX_BATCH = 100_000;

const BOM = String.fromCharCode(0xfeff);

/** Tệp CSV danh sách mã: có BOM để Excel đọc đúng chữ Việt; mã chỉ gồm chữ, số, dấu - nên không cần bọc. */
export function codesCsv(codes: readonly string[]): string {
  return `${BOM}Mã\r\n${codes.map((c) => `${c}\r\n`).join("")}`;
}
