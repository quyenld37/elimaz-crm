// Màn Bộ phối (kế hoạch M5 Task 460; mockup docs/mockups/m5/web-looks.html) ở 1512 × 767, 390 × 844 và 360, API giả.
// Hành vi (tối đa 6 trên trang chủ, kẹp chấm, 409, quyền) kiểm ở test vitest của src/screens/web-looks; ở đây kiểm bố cục
// không tràn ngang với dữ liệu khó (tên 28 ký tự không dấu cách, tên món dài, 3 món, 7 bộ có 6 trên trang chủ, bộ ẩn
// vì hết hàng) và chụp test-results/web-looks-*.png để so với mockup.
import type { Page } from "@playwright/test";
import type { Look, LookList, Product } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`;
const P = [id(901), id(902), id(903)] as const;
const PHOTO = id(951);
const LONG_NAME = "ThứHaiĐầuTuầnMặcGìĐểTựTinNhé";
const PRODUCT_NAMES = [
  "Áo sơ mi lụa tay bồng cổ nơ phối ren thêu tay dáng suông dài",
  "Chân váy bút chì xẻ sau",
  "Giày mũi nhọn da bò",
];
const GRANTS = [
  ...OWNER_GRANTS,
  ...["content.read", "content.manage"].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

const items = P.map((productId, k) => ({ productId, color: "Đen", x: 97, y: 25 + 25 * k }));
const LOOKS: Look[] = Array.from({ length: 7 }, (_, i) => ({
  id: id(i + 1),
  onHome: i < 6,
  name: i === 0 ? LONG_NAME : `Dịp ${String(i + 1)}`,
  headline: "Sáng thứ Hai đi làm — mặc gì để gọn gàng mà vẫn mềm mại?",
  modelHeightCm: 162,
  modelSize: "S",
  photoFileId: PHOTO,
  items,
}));

const LIST: LookList = {
  revision: 3,
  looks: LOOKS,
  statuses: LOOKS.map((l, i) => ({
    id: l.id,
    shown: i < 6 && i !== 2,
    ...(i === 2 ? { reason: "sold-out" as const, productId: P[0] } : i >= 6 ? { reason: "disabled" as const } : {}),
    autoTitle: PRODUCT_NAMES.join(", "),
    linkKey: "0195f3b2",
    path: `/bo-phoi/dip-${String(i + 1)}-0195f3b2`,
  })),
  products: P.map((pid, k) => ({
    id: pid,
    code: `MA${String(k)}`,
    name: PRODUCT_NAMES[k] ?? "",
    onSale: true,
    inStock: k !== 0,
  })),
  photos: [{ productId: P[0], fileId: PHOTO, color: "Đen", position: 0, ready: true, variants: [] }],
  categories: [{ id: id(800), name: "Áo sơ mi công sở dáng dài" }],
};

const product = (pid: string, k: number): Product => ({
  id: pid,
  code: `MA${String(k)}`,
  name: PRODUCT_NAMES[k] ?? "",
  description: "",
  article: [],
  articleHtml: "",
  status: "active",
  version: 1,
  variants: [
    {
      id: id(700 + k),
      productId: pid,
      sku: `MA${String(k)}-DEN-S`,
      barcode: "",
      color: "Đen",
      size: "S",
      price: 590_000,
      status: "active",
      version: 1,
    },
  ],
  images: [],
});

async function looksApi(api: FakeApi) {
  await api.live({ grants: GRANTS });
  await api.on("**/api/v1/content/looks", (r) => json(r, 200, LIST));
  await api.on(/\/api\/v1\/catalog\/products\/[^/?]+$/, (r) => {
    const pid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    const k = P.findIndex((x) => x === pid);
    return json(r, k < 0 ? 404 : 200, k < 0 ? { error: "not_found", request_id: "e2e" } : product(pid, k));
  });
}

const shot = (page: Page, name: string, w: number) =>
  page.screenshot({ path: `test-results/web-looks-${name}-${String(w)}.png`, fullPage: true });

forEachViewport((vp) => {
  test("danh sách 7 bộ, 6/6 trang chủ, bộ thứ 7 khóa; form 3 món; xem trước — không tràn ngang", async ({
    page,
    api,
  }) => {
    await looksApi(api);
    await page.goto("/website/bo-phoi");
    await expect(page.getByText("7 bộ · 6/6 bật hiện trên trang chủ · 5 đang hiện")).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "Trang chủ: bộ 7" })).toBeDisabled();
    await expect(page.getByPlaceholder("Đã đủ 3 sản phẩm — bỏ một món để thêm")).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", vp.width);
  });

  test("hộp xóa có liên kết dài không tràn ngang", async ({ page, api }) => {
    await looksApi(api);
    await page.goto("/website/bo-phoi");
    await page.getByRole("button", { name: "Xóa", exact: true }).click();
    await expect(page.getByRole("dialog")).toContainText("sẽ không mở được nữa");
    await noHorizontalScroll(page);
    await shot(page, "xoa", vp.width);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("tên 28 ký tự không dấu cách, tên món dài không tràn ngang", async ({ page, api }) => {
    await looksApi(api);
    await page.goto("/website/bo-phoi");
    await expect(page.getByRole("heading", { name: `Bộ 1 · ${LONG_NAME}` })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", 360);
  });
});
