// Màn Kiểm kho mở sổ và Hàng hoàn đơn cũ (kế hoạch M7 Task 273; mockup đã duyệt docs/mockups/m7/kiem-kho-mo-so.html và
// hang-hoan-don-cu.html). Đuôi .phone.spec.ts: chạy cả WebKit (iPhone, Safari) và Chromium. VIẾT SẴN, CHƯA CHẠY (lời dặn
// phiên: không chạy Playwright trong lúc làm task; chạy một lần khi xong toàn bộ M7). Đủ ca luật nằm ở vitest
// (src/screens/inventory/opening, legacy); ở đây kiểm bố cục thật: không cuộn ngang ở 360, 390, 1512, tên sản phẩm dài
// 86 ký tự, 412 mã, nút Đăng khóa, quét một mã gửi +1 có khóa chống trùng.
import type { Page } from "@playwright/test";
import type { Grant, LegacyReturn, OpeningStocktake, VariantSearchHit, Warehouse } from "../src/api";
import { expect, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const KHO = "0195f3b2-0000-7000-8000-00000000a001";
const ST = "0195f3b2-0000-7000-8000-0000000c0007";
const V1 = "0195f3b2-0000-7000-8000-00000000b015";
const LONG_NAME = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa Hàn Quốc";
const WAREHOUSES: Warehouse[] = [{ id: KHO, code: "MAIN", name: "Kho chính", status: "active" }];
const GRANTS: Grant[] = [
  "realtime.subscribe",
  "inventory.read",
  "catalog.read",
  "inventory.stocktake",
  "inventory.stocktake.opening",
  "inventory.legacy_return.create",
].map((permission) => ({ permission, scope: { type: "all", id: "" } }));
const HIT: VariantSearchHit = {
  variantId: V1,
  productId: "p1",
  sku: "DAM0123-DEN-S",
  barcode: "2000000000015",
  productCode: "DAM0123",
  productName: LONG_NAME,
  color: "Đen",
  size: "S",
  price: 1,
  sellable: true,
};
const OPENING: OpeningStocktake = {
  stocktakeId: ST,
  warehouseId: KHO,
  status: "open",
  linesExpected: 412,
  linesCounted: 405,
  unitsCounted: 8214,
};
const SIZES = [
  { width: 360, height: 800 },
  { width: 390, height: 844 },
  { width: 1512, height: 767 },
];

async function setup(api: FakeApi, writes: { key: string | null; body: unknown }[]) {
  await api.live({ grants: GRANTS });
  await api.on("**/api/v1/inventory/warehouses", (r) => json(r, 200, WAREHOUSES));
  await api.on("**/api/v1/catalog/variants/search*", (r) => json(r, 200, [HIT]));
  await api.on(`**/api/v1/inventory/opening-stocktakes/${ST}`, (r) => json(r, 200, OPENING));
  await api.on(`**/api/v1/inventory/stocktakes/${ST}/increments`, (r) => {
    writes.push({ key: r.request().headers()["idempotency-key"] ?? null, body: r.request().postDataJSON() });
    return json(r, 200, { variantId: V1, counted: writes.length });
  });
  const returns: LegacyReturn[] = [];
  await api.on("**/api/v1/inventory/legacy-returns*", (r) => json(r, 200, { items: returns }));
}

async function open(page: Page, path: string) {
  await page.goto(path);
}

for (const vp of SIZES) {
  test.describe(`${String(vp.width)} × ${String(vp.height)}`, () => {
    test.use({ viewport: vp });

    test("Kiểm kho mở sổ: 412 mã, tên dài, quét hai lần → hai yêu cầu +1 khác khóa, nút Đăng khóa, không cuộn ngang", async ({
      page,
      api,
    }) => {
      const writes: { key: string | null; body: unknown }[] = [];
      await setup(api, writes);
      await open(page, `/kho/mo-so/${ST}`);
      await expect(page.getByText("405 / 412 mã")).toBeVisible();
      const scan = page.getByLabel("Quét barcode hoặc gõ mã");
      await scan.fill("2000000000015");
      await scan.press("Enter");
      await scan.fill("2000000000015");
      await scan.press("Enter");
      await expect(page.getByText(LONG_NAME).first()).toBeVisible();
      await expect.poll(() => writes.length).toBe(2);
      expect(new Set(writes.map((w) => w.key)).size).toBe(2);
      await expect(page.getByRole("button", { name: /Đăng mở sổ…/ })).toBeDisabled();
      await noHorizontalScroll(page);
    });

    test("Hàng hoàn đơn cũ: thêm món tên dài, hai tình trạng, không cuộn ngang", async ({ page, api }) => {
      await setup(api, []);
      await open(page, "/kho/hang-hoan-cu");
      const scan = page.getByLabel("Quét hoặc gõ SKU để thêm món");
      await scan.fill("DAM0123-DEN-S");
      await scan.press("Enter");
      await expect(page.getByText(LONG_NAME)).toBeVisible();
      await expect(page.getByRole("radiogroup", { name: "Tình trạng DAM0123-DEN-S" })).toBeVisible();
      await noHorizontalScroll(page);
    });
  });
}
