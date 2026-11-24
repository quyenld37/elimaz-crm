// Luật hiển thị và kiểm form của màn Chiến dịch giảm giá (kế hoạch M5 Task 458; mockup docs/mockups/m5/web-campaigns.html).
// Trạng thái hiển thị (draft, paused, scheduled, active, ended) do máy chủ tính; ở đây chỉ dựng chữ, chọn thao tác và
// dựng thân WebCampaignInput. Máy chủ kiểm lại mọi luật (tên 1–120, phần trăm 1–99 và trần, đích 1–30, giờ).
import type { WebCampaign, WebCampaignInput, WebCampaignScope, WebCampaignStatus } from "../../api";
import { shortTime } from "../../app/time";
import { isoToVnLocal, vnLocalToIso } from "../ma-giam-gia/couponRules";

/**
 * Tên miền website: API chỉ trả `path` (/uu-dai/<tên>-<linkKey>). Tệp task 458, Quyết định 1: tên miền theo spec
 * README; đổi ở đây khi website đổi tên miền.
 */
export const WEBSITE_ORIGIN = "https://elimaz.com.vn";
export const NAME_MAX = 120;
export const TARGET_MAX = 30;
export const REASON_MAX = 300;
export const PAGE_SIZES = [10, 20, 50] as const;
export const DEFAULT_PAGE_SIZE = 20;

export const STATUSES: readonly WebCampaignStatus[] = ["draft", "paused", "scheduled", "active", "ended"];
export const SCOPES: readonly WebCampaignScope[] = ["categories", "all", "products"];
export const SCOPE_TEXT: Readonly<Record<WebCampaignScope, string>> = {
  categories: "Danh mục được chọn",
  all: "Toàn bộ sản phẩm",
  products: "Sản phẩm được chọn",
};

const STATUS_TEXT: Readonly<Record<WebCampaignStatus, string>> = {
  draft: "Bản nháp",
  paused: "Tạm dừng",
  scheduled: "Đã hẹn giờ",
  active: "Đang áp dụng",
  ended: "Đã hết hạn",
};

export const statusText = (s: WebCampaignStatus): string => STATUS_TEXT[s];

export function statusChip(s: WebCampaignStatus): { label: string; className: string } {
  const className = s === "active" ? "chip chip-ok" : s === "scheduled" ? "chip chip-line" : "chip";
  return { label: STATUS_TEXT[s], className };
}

/** "2 danh mục", "18 sản phẩm", "Toàn bộ sản phẩm". */
export function scopeText(scope: WebCampaignScope, n: number): string {
  if (scope === "all") return "Toàn bộ sản phẩm";
  return `${String(n)} ${scope === "products" ? "sản phẩm" : "danh mục"}`;
}

/** Dòng phụ dưới tên: phạm vi · Hàng cắt mã · dùng kèm mã giảm giá. */
export function rowSub(c: WebCampaign): string {
  return [
    scopeText(c.scope, c.targetIds.length),
    ...(c.clearance ? ["Hàng cắt mã"] : []),
    ...(c.allowCoupon ? ["dùng kèm mã giảm giá"] : []),
  ].join(" · ");
}

/** "00:00 15/11 → 23:59 25/11" theo giờ Việt Nam. */
export const periodText = (c: WebCampaign): string => `${shortTime(c.startsAt)} → ${shortTime(c.endsAt)}`;

/** Liên kết đầy đủ để chép. */
export const campaignLink = (c: WebCampaign): string => WEBSITE_ORIGIN + c.path;
/** Liên kết hiện trong câu báo (bỏ https://). */
export const linkText = (c: WebCampaign): string => campaignLink(c).replace(/^https?:\/\//, "");

/** Thao tác trên dòng: pauseEdit là "Sửa" của chiến dịch đang bật (hộp Tạm dừng và sửa, HV16). */
export type RowAction = "edit" | "pauseEdit" | "pause" | "enable" | "delete" | "copy";

export function rowActions(c: WebCampaign, canManage: boolean): RowAction[] {
  const running = c.state === "enabled";
  const copy: RowAction[] = running && c.status !== "ended" ? ["copy"] : [];
  if (!canManage) return copy;
  return running ? ["pauseEdit", "pause", ...copy] : ["edit", "enable", "delete"];
}

/** Một đích đang chọn (mẫu hay danh mục) kèm tên để hiện. */
export interface PickedTarget {
  id: string;
  code: string;
  name: string;
}

/** Đích có trong chiến dịch mà danh sách đích không còn (mẫu đã xóa, danh mục đã bỏ). */
export const missingTarget = (id: string): PickedTarget => ({
  id,
  code: "",
  name: `Mục không còn (…${id.slice(-8)})`,
});

/** Nội dung form (chữ của các ô, giờ là chữ của ô datetime-local theo giờ Việt Nam). */
export interface CampaignDraft {
  name: string;
  percent: string;
  scope: WebCampaignScope;
  targets: PickedTarget[];
  start: string;
  end: string;
  allowCoupon: boolean;
  clearance: boolean;
}

export type DraftErrors = Partial<Record<"name" | "percent" | "targets" | "start" | "end", string>>;

/** Form trống của chiến dịch mới: mặc định chọn danh mục (tệp task 458, Quyết định 6). */
export const emptyDraft = (): CampaignDraft => ({
  name: "",
  percent: "",
  scope: "categories",
  targets: [],
  start: "",
  end: "",
  allowCoupon: false,
  clearance: false,
});

export function draftOf(c: WebCampaign, targets: readonly PickedTarget[]): CampaignDraft {
  return {
    name: c.name,
    percent: String(c.percent),
    scope: c.scope,
    targets: [...targets],
    start: isoToVnLocal(c.startsAt),
    end: isoToVnLocal(c.endsAt),
    allowCoupon: c.allowCoupon,
    clearance: c.clearance,
  };
}

export const runeCount = (s: string): number => Array.from(s).length;

function percentOf(s: string): number | null {
  if (!/^\d{1,2}$/.test(s.trim())) return null;
  const n = Number(s.trim());
  return n >= 1 && n <= 99 ? n : null;
}

export function validateDraft(d: CampaignDraft): DraftErrors {
  const e: DraftErrors = {};
  const n = runeCount(d.name.trim());
  if (n === 0) e.name = "Nhập tên chiến dịch";
  else if (n > NAME_MAX) e.name = "Tên tối đa 120 ký tự";
  if (percentOf(d.percent) === null) e.percent = "Phần trăm là số nguyên 1–99";
  if (d.scope !== "all") {
    if (d.targets.length === 0) {
      e.targets = d.scope === "products" ? "Chọn ít nhất một sản phẩm" : "Chọn ít nhất một danh mục";
    } else if (d.targets.length > TARGET_MAX) e.targets = "Tối đa 30 mục";
  }
  const start = vnLocalToIso(d.start);
  const end = vnLocalToIso(d.end);
  if (start === null) e.start = "Chọn giờ bắt đầu";
  if (end === null) e.end = "Chọn giờ kết thúc";
  else if (start !== null && Date.parse(end) <= Date.parse(start)) e.end = "Kết thúc phải sau bắt đầu";
  return e;
}

/** Thân WebCampaignInput của form hợp lệ; form sai → null. */
export function toInput(d: CampaignDraft): WebCampaignInput | null {
  const percent = percentOf(d.percent);
  const startsAt = vnLocalToIso(d.start);
  const endsAt = vnLocalToIso(d.end);
  if (Object.keys(validateDraft(d)).length > 0 || percent === null || startsAt === null || endsAt === null) {
    return null;
  }
  return {
    name: d.name.trim(),
    percent,
    scope: d.scope,
    targetIds: d.scope === "all" ? [] : d.targets.map((t) => t.id),
    allowCoupon: d.allowCoupon,
    clearance: d.clearance,
    startsAt,
    endsAt,
  };
}

const sortedIds = (ids: readonly string[]) => [...ids].sort().join(",");

/** Form trùng bản đã lưu c (đích không kể thứ tự, giờ so theo mốc): không cần lưu trước khi xem trước. */
export function sameAsSaved(d: CampaignDraft, c: WebCampaign): boolean {
  const i = toInput(d);
  if (i === null) return false;
  return (
    i.name === c.name &&
    i.percent === c.percent &&
    i.scope === c.scope &&
    sortedIds(i.targetIds ?? []) === sortedIds(c.targetIds) &&
    i.allowCoupon === c.allowCoupon &&
    i.clearance === c.clearance &&
    Date.parse(i.startsAt) === Date.parse(c.startsAt) &&
    Date.parse(i.endsAt) === Date.parse(c.endsAt)
  );
}

/** Câu vượt trần (HV17): API không trả số trần (tệp task 458, Quyết định 2). */
export const percentTooHighText = (percent: number | string): string =>
  `Chiến dịch giảm ${String(percent)}%, vượt trần của shop.`;
export const TEXT_RAISE_CAP = "Giảm sâu hơn để xả hàng thì nhờ Chủ nâng trần trước; chiến dịch vẫn ở trạng thái cũ.";
export const TEXT_READ_ONLY =
  'Bạn chỉ có quyền xem khuyến mãi (thiếu quyền "Quản lý khuyến mãi"). "Chép liên kết" vẫn dùng được.';
