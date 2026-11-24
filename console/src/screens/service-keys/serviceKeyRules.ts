// Luật thuần của màn Khóa dịch vụ (kế hoạch M5 Task 208, M5-25; mockup docs/mockups/m5/service-keys.html; ADR-18,
// Task 189). Máy chủ vẫn kiểm: quyền ngoài web.* → 422 permission_not_allowed_for_key, CIDR sai → 400 invalid_input.
import type { ServiceKey } from "../../api";

export interface KeyPermission {
  /** Khóa quyền (identity.ServiceKeyPermissions, docs/quyen.md). */
  key: string;
  label: string;
  hint?: string;
  /** Tích sẵn khi tạo khóa (mockup B: mọi quyền trừ Mua flash sale của đợt 2). */
  byDefault: boolean;
}

/** Chín quyền web.* khóa giữ được, đúng thứ tự của identity.ServiceKeyPermissions. */
export const KEY_PERMISSIONS: readonly KeyPermission[] = [
  {
    key: "web.catalog.read",
    label: "Đọc danh mục",
    hint: "Sản phẩm, giá, còn hàng, chiến dịch, banner, bộ phối, trạng thái nhận đơn",
    byDefault: true,
  },
  { key: "web.orders.create", label: "Tạo đơn web", byDefault: true },
  {
    key: "web.orders.read",
    label: "Tra đơn web",
    hint: "Trang đơn hàng của khách, yêu cầu và tin khách thấy",
    byDefault: true,
  },
  { key: "web.orders.cancel", label: "Khách hủy đơn web", byDefault: true },
  {
    key: "web.requests.write",
    label: "Gửi yêu cầu về đơn hàng, góp ý",
    hint: "Kèm ảnh khách tải lên",
    byDefault: true,
  },
  {
    key: "web.coupons.issue",
    label: "Mã giảm giá và quà sinh nhật",
    hint: "Phát mã của website, đọc quà sinh nhật cần gửi",
    byDefault: true,
  },
  { key: "web.birthdays.write", label: "Đăng ký sinh nhật", byDefault: true },
  { key: "web.shipping.quote", label: "Báo phí giao hàng", byDefault: true },
  { key: "web.flash.buy", label: "Mua flash sale", hint: "Chỉ dùng khi bật flash sale (đợt 2)", byDefault: false },
];

/** Từ số quyền này trở lên thì chip ghi "n quyền website" thay vì từng tên (bảng gọn trên điện thoại). */
const CHIP_COUNT_FROM = 4;

/** Chip cột Quyền: ít quyền → tên từng quyền; nhiều → số quyền; quyền màn chưa biết → mã nguyên. */
export function permChips(perms: readonly string[]): string[] {
  if (perms.length >= CHIP_COUNT_FROM) return [`${String(perms.length)} quyền website`];
  return perms.map((p) => KEY_PERMISSIONS.find((k) => k.key === p)?.label ?? p);
}

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/;
const IPV6 = /^[0-9a-fA-F:.]*:[0-9a-fA-F:.]*\/(\d{1,3})$/;

function isCidr(s: string): boolean {
  const v4 = IPV4.exec(s);
  if (v4 !== null) {
    const nums = v4.slice(1).map(Number);
    const bits = nums.pop() ?? 99;
    return nums.every((n) => n <= 255) && bits <= 32;
  }
  const v6 = IPV6.exec(s);
  return v6 !== null && Number(v6[1]) <= 128;
}

/** Đọc ô "IP được gọi": mỗi dòng một dải (dòng trống bỏ qua); rỗng = mọi địa chỉ. */
export function parseCidrs(text: string): { cidrs: string[] } | { error: string } {
  const lines = text.split("\n").map((l) => l.trim());
  const cidrs: string[] = [];
  for (const [i, line] of lines.entries()) {
    if (line === "") continue;
    if (!isCidr(line)) return { error: `Dòng ${String(i + 1)} chưa đúng dạng địa chỉ/dải, ví dụ 13.140.64.56/32` };
    cidrs.push(line);
  }
  return { cidrs };
}

export interface CreateForm {
  name: string;
  perms: readonly string[];
  ips: string;
}

export interface CreateErrors {
  name?: string;
  perms?: string;
  ips?: string;
}

export const MAX_NAME = 80;

export function validateCreate(f: CreateForm): CreateErrors {
  const e: CreateErrors = {};
  const n = f.name.trim().length;
  if (n < 1 || n > MAX_NAME) e.name = "Tên 1–80 ký tự";
  if (f.perms.length === 0) e.perms = "Chọn ít nhất một quyền";
  const ips = parseCidrs(f.ips);
  if ("error" in ips) e.ips = ips.error;
  return e;
}

const DATE = new Intl.DateTimeFormat("en-GB", {
  timeZone: "Asia/Ho_Chi_Minh",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});
const DAY = new Intl.DateTimeFormat("en-GB", { timeZone: "Asia/Ho_Chi_Minh", day: "2-digit", month: "2-digit" });

/** "14/11/2026" theo giờ Việt Nam. */
export const fullDate = (iso: string) => DATE.format(Date.parse(iso));

/** Chip trạng thái: đang dùng, hay đã thu hồi kèm ngày. */
export function keyStatus(k: Pick<ServiceKey, "revokedAt">): { text: string; cls: string } {
  if (k.revokedAt === undefined) return { text: "Đang dùng", cls: "chip chip-ok" };
  return { text: `Đã thu hồi ${DAY.format(Date.parse(k.revokedAt))}`, cls: "chip chip-dark" };
}

/** Tiền tố hiện trong bảng: "ek_<tiền tố>_…" (không bao giờ có phần bí mật). */
export const prefixText = (k: Pick<ServiceKey, "keyPrefix">) => `ek_${k.keyPrefix}_…`;
