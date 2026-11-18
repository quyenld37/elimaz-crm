// Màn Sản phẩm — danh sách (kế hoạch M1 Task 50; mockup docs/mockups/m1/san-pham-danh-sach.html, trạng thái A–E) ở
// 1512 × 767 và 390 × 844, API giả (e2e/fixtures.ts). Kiểm: 0 / 1 / 200 mẫu (cuộn ảo, tải tiếp theo con trỏ), tên 80
// ký tự không tràn, lọc và tìm không thấy, cột "Có thể bán" theo kho. Ảnh chụp trạng thái A, D lưu vào thư mục kết quả
// của test để so với docs/mockups/m1/png/san-pham-danh-sach-{1512,390}.png.
import type { Route } from "@playwright/test";
import type { Category, Grant, ProductListItem, ProductPage, Tag } from "../src/api";
import { expect, forEachViewport, test, type FakeApi } from "./fixtures";

const KHO = "0195f3b2-0000-7000-8000-00000000a001";
const VAY = "0195f3b2-0000-7000-8000-00000000c001";
const VAY_DAI = "0195f3b2-0000-7000-8000-00000000c002";
const HANG_MOI = "0195f3b2-0000-7000-8000-00000000d001";
const NAME_80 = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";

const GRANTS_WITH_WAREHOUSE: Grant[] = [
  ...["realtime.subscribe", "catalog.read"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
  { permission: "inventory.read", scope: { type: "warehouse", id: KHO } },
];

const CATS: Category[] = [
  { id: VAY, name: "Váy", slug: "vay", depth: 0, position: 1, status: "active", version: 1 },
  { id: VAY_DAI, parentId: VAY, name: "Váy dài", slug: "vay-dai", depth: 1, position: 1, status: "active", version: 1 },
];
const TAGS: Tag[] = [{ id: HANG_MOI, code: "moi", name: "Hàng mới" }];

// Ảnh bìa giả: một SVG màu nền như ô ảnh của mockup.
const COVER_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="200"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#d8cfc8"/><stop offset="1" stop-color="#f5f2ef"/></linearGradient></defs><rect width="160" height="200" fill="url(#g)"/></svg>';
const cover = (i: number): ProductListItem["cover"] => ({
  imageId: `img-${String(i)}`,
  status: "ready",
  variants: [{ width: 160, format: "jpeg", url: `/e2e-anh/${String(i)}.svg` }],
});

function product(i: number, extra: Partial<ProductListItem> = {}): ProductListItem {
  const n = String(i).padStart(4, "0");
  return {
    id: `0195f3b2-0000-7000-8000-00000001${n}`,
    code: `DAM${n}`,
    name: `Đầm mẫu số ${n}`,
    status: "active",
    categoryId: VAY_DAI,
    categoryPath: ["Váy", "Váy dài"],
    variantCount: 2,
    minPrice: 350000,
    maxPrice: 350000,
    cover: cover(i),
    updatedAt: "2026-11-18T08:00:00Z",
    ...extra,
  };
}

/** Sáu mẫu của trạng thái A trong mockup (ảnh, tồn, tên dài). */
const MOCKUP_A: ProductListItem[] = [
  product(1, { code: "DAM0123", name: NAME_80, minPrice: 350000, maxPrice: 420000, available: 46 }),
  product(2, {
    code: "AO0456",
    name: "Áo sơ mi lụa cổ đức tay dài",
    categoryPath: ["Áo", "Áo sơ mi"],
    minPrice: 289000,
    maxPrice: 289000,
    available: 12,
  }),
  product(3, {
    code: "QU0789",
    name: "Quần âu ống đứng cạp cao",
    categoryPath: ["Quần", "Quần âu"],
    minPrice: 320000,
    maxPrice: 320000,
    available: -2,
  }),
  product(4, {
    code: "VAY0101",
    name: "Chân váy bút chì xẻ sau",
    categoryPath: ["Váy", "Chân váy"],
    minPrice: 259000,
    maxPrice: 259000,
    available: 0,
  }),
  product(5, {
    code: "SET0012",
    name: "Set vest blazer và chân váy chữ A",
    categoryPath: ["Set", "Set vest"],
    minPrice: 690000,
    maxPrice: 690000,
    available: 8,
    cover: { imageId: "lost", status: "lost" },
  }),
  product(6, {
    code: "AO0321",
    name: "Áo kiểu cổ nơ tay phồng",
    categoryPath: ["Áo", "Áo kiểu"],
    minPrice: 239000,
    maxPrice: 259000,
    available: 133,
  }),
];

/** API sản phẩm có trạng thái: trang 50 mẫu theo con trỏ là vị trí; ghi lại mọi truy vấn đã nhận. */
async function serveProducts(api: FakeApi, all: readonly ProductListItem[], filter?: (q: URLSearchParams) => boolean) {
  const seen: URLSearchParams[] = [];
  const handler = async (route: Route) => {
    const sp = new URL(route.request().url()).searchParams;
    seen.push(sp);
    const list = filter === undefined || filter(sp) ? all : [];
    const start = Number(sp.get("cursor") ?? "0");
    const end = start + Number(sp.get("limit") ?? "50");
    const body: ProductPage = {
      items: list.slice(start, end),
      ...(end < list.length ? { nextCursor: String(end) } : {}),
    };
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  };
  await api.on(/\/api\/v1\/catalog\/products(\?|$)/, handler);
  await api.on("**/api/v1/catalog/categories", (r) =>
    r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(CATS) }),
  );
  await api.on("**/api/v1/catalog/tags", (r) =>
    r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(TAGS) }),
  );
  await api.on("**/e2e-anh/*", (r) => r.fulfill({ status: 200, contentType: "image/svg+xml", body: COVER_SVG }));
  return seen;
}

async function noHorizontalScroll(page: import("@playwright/test").Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
}

const dataRows = (page: import("@playwright/test").Page) => page.locator("tbody tr:not(.v-pad)");

forEachViewport((vp) => {
  test("0 mẫu → Chưa có mẫu nào, nút Nhập Excel và Thêm mẫu; ảnh chụp trạng thái D", async ({ page, api }, info) => {
    await api.live();
    await serveProducts(api, []);
    await page.goto("/san-pham");
    const empty = page.locator(".empty");
    await expect(empty.getByRole("heading", { name: "Chưa có mẫu nào" })).toBeVisible();
    await expect(empty.getByRole("link", { name: "Thêm mẫu" })).toBeVisible();
    await expect(empty.getByRole("link", { name: "Nhập Excel" })).toBeVisible();
    await expect(page.getByRole("table")).toHaveCount(0);
    await noHorizontalScroll(page);
    await page.screenshot({ path: info.outputPath(`trang-thai-d-${String(vp.width)}.png`), fullPage: true });
  });

  test("1 mẫu → một dòng, Hết danh sách, bấm dòng sang trang sửa mẫu", async ({ page, api }) => {
    await api.live();
    await serveProducts(api, [
      product(1, { code: "DAM0001", name: "Đầm bút chì cổ tim", minPrice: 390000, maxPrice: 390000 }),
    ]);
    await page.goto("/san-pham");
    await expect(page.getByText("DAM0001")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(1);
    await expect(page.getByText("390.000 ₫")).toBeVisible();
    await expect(page.getByText("Hết danh sách.")).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByText("Đầm bút chì cổ tim").click();
    await expect(page).toHaveURL(/\/san-pham\/0195f3b2-0000-7000-8000-000000010001$/);
  });

  test("200 mẫu: cuộn tải tiếp theo con trỏ tới hết, chỉ dựng phần đang thấy", async ({ page, api }) => {
    await api.live();
    const all = Array.from({ length: 200 }, (_, i) => product(i + 1));
    const seen = await serveProducts(api, all);
    await page.goto("/san-pham");
    await expect(page.getByText("Đang hiện 50 mẫu đầu. Cuộn xuống để tải tiếp.")).toBeVisible();
    // Cuộn tới cuối nhiều lần: mỗi lần chạm cuối tải thêm 50 mẫu, tới khi hết.
    await expect(async () => {
      await page.evaluate(() => {
        window.scrollTo(0, document.documentElement.scrollHeight);
      });
      await expect(page.getByText("Hết danh sách.")).toBeVisible({ timeout: 500 });
    }).toPass({ timeout: 15_000 });
    // Trang cuối vừa nối thêm vào dưới: cuộn xuống đáy lần nữa thì thấy mẫu cuối.
    await expect(async () => {
      await page.evaluate(() => {
        window.scrollTo(0, document.documentElement.scrollHeight);
      });
      await expect(page.getByText("DAM0200")).toBeVisible({ timeout: 500 });
    }).toPass({ timeout: 10_000 });
    expect(seen.map((s) => s.get("cursor"))).toEqual([null, "50", "100", "150"]);
    expect(seen.every((s) => s.get("limit") === "50")).toBe(true);
    // Danh sách ảo: 200 mẫu đã tải nhưng chỉ vài chục dòng nằm trong DOM.
    expect(await dataRows(page).count()).toBeLessThan(60);
    await expect(page.getByText("DAM0001")).toHaveCount(0);
    await page.evaluate(() => {
      window.scrollTo(0, 0);
    });
    await expect(page.getByText("DAM0001")).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("tên 80 ký tự cắt hai dòng, không tràn; ảnh chụp trạng thái A", async ({ page, api }, info) => {
    await api.live({ grants: GRANTS_WITH_WAREHOUSE });
    await serveProducts(
      api,
      [...MOCKUP_A, ...Array.from({ length: 44 }, (_, i) => product(i + 7))].concat(
        Array.from({ length: 150 }, (_, i) => product(i + 51)),
      ),
    );
    await page.goto("/san-pham?status=active");
    const name = page.getByText(NAME_80);
    await expect(name).toBeVisible();
    const box = await name.boundingBox();
    const lineHeight = await name.evaluate((el) => parseFloat(getComputedStyle(el).lineHeight));
    expect(box?.height).toBeLessThanOrEqual(lineHeight * 2 + 1);
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(vp.width);
    // Cột có thể bán: âm đỏ kèm câu thiếu, 0 kèm Hết hàng; ảnh mất có ghi chú.
    await expect(page.getByText("Thiếu 2 so với phần giữ")).toBeVisible();
    await expect(page.getByText("Hết hàng", { exact: true })).toBeVisible();
    await expect(page.getByText("Ảnh bị mất")).toBeVisible();
    await expect(page.getByText("Trạng thái: Đang bán")).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: info.outputPath(`trang-thai-a-${String(vp.width)}.png`), fullPage: false });
  });

  test("lọc, tìm không thấy → câu của trạng thái E; Xóa bộ lọc về danh sách", async ({ page, api }) => {
    await api.live();
    const seen = await serveProducts(api, [product(1)], (sp) => sp.get("q") === null);
    await page.goto("/san-pham");
    await expect(page.getByText("DAM0001")).toBeVisible();
    await page.getByRole("combobox", { name: "Trạng thái" }).selectOption("active");
    await page.getByRole("combobox", { name: "Danh mục" }).selectOption(VAY);
    await page.getByRole("searchbox", { name: "Tìm sản phẩm" }).fill("đầm nhung");
    const empty = page.locator(".empty");
    await expect(empty.getByRole("heading", { name: 'Không có mẫu khớp "đầm nhung"' })).toBeVisible();
    await expect(empty.getByText("Đang lọc: Trạng thái Đang bán, danh mục Váy.")).toBeVisible();
    const last = seen.at(-1);
    expect(last?.get("q")).toBe("đầm nhung");
    expect(last?.getAll("status")).toEqual(["active"]);
    expect(last?.get("categoryId")).toBe(VAY);
    await noHorizontalScroll(page);
    await empty.getByRole("button", { name: "Xóa bộ lọc" }).click();
    await expect(page.getByText("DAM0001")).toBeVisible();
    await expect(page.getByRole("searchbox", { name: "Tìm sản phẩm" })).toHaveValue("");
    await expect(page).toHaveURL(/\/san-pham$/);
  });
});

test("không có kho trong grant → không có ô chọn kho, không gửi warehouseId", async ({ page, api }) => {
  await api.live();
  const seen = await serveProducts(api, [product(1)]);
  await page.goto("/san-pham");
  await expect(page.getByText("DAM0001")).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Có thể bán" })).toHaveCount(0);
  await expect(page.getByRole("combobox", { name: "Kho tính có thể bán" })).toHaveCount(0);
  expect(seen.every((s) => s.get("warehouseId") === null)).toBe(true);
});
