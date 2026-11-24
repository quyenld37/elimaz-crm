// Luật thuần của màn Banner trang chủ (Task 459).
import { describe, expect, it } from "vitest";
import type { HomeBanner, HomeBannerHidden } from "../../api";
import {
  HIDDEN_TEXT,
  MAX_BANNERS,
  blockReason,
  campaignWindow,
  changeKind,
  cloneBanner,
  hiddenLine,
  moveBanner,
  newBanner,
  ratioWarning,
  sameBanners,
  toRequest,
  type ImageState,
} from "./bannerRules";

const ID = "0195f3b2-0000-7000-8000-0000000b0001";
const ID2 = "0195f3b2-0000-7000-8000-0000000b0002";
const F1 = "0195f3b2-0000-7000-8000-0000000f0001";
const F2 = "0195f3b2-0000-7000-8000-0000000f0002";
const C1 = "0195f3b2-0000-7000-8000-00000000e001";

const image = (p: Partial<HomeBanner> = {}): HomeBanner => ({
  id: ID,
  enabled: true,
  kind: "image",
  imageDesktop: F1,
  imageMobile: F2,
  alt: "Black Friday",
  linkKind: "offers",
  ...p,
});

describe("lý do ẩn", () => {
  it("đủ 9 lý do của website, đúng chữ mockup", () => {
    const all: HomeBannerHidden[] = [
      "disabled",
      "scheduled",
      "expired",
      "campaign-missing",
      "campaign-not-running",
      "category-missing",
      "no-products",
      "image-missing",
      "product-missing",
    ];
    expect(all.map((r) => HIDDEN_TEXT[r])).toEqual([
      "Đang tắt",
      "Chưa tới giờ hiện",
      "Đã hết thời gian hiện",
      "Chưa chọn chiến dịch, hoặc chiến dịch đã bị xóa",
      "Chiến dịch chưa áp dụng (bản nháp, tạm dừng, chưa tới giờ hoặc hết hạn)",
      "Chưa chọn danh mục",
      "Chưa có sản phẩm phù hợp để hiện",
      "Chưa tải ảnh, hoặc ảnh không còn trên máy chủ",
      "Sản phẩm liên kết không còn bán",
    ]);
  });
  it("dòng danh sách: đang hiện, đang ẩn chữ thường, chưa có trạng thái", () => {
    expect(hiddenLine({ id: ID, shown: true })).toBe("Đang hiện");
    expect(hiddenLine({ id: ID, shown: false, reason: "scheduled" })).toBe("Đang ẩn: chưa tới giờ hiện");
    expect(hiddenLine({ id: ID, shown: false })).toBe("Đang ẩn");
    expect(hiddenLine(undefined)).toBe("");
  });
});

describe("thêm, đổi loại, nhân bản, dời", () => {
  it("banner mới: bật, màu tự động; banner ảnh: đích Ưu đãi, không màu", () => {
    expect(newBanner("campaign", ID)).toEqual({ id: ID, enabled: true, kind: "campaign", theme: "auto" });
    expect(newBanner("image", ID)).toEqual({ id: ID, enabled: true, kind: "image", linkKind: "offers", alt: "" });
  });
  it("đổi loại giữ mã, bật/tắt, giờ; bỏ trường của loại cũ", () => {
    const b: HomeBanner = {
      ...newBanner("campaign", ID),
      campaignId: C1,
      title: "Xả hàng",
      enabled: false,
      startsAt: "2026-11-14T17:00:00Z",
    };
    expect(changeKind(b, "trending")).toEqual({
      id: ID,
      enabled: false,
      kind: "trending",
      theme: "auto",
      startsAt: "2026-11-14T17:00:00Z",
    });
  });
  it("nhân bản: mã mới, tắt, chép ghim (mảng riêng)", () => {
    const b: HomeBanner = { ...newBanner("category", ID), productIds: [C1] };
    const c = cloneBanner(b, ID2);
    expect(c).toEqual({ ...b, id: ID2, enabled: false });
    expect(c.productIds).not.toBe(b.productIds);
  });
  it("dời lên/xuống, ở mép thì giữ nguyên", () => {
    const a = newBanner("new", ID);
    const b = newBanner("new", ID2);
    expect(moveBanner([a, b], 1, -1)).toEqual([b, a]);
    expect(moveBanner([a, b], 0, -1)).toEqual([a, b]);
    expect(moveBanner([a, b], 1, 1)).toEqual([a, b]);
  });
  it("MAX_BANNERS là 8", () => {
    expect(MAX_BANNERS).toBe(8);
  });
});

describe("thân gửi máy chủ", () => {
  it("banner ảnh: bỏ chữ, màu, ghim; đích Ưu đãi không gửi linkId; bỏ ô rỗng", () => {
    const b: HomeBanner = {
      ...image({ imageMobile: undefined, linkId: C1 }),
      title: "x",
      theme: "red",
      productIds: [C1],
    };
    expect(toRequest([b])).toEqual([
      { id: ID, enabled: true, kind: "image", imageDesktop: F1, alt: "Black Friday", linkKind: "offers" },
    ]);
  });
  it("banner giảm giá: bỏ ảnh, chữ rỗng; giữ chiến dịch, màu, ghim, giờ", () => {
    const b: HomeBanner = {
      ...newBanner("campaign", ID),
      campaignId: C1,
      categoryId: C1,
      kicker: "",
      title: "Xả hàng",
      productIds: [C1],
      imageDesktop: F1,
      endsAt: "2026-11-25T16:59:00Z",
    };
    expect(toRequest([b])).toEqual([
      {
        id: ID,
        enabled: true,
        kind: "campaign",
        campaignId: C1,
        title: "Xả hàng",
        theme: "auto",
        productIds: [C1],
        endsAt: "2026-11-25T16:59:00Z",
      },
    ]);
  });
  it("so hai danh sách theo thân gửi (ô rỗng = không có)", () => {
    const a = newBanner("trending", ID);
    expect(sameBanners([a], [{ ...a, kicker: "" }])).toBe(true);
    expect(sameBanners([a], [{ ...a, enabled: false }])).toBe(false);
  });
});

describe("tỉ lệ ảnh", () => {
  it("lệch ≤ 2% → không cảnh báo", () => {
    expect(ratioWarning(2368, 600, "desktop")).toBeNull();
    expect(ratioWarning(2400, 600, "desktop")).toBeNull(); // 1,4%
  });
  it("ảnh hẹp hơn khung → cắt trên và dưới (mockup: 1920 × 600 → khoảng 19%)", () => {
    expect(ratioWarning(1920, 600, "desktop")).toBe("khác tỉ lệ 2368 × 600: khoảng 19% ảnh ở trên và dưới sẽ bị cắt");
  });
  it("ảnh rộng hơn khung → cắt hai bên", () => {
    expect(ratioWarning(1800, 640, "mobile")).toBe("khác tỉ lệ 1080 × 640: khoảng 40% ảnh ở hai bên sẽ bị cắt");
  });
});

describe("lý do khóa nút Lưu", () => {
  const st =
    (m: Record<string, ImageState>) =>
    (b: HomeBanner, slot: "desktop" | "mobile"): ImageState | null => {
      const id = slot === "desktop" ? b.imageDesktop : b.imageMobile;
      return id === undefined ? null : (m[id] ?? { phase: "ready" });
    };
  it("ảnh điện thoại đang xử lý → câu mockup", () => {
    expect(blockReason([newBanner("new", ID2), image()], st({ [F2]: { phase: "processing" } }))).toBe(
      "Ảnh điện thoại của banner 2 đang xử lý, chờ xong mới lưu được.",
    );
  });
  it("đang tải lên, bị từ chối", () => {
    expect(blockReason([image()], st({ [F1]: { phase: "uploading" } }))).toBe(
      "Ảnh máy tính của banner 1 đang tải lên, chờ xong mới lưu được.",
    );
    expect(blockReason([image()], st({ [F1]: { phase: "rejected", message: "Ảnh quá nhỏ" } }))).toBe(
      "Ảnh máy tính của banner 1 bị từ chối. Tải ảnh khác hoặc bỏ ảnh.",
    );
  });
  it("ảnh không còn trên máy chủ → khóa", () => {
    expect(blockReason([image()], st({ [F2]: { phase: "missing" } }))).toBe(
      "Ảnh điện thoại của banner 1 không còn trên máy chủ. Tải ảnh khác hoặc bỏ ảnh.",
    );
  });
  it("ảnh sẵn sàng → không khóa; banner không phải ảnh bỏ qua trường ảnh", () => {
    expect(blockReason([image()], st({}))).toBeNull();
    expect(
      blockReason([{ ...newBanner("new", ID), imageDesktop: F1 }], st({ [F1]: { phase: "processing" } })),
    ).toBeNull();
  });
});

it("theo thời gian chiến dịch: chép giờ bắt đầu, kết thúc của chiến dịch", () => {
  expect(campaignWindow({ startsAt: "2026-11-14T17:00:00Z", endsAt: "2026-11-25T16:59:00Z" })).toEqual({
    startsAt: "2026-11-14T17:00:00Z",
    endsAt: "2026-11-25T16:59:00Z",
  });
});
