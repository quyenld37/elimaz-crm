// Màn Sản phẩm — trình soạn bài viết (kế hoạch M1 Task 326; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, B,
// C) ở 1512 × 767 và 390 × 844, API giả có bộ nhớ. Kiểm: bài 30 khối lưu và tải lại đúng thứ tự; khối chứa
// `<img src=x onerror=alert(1)>` hiện nguyên văn và không chạy (cả ở Xem trước); máy chủ trả invalid_article → lỗi hiện
// tại đúng khối đó; trang không cuộn ngang. Ảnh chụp thẻ bài viết (trạng thái A) lưu vào thư mục kết quả để so với
// docs/mockups/m1/png/san-pham-sua-{1512,390}.png.
import type { Page } from "@playwright/test";
import type { ArticleBlock, Product, ProductClassification, ProductImage, UpdateProductRequest } from "../src/api";
import { expect, forEachViewport, json, noHorizontalScroll, OWNER_GRANTS, test, type FakeApi } from "./fixtures";

const PID = "0195f3b2-0000-7000-8000-000000000123";
const NAME_80 = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
const XSS = "Mẹo phối: đi cùng giày mũi nhọn và túi hộp nhỏ. <img src=x onerror=alert(1)>";
const GONE = "0195f3b2-0000-7000-8000-00000000f0ff";
const GRANTS = [
  ...OWNER_GRANTS,
  ...["catalog.manage"].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

const image = (n: number, color: string): ProductImage => {
  const fileId = `0195f3b2-0000-7000-8000-00000000f00${String(n)}`;
  return {
    id: `0195f3b2-0000-7000-8000-00000000e00${String(n)}`,
    fileId,
    position: n,
    status: "ready",
    color,
    variants: [400, 800].map((width) => ({
      width,
      format: "jpeg" as const,
      url: `/anh-e2e/${fileId}-${String(width)}.svg`,
    })),
  };
};
const IMAGES = [image(1, "Đen"), image(2, "Be")];

/** Bài 30 khối như mockup trạng thái A (khối 4 là ảnh 2, khối 29 có chuỗi HTML, khối 30 là danh sách). */
function thirty(): ArticleBlock[] {
  return Array.from({ length: 30 }, (_, i): ArticleBlock => {
    if (i === 0) return { type: "heading", text: "Chất liệu và form dáng", level: 2 };
    if (i === 1)
      return {
        type: "paragraph",
        text: "Lụa Hàn cao cấp, mặt vải mịn, rủ tự nhiên. Form suông che khuyết điểm vòng hai, dài qua gối 5 cm với người cao 1m60.",
      };
    if (i === 2) return { type: "list", items: ["Giặt tay nước lạnh", "Không vắt mạnh", "Là ở nhiệt độ thấp"] };
    if (i === 3) return { type: "image", fileId: IMAGES[1]?.fileId ?? "" };
    if (i === 28) return { type: "paragraph", text: XSS };
    if (i === 29) return { type: "list", items: ["Đổi size trong 7 ngày"] };
    return { type: "paragraph", text: `Đoạn ${String(i + 1)}` };
  });
}

/** Máy chủ giả: một mẫu; updateProduct lưu bài, version +1; failNext trả lỗi một lần. */
class FakeProduct {
  product: Product;
  failNext: string | null = null;
  patches: UpdateProductRequest[] = [];
  constructor(article: ArticleBlock[]) {
    this.product = {
      id: PID,
      code: "DAM0123",
      name: NAME_80,
      description: "Đầm suông dáng dài, cổ vuông thanh lịch.",
      article,
      articleHtml: "",
      status: "active",
      version: 12,
      variants: [],
      images: IMAGES,
    };
  }
}

async function serve(api: FakeApi, page: Page, fake: FakeProduct) {
  await api.on("**/api/v1/catalog/categories", (r) => json(r, 200, []));
  await api.on("**/api/v1/catalog/tags", (r) => json(r, 200, []));
  await api.on("**/api/v1/catalog/attributes", (r) => json(r, 200, []));
  await api.on(`**/api/v1/catalog/products/${PID}/classification`, (r) => {
    const c: ProductClassification = {
      productId: PID,
      version: fake.product.version,
      attributeValueIds: [],
      tagIds: [],
    };
    return json(r, 200, c);
  });
  await api.on(`**/api/v1/catalog/products/${PID}`, (r) => {
    if (r.request().method() !== "PATCH") return json(r, 200, fake.product);
    const body = r.request().postDataJSON() as UpdateProductRequest;
    fake.patches.push(body);
    if (fake.failNext !== null) {
      const code = fake.failNext;
      fake.failNext = null;
      return json(r, 422, { error: code, request_id: "e2e" });
    }
    if (body.version !== fake.product.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
    const article = (body.article ?? fake.product.article) as ArticleBlock[];
    fake.product = { ...fake.product, article, version: fake.product.version + 1 };
    return json(r, 200, fake.product);
  });
  await page.route("**/anh-e2e/**", (r) =>
    r.fulfill({
      status: 200,
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="500"><rect width="400" height="500" fill="#c9b8a8"/></svg>',
    }),
  );
}

async function open(page: Page, api: FakeApi, article: ArticleBlock[]) {
  const fake = new FakeProduct(article);
  // Mã chèn vào chữ mà chạy được thì sẽ mở hộp alert: coi là lỗi.
  const dialogs: string[] = [];
  page.on("dialog", (d) => {
    dialogs.push(d.message());
    void d.dismiss();
  });
  await api.live({ grants: GRANTS });
  await serve(api, page, fake);
  await page.goto(`/san-pham/${PID}`);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(NAME_80);
  return { fake, dialogs };
}

const card = (page: Page) =>
  page.locator(".card").filter({ has: page.getByRole("heading", { name: "Bài viết chi tiết" }) });
const blocks = (page: Page) => page.getByTestId("article-block");
const save = (page: Page) => page.getByRole("button", { name: "Lưu mẫu" }).last().click();

forEachViewport((vp) => {
  test("trạng thái A: 30 khối, khối 5–28 thu gọn, chữ có < > hiện nguyên văn; không tràn; ảnh chụp", async ({
    page,
    api,
  }, info) => {
    const { dialogs } = await open(page, api, thirty());
    await expect(card(page).getByText("30 khối")).toBeVisible();
    await expect(blocks(page)).toHaveCount(6);
    await expect(page.getByRole("button", { name: "Khối 5–28 (24 khối) đang thu gọn — bấm để mở" })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Khối 29 · Đoạn văn" })).toHaveValue(XSS);
    await expect(page.getByText("Chữ được giữ nguyên văn, kể cả ký tự < >; không có định dạng HTML.")).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Chọn từ ảnh của mẫu (khối 4)" })).toHaveValue(
      IMAGES[1]?.fileId ?? "",
    );
    await expect(blocks(page).nth(3).locator(".thumb img")).toBeVisible();
    const box = await card(page).boundingBox();
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(vp.width);
    await noHorizontalScroll(page);
    await card(page).screenshot({ path: info.outputPath(`bai-viet-trang-thai-a-${String(vp.width)}.png`) });
    expect(dialogs).toEqual([]);
  });

  test("Xem trước: chữ <img src=x onerror=alert(1)> hiện nguyên văn, không chạy; ảnh của khối ảnh hiện", async ({
    page,
    api,
  }) => {
    const { dialogs } = await open(page, api, thirty());
    await card(page).getByRole("tab", { name: "Xem trước" }).click();
    const pv = page.getByTestId("article-preview");
    await expect(pv.getByText(XSS)).toBeVisible();
    await expect(pv.getByRole("heading", { name: "Chất liệu và form dáng" })).toBeVisible();
    await expect(pv.locator("li")).toHaveCount(4);
    await expect(pv.locator("img")).toHaveCount(1);
    await expect(pv.locator("img[onerror], img[src='x']")).toHaveCount(0);
    await noHorizontalScroll(page);
    expect(dialogs).toEqual([]);
  });

  test("bài 0 khối → thêm 30 khối, lưu, tải lại → đúng 30 khối đúng thứ tự", async ({ page, api }) => {
    const { fake, dialogs } = await open(page, api, []);
    await expect(card(page).getByText("Bài viết chưa có khối nào. Website chỉ hiện mô tả ngắn.")).toBeVisible();
    const want: string[] = [];
    for (let i = 1; i <= 30; i++) {
      const text = i === 29 ? XSS : `Đoạn ${String(i)}`;
      want.push(text);
      await card(page).getByRole("button", { name: "+ Đoạn văn" }).click();
      await page.getByRole("textbox", { name: `Khối ${String(i)} · Đoạn văn` }).fill(text);
    }
    // Đổi chỗ khối 1 và 2.
    await page.getByRole("button", { name: "Đưa khối 1 xuống" }).click();
    want.splice(0, 2, want[1] ?? "", want[0] ?? "");
    await save(page);
    await expect.poll(() => fake.product.version).toBe(13);
    expect(fake.product.article).toEqual(want.map((text) => ({ type: "paragraph", text })));

    await page.reload();
    await expect(page.getByRole("heading", { name: NAME_80 })).toBeVisible();
    await expect(card(page).getByText("30 khối")).toBeVisible();
    await page.getByRole("button", { name: /đang thu gọn — bấm để mở$/ }).click();
    await expect(blocks(page)).toHaveCount(30);
    const got = await blocks(page)
      .locator("textarea")
      .evaluateAll((els) => els.map((e) => (e instanceof HTMLTextAreaElement ? e.value : "")));
    expect(got).toEqual(want);
    expect(dialogs).toEqual([]);
  });

  test("máy chủ trả invalid_article → lỗi hiện tại đúng khối (khối ảnh trỏ ảnh đã xóa)", async ({ page, api }) => {
    const art = thirty();
    art[29] = { type: "image", fileId: GONE };
    const { fake } = await open(page, api, art);
    await page.getByRole("textbox", { name: "Khối 2 · Đoạn văn" }).fill("Lụa Hàn, sửa lại.");
    fake.failNext = "invalid_article";
    await save(page);
    await expect(page.getByText("Chưa lưu: bài viết có 1 khối sai.")).toBeVisible();
    await expect(
      page.getByText("Khối 30 (Ảnh): ảnh này không còn dùng được, chọn ảnh khác. Sửa khối được tô đỏ rồi lưu lại."),
    ).toBeVisible();
    const bad = blocks(page).filter({ hasText: "30 · Ảnh" });
    await expect(bad).toHaveClass(/is-err/);
    await expect(bad.getByText("Ảnh này không còn dùng được, chọn ảnh khác.")).toBeVisible();
    await expect(page.locator(".block.is-err")).toHaveCount(1);
    await noHorizontalScroll(page);
    // Chọn ảnh khác rồi lưu → qua.
    await page.getByRole("combobox", { name: "Chọn từ ảnh của mẫu (khối 30)" }).selectOption(IMAGES[0]?.fileId ?? "");
    await expect(page.locator(".block.is-err")).toHaveCount(0);
    await save(page);
    await expect.poll(() => fake.product.version).toBe(13);
    expect(fake.product.article[29]).toEqual({ type: "image", fileId: IMAGES[0]?.fileId });
    expect(fake.patches.length).toBe(2);
  });
});
