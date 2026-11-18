// Màn Sản phẩm — khối Ảnh (kế hoạch M1 Task 325; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, D, E) ở
// 1512 × 767 và 390 × 844, API giả có bộ nhớ (e2e/fixtures.ts + máy chủ giả dưới đây). Kiểm: tải 3 ảnh (một ảnh là
// HTML đổi đuôi html-as.jpg) → 2 ảnh vào lưới, 1 dòng "Tệp không phải ảnh"; xóa ảnh → thông báo Hoàn tác, bấm thì ảnh
// về; ảnh lost → ô trống có ghi chú; dời ảnh ← →; trang không cuộn ngang. Ảnh chụp trạng thái A lưu vào thư mục kết
// quả để so với docs/mockups/m1/png/san-pham-sua-{1512,390}.png.
import type { Page, Route } from "@playwright/test";
import type {
  AttachProductImageRequest,
  CreateUploadRequest,
  FileInfo,
  Product,
  ProductClassification,
  ProductImage,
  ReorderProductImagesRequest,
  Variant,
} from "../src/api";
import { expect, forEachViewport, noHorizontalScroll, OWNER_GRANTS, test, type FakeApi } from "./fixtures";

const PID = "0195f3b2-0000-7000-8000-000000000123";
const NAME_80 = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
const GRANTS = [
  ...OWNER_GRANTS,
  ...["catalog.manage", "files.upload"].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

let seq = 0;
const newId = () => {
  seq++;
  return `0195f3b2-0000-7000-8000-${String(900000 + seq).padStart(12, "0")}`;
};

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
const err = (route: Route, status: number, code: string) => json(route, status, { error: code, request_id: "e2e" });

const variantsOf = (fileId: string): ProductImage["variants"] =>
  [400, 800].map((width) => ({ width, format: "jpeg" as const, url: `/anh-e2e/${fileId}-${String(width)}.svg` }));

/** Biến thể 2 màu × 2 size (màu của ảnh chọn trong Đen, Be). */
const VARIANTS: Variant[] = (["Đen", "Be"] as const).flatMap((color, ci) =>
  ["S", "M"].map((size, si) => ({
    id: `0195f3b2-0000-7000-8000-00000000020${String(ci * 2 + si)}`,
    productId: PID,
    sku: `DAM0123-${String(ci)}-${size}`,
    barcode: `2000000000${String(ci)}${String(si)}0`,
    color,
    size,
    price: 350000,
    status: "active" as const,
    version: 1,
  })),
);

/** Máy chủ giả: một mẫu, ảnh đang hiện và đã xóa, tệp tải lên (tên → trạng thái sau khi xử lý). */
class FakeImages {
  product: Product;
  deleted = new Map<string, ProductImage>();
  files = new Map<string, { name: string; status: FileInfo["status"]; polls: number; put: boolean }>();
  done = new Map<string, { status: number; body: unknown }>();

  constructor(images: ProductImage[]) {
    this.product = {
      id: PID,
      code: "DAM0123",
      name: NAME_80,
      description: "",
      article: [],
      articleHtml: "",
      status: "active",
      version: 12,
      variants: VARIANTS,
      images,
    };
  }

  /** Ảnh đang hiện mới, version +1. Như máy chủ thật: chỉ sắp lại mới đánh số lại; gắn thêm thì vào cuối. */
  set(images: ProductImage[], renumber = false): Product {
    this.product = {
      ...this.product,
      images: renumber ? images.map((x, i) => ({ ...x, position: i + 1 })) : images,
      version: this.product.version + 1,
    };
    return this.product;
  }

  file(id: string): FileInfo | null {
    const f = this.files.get(id);
    if (f === undefined) return null;
    // Lần hỏi đầu: đang xử lý; sau đó: kết quả theo nội dung (html-as.jpg là HTML → not_image).
    if (f.status === "processing" && f.polls > 0) f.status = f.name === "html-as.jpg" ? "rejected" : "ready";
    f.polls++;
    return {
      id,
      purpose: "product_image",
      status: f.status,
      variants: f.status === "ready" ? (variantsOf(id) ?? []) : [],
      ...(f.status === "rejected" ? { rejectReason: "not_image" } : {}),
    };
  }
}

async function serve(api: FakeApi, page: Page, fake: FakeImages) {
  const cls: ProductClassification = { productId: PID, version: 12, attributeValueIds: [], tagIds: [] };
  await api.on("**/api/v1/catalog/categories", (r) => json(r, 200, []));
  await api.on("**/api/v1/catalog/tags", (r) => json(r, 200, []));
  await api.on("**/api/v1/catalog/attributes", (r) => json(r, 200, []));
  await api.on(`**/api/v1/catalog/products/${PID}`, (r) => json(r, 200, fake.product));
  await api.on(`**/api/v1/catalog/products/${PID}/classification`, (r) =>
    json(r, 200, { ...cls, version: fake.product.version }),
  );
  // Ảnh công khai và đường PUT ký sẵn (cùng gốc với trang để khỏi CORS).
  await page.route("**/anh-e2e/**", (r) =>
    r.fulfill({
      status: 200,
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="500"><rect width="400" height="500" fill="#c9b8a8"/></svg>',
    }),
  );
  await page.route("**/kho-e2e/**", async (r) => {
    const id = new URL(r.request().url()).pathname.split("/").at(-1) ?? "";
    const f = fake.files.get(id);
    if (r.request().method() !== "PUT" || f === undefined) return r.fulfill({ status: 403 });
    f.put = true;
    return r.fulfill({ status: 200 });
  });
  await api.on("**/api/v1/files/**", async (r) => {
    const req = r.request();
    const path = new URL(req.url()).pathname;
    if (req.method() === "GET") {
      const info = fake.file(path.split("/").at(-1) ?? "");
      return info === null ? err(r, 404, "not_found") : json(r, 200, info);
    }
    const key = req.headers()["idempotency-key"] ?? "";
    const saved = fake.done.get(key);
    if (saved !== undefined) return json(r, saved.status, saved.body);
    let reply: { status: number; body: unknown };
    if (path === "/api/v1/files/uploads") {
      const b = req.postDataJSON() as CreateUploadRequest;
      const id = newId();
      fake.files.set(id, { name: b.fileName, status: "awaiting_upload", polls: 0, put: false });
      reply = {
        status: 201,
        body: { fileId: id, putUrl: `/kho-e2e/tmp/${id}`, headers: { "Content-Type": b.contentType }, expiresAt: "" },
      };
    } else {
      const id = path.split("/").at(-2) ?? "";
      const f = fake.files.get(id);
      if (f?.put !== true) return err(r, 409, "upload_missing");
      f.status = "processing";
      reply = { status: 200, body: fake.file(id) };
    }
    fake.done.set(key, reply);
    return json(r, reply.status, reply.body);
  });
  await api.on(new RegExp(`/api/v1/catalog/products/${PID}/images(/.*)?$`), async (r) => {
    const req = r.request();
    const path = new URL(req.url()).pathname.replace(`/api/v1/catalog/products/${PID}/images`, "");
    const key = req.headers()["idempotency-key"] ?? "";
    const saved = fake.done.get(key);
    if (saved !== undefined) return json(r, saved.status, saved.body);
    const imgs = fake.product.images;
    let reply: { status: number; body: unknown } = { status: 404, body: { error: "not_found", request_id: "e2e" } };
    const conflict = { status: 409, body: { error: "version_conflict", request_id: "e2e" } };
    if (req.method() === "POST" && path === "") {
      const b = req.postDataJSON() as AttachProductImageRequest;
      const f = fake.files.get(b.fileId);
      if (b.productVersion !== fake.product.version) reply = conflict;
      else if (f?.status !== "ready") reply = { status: 409, body: { error: "file_not_ready", request_id: "e2e" } };
      else {
        const img: ProductImage = {
          id: newId(),
          fileId: b.fileId,
          position: Math.max(0, ...imgs.map((x) => x.position)) + 1,
          status: "ready",
          ...(b.color === undefined ? {} : { color: b.color }),
          variants: variantsOf(b.fileId) ?? [],
        };
        reply = { status: 201, body: fake.set([...imgs, img]) };
      }
    } else if (req.method() === "PUT" && path === "/order") {
      const b = req.postDataJSON() as ReorderProductImagesRequest;
      if (b.productVersion !== fake.product.version) reply = conflict;
      else
        reply = {
          status: 200,
          body: fake.set(
            b.imageIds.map((id) => imgs.find((x) => x.id === id)).filter((x) => x !== undefined),
            true,
          ),
        };
    } else if (req.method() === "DELETE") {
      const id = path.slice(1);
      const img = imgs.find((x) => x.id === id);
      if (img !== undefined) {
        fake.deleted.set(id, img);
        reply = { status: 200, body: fake.set(imgs.filter((x) => x.id !== id)) };
      }
    } else if (req.method() === "POST" && path.endsWith("/restore")) {
      const id = path.split("/")[1] ?? "";
      const img = fake.deleted.get(id);
      if (img !== undefined) {
        fake.deleted.delete(id);
        const next = [...imgs, img].sort((a, b) => a.position - b.position);
        reply = { status: 200, body: fake.set(next) };
      }
    }
    if (reply.status < 300) fake.done.set(key, reply);
    return json(r, reply.status, reply.body);
  });
}

const tiles = (page: Page) => page.getByTestId("img-tile");

/** Ảnh của trạng thái A: ảnh bìa màu Đen, ảnh 2 màu Be, ảnh 3 mất. */
function mockupImages(): ProductImage[] {
  const a = newId();
  const b = newId();
  return [
    { id: newId(), fileId: a, position: 1, status: "ready", color: "Đen", variants: variantsOf(a) ?? [] },
    { id: newId(), fileId: b, position: 2, status: "ready", color: "Be", variants: variantsOf(b) ?? [] },
    { id: newId(), fileId: newId(), position: 3, status: "lost", color: "Đen" },
  ];
}

async function open(page: Page, api: FakeApi, images: ProductImage[]) {
  const fake = new FakeImages(images);
  await api.live({ grants: GRANTS });
  await serve(api, page, fake);
  await page.goto(`/san-pham/${PID}`);
  await expect(page.getByRole("heading", { name: NAME_80 })).toBeVisible();
  return fake;
}

forEachViewport((vp) => {
  test("trạng thái A: ảnh bìa, ảnh mất có ghi chú, ô màu; không tràn; ảnh chụp so mockup", async ({
    page,
    api,
  }, info) => {
    await open(page, api, mockupImages());
    await expect(tiles(page)).toHaveCount(3);
    await expect(page.getByText("3 ảnh", { exact: true })).toBeVisible();
    await expect(tiles(page).nth(0).getByText("1 · Ảnh bìa")).toBeVisible();
    await expect(tiles(page).nth(0).locator("img")).toBeVisible();
    await expect(tiles(page).nth(2).getByText("Ảnh bị mất")).toBeVisible();
    await expect(tiles(page).nth(2).getByText("Kho tệp không còn ảnh này. Tải lại ảnh.")).toBeVisible();
    await expect(tiles(page).nth(2).getByRole("button", { name: "Tải lại" })).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Màu của ảnh 2" })).toHaveValue("Be");
    for (let i = 0; i < 3; i++) {
      const box = await tiles(page).nth(i).boundingBox();
      expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(vp.width);
    }
    await noHorizontalScroll(page);
    await page.locator(".p-images").screenshot({ path: info.outputPath(`anh-trang-thai-a-${String(vp.width)}.png`) });
  });

  test("tải 3 ảnh, một ảnh là HTML đổi đuôi → 2 ảnh vào lưới, 1 báo Tệp không phải ảnh", async ({ page, api }) => {
    const fake = await open(page, api, []);
    await expect(page.getByText("0 ảnh", { exact: true })).toBeVisible();
    await page.getByLabel("Chọn ảnh từ máy").setInputFiles([
      { name: "a.jpg", mimeType: "image/jpeg", buffer: Buffer.from([0xff, 0xd8, 0xff, 0xe0]) },
      { name: "html-as.jpg", mimeType: "image/jpeg", buffer: Buffer.from("<html><body>bảng giá</body></html>") },
      { name: "c.jpg", mimeType: "image/jpeg", buffer: Buffer.from([0xff, 0xd8, 0xff, 0xe1]) },
    ]);
    await expect(page.getByText("Đang tải lên (chưa thuộc mẫu)")).toBeVisible();
    await expect(tiles(page)).toHaveCount(2, { timeout: 15_000 });
    await expect(page.getByText("Tệp không phải ảnh")).toBeVisible();
    await expect(page.getByText("html-as.jpg · không được thêm vào mẫu")).toBeVisible();
    await expect(page.getByText("2 ảnh", { exact: true })).toBeVisible();
    expect(fake.product.images.length).toBe(2);
    expect([...fake.files.values()].every((f) => f.put)).toBe(true);
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Bỏ", exact: true }).click();
    await expect(page.getByText("Tệp không phải ảnh")).toHaveCount(0);
    await expect(page.getByText("Đang tải lên (chưa thuộc mẫu)")).toHaveCount(0);
  });

  test("xóa ảnh → thông báo Hoàn tác; bấm Hoàn tác → ảnh về đúng chỗ", async ({ page, api }) => {
    const fake = await open(page, api, mockupImages());
    const second = fake.product.images[1]?.id;
    await page.getByRole("button", { name: "Xóa ảnh 2" }).click();
    await expect(tiles(page)).toHaveCount(2);
    await expect(page.getByText("Đã xóa ảnh 2. Khôi phục được trong 30 ngày.")).toBeVisible();
    const undo = page.getByRole("button", { name: /^Hoàn tác \(\d+\)$/ });
    await expect(undo).toBeInViewport();
    await noHorizontalScroll(page);
    await undo.click();
    await expect(tiles(page)).toHaveCount(3);
    expect(fake.product.images[1]?.id).toBe(second);
    await expect(page.getByRole("combobox", { name: "Màu của ảnh 2" })).toHaveValue("Be");
  });

  test("dời ảnh 2 sang trái → thành ảnh bìa; lưu mẫu sau đó không báo xung đột", async ({ page, api }) => {
    const fake = await open(page, api, mockupImages());
    const second = fake.product.images[1]?.id;
    await page.getByRole("button", { name: "Dời ảnh 2 sang trái" }).click();
    await expect(page.getByRole("combobox", { name: "Màu của ảnh 1" })).toHaveValue("Be");
    expect(fake.product.images[0]?.id).toBe(second);
    await expect(page.getByText("Mẫu đã thay đổi, tải lại.")).toHaveCount(0);
  });
});
