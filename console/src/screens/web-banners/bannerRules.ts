// Luật thuần của màn Banner trang chủ (kế hoạch M5 Task 459; mockup docs/mockups/m5/web-banners.html, HV18, HV19).
// Hiện/ẩn và lý do do máy chủ tính (getBanners, previewBanners của Task 453); ở đây chỉ dựng chữ, đổi danh sách đang sửa
// và dựng thân HomeBanner theo loại. Máy chủ kiểm lại mọi luật (≤ 8 banner, độ dài chữ, ghim, ảnh, giờ).
import { moveTo } from "../products/imageRules";
import type { HomeBanner, HomeBannerHidden, HomeBannerKind, HomeBannerStatus, HomeBannerTheme } from "../../api";

export const MAX_BANNERS = 8;
export const MAX_PINS = 3;
export const KICKER_MAX = 40;
export const TITLE_MAX = 60;
export const CTA_MAX = 30;
export const ALT_MAX = 120;
/** Ảnh nhận (mục đích web_banner): JPEG, PNG, WebP ≤ 8 MB. */
export const BANNER_ACCEPT = "image/jpeg,image/png,image/webp";
export const BANNER_MAX_BYTES = 8 << 20;

export const KINDS: readonly HomeBannerKind[] = ["campaign", "trending", "new", "category", "image"];
/** Tên ngắn (danh sách, ô "Loại banner mới"). */
export const KIND_TEXT: Readonly<Record<HomeBannerKind, string>> = {
  campaign: "Giảm giá",
  trending: "Xu hướng",
  new: "Hàng mới",
  category: "Danh mục",
  image: "Ảnh thiết kế",
};
/** Tên dài (ô "1. Loại banner" của form). */
export const KIND_LONG: Readonly<Record<HomeBannerKind, string>> = {
  campaign: "Chiến dịch giảm giá",
  trending: "Xu hướng (bán chạy 30 ngày)",
  new: "Hàng mới",
  category: "Danh mục",
  image: "Ảnh thiết kế sẵn",
};
export const THEMES: readonly HomeBannerTheme[] = ["auto", "red", "black", "light"];
export const THEME_TEXT: Readonly<Record<HomeBannerTheme, string>> = {
  auto: "Tự động",
  red: "Đỏ",
  black: "Đen",
  light: "Sáng",
};

/** 9 lý do ẩn (đúng thứ tự, chữ của mockup trạng thái D). */
export const HIDDEN_TEXT: Readonly<Record<HomeBannerHidden, string>> = {
  disabled: "Đang tắt",
  scheduled: "Chưa tới giờ hiện",
  expired: "Đã hết thời gian hiện",
  "campaign-missing": "Chưa chọn chiến dịch, hoặc chiến dịch đã bị xóa",
  "campaign-not-running": "Chiến dịch chưa áp dụng (bản nháp, tạm dừng, chưa tới giờ hoặc hết hạn)",
  "category-missing": "Chưa chọn danh mục",
  "no-products": "Chưa có sản phẩm phù hợp để hiện",
  "image-missing": "Chưa tải ảnh, hoặc ảnh không còn trên máy chủ",
  "product-missing": "Sản phẩm liên kết không còn bán",
};

const lowerFirst = (s: string) => s.charAt(0).toLowerCase() + s.slice(1);

/** Dòng trạng thái ở danh sách: "Đang hiện", "Đang ẩn: <lý do>"; chưa có trạng thái → rỗng. */
export function hiddenLine(s: HomeBannerStatus | undefined): string {
  if (s === undefined) return "";
  if (s.shown) return "Đang hiện";
  return s.reason === undefined ? "Đang ẩn" : `Đang ẩn: ${lowerFirst(HIDDEN_TEXT[s.reason])}`;
}

/** Banner mới của loại kind: bật; loại ảnh đích mặc định trang Ưu đãi. */
export function newBanner(kind: HomeBannerKind, id: string): HomeBanner {
  if (kind === "image") return { id, enabled: true, kind, linkKind: "offers", alt: "" };
  return { id, enabled: true, kind, theme: "auto" };
}

/** Đổi loại: giữ mã, bật/tắt, khung giờ; trường của loại cũ bỏ. */
export function changeKind(b: HomeBanner, kind: HomeBannerKind): HomeBanner {
  return {
    ...newBanner(kind, b.id),
    enabled: b.enabled,
    ...(b.startsAt === undefined ? {} : { startsAt: b.startsAt }),
    ...(b.endsAt === undefined ? {} : { endsAt: b.endsAt }),
  };
}

/** Nhân bản: mã mới, bản sao tắt (như admin). */
export function cloneBanner(b: HomeBanner, id: string): HomeBanner {
  return { ...b, id, enabled: false, ...(b.productIds === undefined ? {} : { productIds: [...b.productIds] }) };
}

/** Dời banner ở vị trí i lên (-1) hay xuống (+1); ở mép thì giữ nguyên (moveTo của imageRules kẹp vị trí). */
export function moveBanner<T>(xs: readonly T[], i: number, dir: -1 | 1): T[] {
  return moveTo(xs, i, Math.max(i + dir, 0));
}

const filled = (s: string | undefined): s is string => s !== undefined && s !== "";

function pick(b: HomeBanner): HomeBanner {
  const common: HomeBanner = {
    id: b.id,
    enabled: b.enabled,
    kind: b.kind,
  };
  const time = {
    ...(b.startsAt === undefined ? {} : { startsAt: b.startsAt }),
    ...(b.endsAt === undefined ? {} : { endsAt: b.endsAt }),
  };
  if (b.kind === "image") {
    return {
      ...common,
      ...(b.imageDesktop === undefined ? {} : { imageDesktop: b.imageDesktop }),
      ...(b.imageMobile === undefined ? {} : { imageMobile: b.imageMobile }),
      ...(filled(b.alt) ? { alt: b.alt } : {}),
      ...(b.linkKind === undefined ? {} : { linkKind: b.linkKind }),
      ...(b.linkKind === "offers" || b.linkId === undefined ? {} : { linkId: b.linkId }),
      ...time,
    };
  }
  return {
    ...common,
    ...(b.kind === "campaign" && b.campaignId !== undefined ? { campaignId: b.campaignId } : {}),
    ...(b.kind === "category" && b.categoryId !== undefined ? { categoryId: b.categoryId } : {}),
    ...(filled(b.kicker) ? { kicker: b.kicker } : {}),
    ...(filled(b.title) ? { title: b.title } : {}),
    ...(filled(b.cta) ? { cta: b.cta } : {}),
    theme: b.theme ?? "auto",
    ...(b.productIds === undefined || b.productIds.length === 0 ? {} : { productIds: [...b.productIds] }),
    ...time,
  };
}

/** Thân gửi saveBanners/previewBanners: chỉ trường của đúng loại, bỏ ô chữ rỗng. */
export function toRequest(bs: readonly HomeBanner[]): HomeBanner[] {
  return bs.map(pick);
}

/** Hai danh sách giống nhau theo thân gửi (để biết có thay đổi chưa lưu). */
export function sameBanners(a: readonly HomeBanner[], b: readonly HomeBanner[]): boolean {
  return JSON.stringify(toRequest(a)) === JSON.stringify(toRequest(b));
}

export type Slot = "desktop" | "mobile";
/** Khung ảnh khuyên dùng và vùng an toàn (giữa) của mỗi ô ảnh. */
export const FRAME: Readonly<Record<Slot, { w: number; h: number; safe: number }>> = {
  desktop: { w: 2368, h: 600, safe: 1800 },
  mobile: { w: 1080, h: 640, safe: 900 },
};
const RATIO_TOLERANCE = 0.02;

/** Cảnh báo ảnh lệch tỉ lệ khung quá 2% (ảnh vẫn lưu được, website cắt phần thừa); đúng tỉ lệ → null. */
export function ratioWarning(w: number, h: number, slot: Slot): string | null {
  const f = FRAME[slot];
  const r = w / h / (f.w / f.h);
  if (Math.abs(r - 1) <= RATIO_TOLERANCE) return null;
  const cut = Math.round((r < 1 ? 1 - r : 1 - 1 / r) * 100);
  const where = r < 1 ? "ở trên và dưới" : "ở hai bên";
  return `khác tỉ lệ ${String(f.w)} × ${String(f.h)}: khoảng ${String(cut)}% ảnh ${where} sẽ bị cắt`;
}

/** Trạng thái một ô ảnh trên màn: đang tải, đang xử lý, sẵn sàng, bị từ chối (kèm câu), không còn. */
export interface ImageState {
  phase: "uploading" | "processing" | "ready" | "rejected" | "missing";
  message?: string;
}

const SLOT_TEXT: Readonly<Record<Slot, string>> = { desktop: "máy tính", mobile: "điện thoại" };

/**
 * Lý do nút Lưu khóa vì ảnh (HV18): ảnh đang tải, đang xử lý, bị từ chối, không còn; null khi lưu được. state(b, ô)
 * trả trạng thái ô ảnh của banner b (null khi ô trống). Chỉ banner ảnh có ảnh.
 */
export function blockReason(
  bs: readonly HomeBanner[],
  state: (b: HomeBanner, slot: Slot) => ImageState | null,
): string | null {
  for (const [i, b] of bs.entries()) {
    if (b.kind !== "image") continue;
    for (const slot of ["desktop", "mobile"] as const) {
      const s = state(b, slot);
      const who = `Ảnh ${SLOT_TEXT[slot]} của banner ${String(i + 1)}`;
      if (s?.phase === "uploading") return `${who} đang tải lên, chờ xong mới lưu được.`;
      if (s?.phase === "processing") return `${who} đang xử lý, chờ xong mới lưu được.`;
      if (s?.phase === "rejected") return `${who} bị từ chối. Tải ảnh khác hoặc bỏ ảnh.`;
      if (s?.phase === "missing") return `${who} không còn trên máy chủ. Tải ảnh khác hoặc bỏ ảnh.`;
    }
  }
  return null;
}

/** "Theo thời gian chiến dịch": chép giờ bắt đầu, kết thúc của chiến dịch (như admin cũ). */
export function campaignWindow(c: { startsAt: string; endsAt: string }): { startsAt: string; endsAt: string } {
  return { startsAt: c.startsAt, endsAt: c.endsAt };
}
