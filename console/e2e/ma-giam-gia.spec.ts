// Màn Mã giảm giá (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/ma-giam-gia.html) ở 1512 × 767, 390 × 844 và 360,
// API giả. Hành vi (đổi đơn vị, trường khóa, đợt mã, tải CSV) kiểm ở test vitest của src/screens/ma-giam-gia; ở đây
// kiểm bố cục không tràn và chụp test-results/ma-giam-gia-*.png để so với docs/mockups/m2/png/ma-giam-gia-*.png.
import type { Page } from "@playwright/test";
import type { Coupon, CouponBatch, CouponRedemption } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000d${String(n).padStart(4, "0")}`;
const BATCH = id(90);
const GRANTS = [
  ...OWNER_GRANTS,
  ...["promotion.read", "promotion.manage"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
];

const base: Coupon = {
  id: id(1),
  code: "THU10",
  name: "Giảm 10% mùa thu cho khách quay lại, áp cho mọi đơn từ năm trăm nghìn",
  discountKind: "percent",
  value: 1000,
  maxDiscount: 150_000,
  minSubtotal: 500_000,
  channels: [],
  maxUses: 200,
  usedCount: 87,
  remainingUses: 113,
  releaseOnCancel: true,
  validFrom: "2026-10-31T17:00:00Z",
  validTo: "2099-11-30T16:59:00Z",
  status: "active",
  bound: false,
  createdAt: "2026-10-30T00:00:00Z",
  version: 3,
};
const ROWS: Coupon[] = [
  base,
  {
    ...base,
    id: id(2),
    code: "FLASH50",
    name: "Live tối 15/11",
    discountKind: "fixed",
    value: 50_000,
    maxDiscount: undefined,
    minSubtotal: 0,
    perCustomerLimit: 1,
    maxUses: 100,
    usedCount: 100,
    remainingUses: 0,
  },
  {
    ...base,
    id: id(3),
    code: "SN-7KQ2M9XA",
    name: "Sinh nhật tháng 11",
    value: 1500,
    maxDiscount: 100_000,
    minSubtotal: 0,
    batchId: BATCH,
    bound: true,
    maxUses: 1,
    usedCount: 0,
  },
  {
    ...base,
    id: id(4),
    code: "HE2026",
    name: "Xả hè",
    value: 2000,
    maxDiscount: undefined,
    minSubtotal: 0,
    channels: ["web"],
    maxUses: undefined,
    usedCount: 412,
    validFrom: "2026-05-31T17:00:00Z",
    validTo: "2026-08-31T16:59:00Z",
  },
  {
    ...base,
    id: id(5),
    code: "NHAPNHAM",
    name: "Tạo nhầm",
    discountKind: "fixed",
    value: 500_000,
    maxDiscount: undefined,
    minSubtotal: 0,
    maxUses: undefined,
    usedCount: 0,
    status: "disabled",
  },
];
const BATCH_ROW: CouponBatch = {
  id: BATCH,
  name: "Sinh nhật tháng 11",
  prefix: "SN",
  quantity: 500,
  template: { name: "Sinh nhật tháng 11", discountKind: "percent", value: 1500, validFrom: "", validTo: "" },
  createdAt: "2026-11-01T00:00:00Z",
};
const REDEMPTIONS: CouponRedemption[] = [
  { id: id(61), orderId: id(71), orderCode: "DH0001236", discount: 150_000, status: "active", createdAt: "" },
  { id: id(62), orderId: id(72), orderCode: "DH0001201", discount: 62_000, status: "active", createdAt: "" },
  {
    id: id(63),
    orderId: id(73),
    orderCode: "DH0001177",
    discount: 80_000,
    status: "released",
    createdAt: "",
    releasedAt: "",
  },
];

async function couponApi(api: FakeApi) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/coupons(\?|$)/, (r) => json(r, 200, { items: ROWS, nextCursor: "c2" }));
  await api.on(/\/api\/v1\/coupons\/[^/]+\/redemptions/, (r) => json(r, 200, { items: REDEMPTIONS }));
  await api.on(/\/api\/v1\/coupons\/[^/?]+$/, (r) => json(r, 200, base));
  await api.on(/\/api\/v1\/coupon-batches\/[^/]+$/, (r) => json(r, 200, BATCH_ROW));
}

const shot = (page: Page, name: string, w: number) =>
  page.screenshot({ path: `test-results/ma-giam-gia-${name}-${String(w)}.png`, fullPage: true });

forEachViewport((vp) => {
  test("danh sách: đủ trạng thái, mã thuộc đợt, không tràn ngang", async ({ page, api }) => {
    await couponApi(api);
    await page.goto("/ma-giam-gia");
    await expect(page.getByRole("link", { name: "SN-7KQ2M9XA" })).toBeVisible();
    await expect(page.getByText("Đợt: Sinh nhật tháng 11 (500 mã)")).toBeVisible();
    await expect(page.getByText("Hết lượt")).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", vp.width);
  });

  test("tạo mã và tạo đợt: hộp không tràn ngang", async ({ page, api }) => {
    await couponApi(api);
    await page.goto("/ma-giam-gia");
    await page.getByRole("button", { name: "Tạo mã" }).click();
    await expect(page.getByRole("dialog", { name: "Tạo mã" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "tao-ma", vp.width);
    await page.getByRole("button", { name: "Hủy" }).click();
    await page.getByRole("button", { name: "Tạo đợt mã" }).click();
    await expect(page.getByRole("dialog", { name: "Tạo đợt mã" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "tao-dot", vp.width);
  });

  test("chi tiết mã đã dùng: ô khóa, lượt dùng, không tràn ngang", async ({ page, api }) => {
    await couponApi(api);
    await page.goto(`/ma-giam-gia/${base.id}`);
    await expect(page.getByLabel("Giảm (%) · khóa")).toBeDisabled();
    await expect(page.getByRole("link", { name: "DH0001177" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "chi-tiet", vp.width);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("danh sách và chi tiết không tràn ngang với tên dài", async ({ page, api }) => {
    await couponApi(api);
    await page.goto("/ma-giam-gia");
    await expect(page.getByRole("link", { name: "THU10" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", 360);
    await page.goto(`/ma-giam-gia/${base.id}`);
    await expect(page.getByRole("heading", { name: "THU10" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "chi-tiet", 360);
  });
});
