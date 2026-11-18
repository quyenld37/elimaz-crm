// Màn Sản phẩm — thêm và sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, C) ở
// 1512 × 767 và 390 × 844, API giả có bộ nhớ (e2e/fixtures.ts + máy chủ giả dưới đây, trả lại kết quả đã lưu khi gửi lại
// cùng Idempotency-Key như máy chủ thật). Kiểm: tạo 2 màu × 3 size → 6 biến thể; SKU trùng → "Mã hàng đã có"; người
// khác vừa lưu → "Mẫu đã thay đổi, tải lại."; mạng rớt lúc lưu → gửi lại cùng khóa, chỉ một mẫu được tạo; tên 80 ký tự
// không tràn. Ảnh chụp trạng thái A lưu vào thư mục kết quả để so với docs/mockups/m1/png/san-pham-sua-{1512,390}.png.
import type { Page, Route } from "@playwright/test";
import type {
  AddVariantRequest,
  Attribute,
  Category,
  CreateProductRequest,
  Product,
  ProductClassification,
  SetClassificationRequest,
  Tag,
  UpdateProductRequest,
  UpdateVariantRequest,
  Variant,
} from "../src/api";
import { expect, forEachViewport, OWNER_GRANTS, test, type FakeApi } from "./fixtures";

const PID = "0195f3b2-0000-7000-8000-000000000123";
const VAY = "0195f3b2-0000-7000-8000-00000000c001";
const VAY_DAI = "0195f3b2-0000-7000-8000-00000000c002";
const MOI = "0195f3b2-0000-7000-8000-00000000d001";
const CONG_SO = "0195f3b2-0000-7000-8000-00000000d002";
const BAN_CHAY = "0195f3b2-0000-7000-8000-00000000d003";
const NAME_80 = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
const DESC =
  "Đầm suông dáng dài, cổ vuông thanh lịch, nút ngọc trai trang trí. Chất lụa mềm, không nhăn, hợp đi làm và dự tiệc nhẹ.";

const CATS: Category[] = [
  { id: VAY, name: "Váy", slug: "vay", depth: 1, position: 1, status: "active", version: 1 },
  { id: VAY_DAI, parentId: VAY, name: "Váy dài", slug: "vay-dai", depth: 2, position: 1, status: "active", version: 1 },
];
const TAGS: Tag[] = [
  { id: MOI, code: "hang-moi", name: "Hàng mới" },
  { id: CONG_SO, code: "cong-so", name: "Công sở" },
  { id: BAN_CHAY, code: "ban-chay", name: "Bán chạy" },
];
const attr = (n: number, name: string, values: string[]): Attribute => {
  const id = `0195f3b2-0000-7000-8000-00000000e${String(n)}00`;
  return {
    id,
    code: `a${String(n)}`,
    name,
    position: n,
    values: values.map((value, i) => ({
      id: `0195f3b2-0000-7000-8000-00000000e${String(n)}${String(i + 1).padStart(2, "0")}`,
      attributeId: id,
      value,
      position: i + 1,
    })),
  };
};
const ATTRS: Attribute[] = [
  attr(1, "Chất liệu", ["Lụa", "Voan"]),
  attr(2, "Kiểu dáng", ["Suông"]),
  attr(3, "Độ dài", ["Qua gối"]),
  attr(4, "Cổ áo", ["Cổ vuông"]),
];

const MANAGER = [...OWNER_GRANTS, { permission: "catalog.manage", scope: { type: "all" as const, id: "" } }];

let seq = 0;
const newId = () => {
  seq++;
  return `0195f3b2-0000-7000-8000-${String(seq).padStart(12, "0")}`;
};

/** Máy chủ giả của mẫu: một mẫu, phân loại, version như catalog thật (mỗi lần sửa biến thể version mẫu +1). */
class FakeCatalog {
  products = new Map<string, Product>();
  classes = new Map<string, ProductClassification>();
  skus = new Set<string>(["AO0456-DEN-L"]);
  /** Khóa chống trùng đã xong → câu trả lời đã lưu. */
  done = new Map<string, { status: number; body: unknown }>();
  keys: { op: string; key: string }[] = [];
  /** Lần tới của op bị mạng rớt (yêu cầu tới máy chủ, máy chủ làm xong, câu trả lời mất). */
  dropReply = new Set<string>();

  seed(p: Product, c: ProductClassification) {
    this.products.set(p.id, p);
    this.classes.set(p.id, c);
    for (const v of p.variants) this.skus.add(v.sku);
  }

  bump(id: string): Product {
    const p = this.products.get(id);
    if (p === undefined) throw new Error("không có mẫu");
    const next = { ...p, version: p.version + 1 };
    this.products.set(id, next);
    const c = this.classes.get(id);
    if (c !== undefined) this.classes.set(id, { ...c, version: next.version });
    return next;
  }
}

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
const err = (route: Route, status: number, code: string) => json(route, status, { error: code, request_id: "e2e" });

function variantFrom(productId: string, v: Omit<AddVariantRequest, "productVersion">, i: number): Variant {
  return {
    id: newId(),
    productId,
    sku: v.sku.toUpperCase(),
    barcode: v.barcode ?? `20000000${String(10000 + i).padStart(5, "0")}`,
    color: v.color,
    size: v.size,
    price: v.price,
    ...(v.costPrice === undefined ? {} : { costPrice: v.costPrice }),
    status: "active",
    version: 1,
  };
}

async function serveCatalog(api: FakeApi, fake: FakeCatalog) {
  await api.on("**/api/v1/catalog/categories", (r) => json(r, 200, CATS));
  await api.on("**/api/v1/catalog/tags", (r) => json(r, 200, TAGS));
  await api.on("**/api/v1/catalog/attributes", (r) => json(r, 200, ATTRS));
  await api.on(/\/api\/v1\/catalog\/(products|variants)(\/.*)?$/, async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const method = req.method();
    const m = /^\/api\/v1\/catalog\/(products|variants)(?:\/([^/]+))?(?:\/(classification|variants))?$/.exec(path);
    if (m === null) return err(route, 404, "not_found");
    const [, kind, id, sub] = m;
    if (method === "GET") {
      if (kind === "products" && id !== undefined && sub === undefined) {
        const p = fake.products.get(id);
        return p === undefined ? err(route, 404, "not_found") : json(route, 200, p);
      }
      if (sub === "classification" && id !== undefined) {
        const c = fake.classes.get(id);
        return c === undefined ? err(route, 404, "not_found") : json(route, 200, c);
      }
      return json(route, 200, { items: [] });
    }
    const op = `${method} ${kind ?? ""}${sub === undefined ? "" : `/${sub}`}`;
    const key = req.headers()["idempotency-key"] ?? "";
    fake.keys.push({ op, key });
    const saved = fake.done.get(key);
    if (saved !== undefined) return json(route, saved.status, saved.body);
    const body: unknown = req.postDataJSON();
    const reply = applyWrite(fake, op, id, body);
    if (reply.status < 300) fake.done.set(key, reply);
    if (fake.dropReply.delete(op)) return route.abort("connectionreset");
    return json(route, reply.status, reply.body);
  });
}

function applyWrite(
  fake: FakeCatalog,
  op: string,
  id: string | undefined,
  body: unknown,
): { status: number; body: unknown } {
  const conflict = { status: 409, body: { error: "version_conflict", request_id: "e2e" } };
  const skuTaken = { status: 409, body: { error: "sku_taken", request_id: "e2e" } };
  switch (op) {
    case "POST products": {
      const b = body as CreateProductRequest;
      if (b.variants.some((v) => fake.skus.has(v.sku.toUpperCase()))) return skuTaken;
      const pid = newId();
      const variants = b.variants.map((v, i) => variantFrom(pid, v, i));
      for (const v of variants) fake.skus.add(v.sku);
      const p: Product = {
        id: pid,
        code: b.code.toUpperCase(),
        name: b.name,
        description: b.description ?? "",
        article: [],
        articleHtml: "",
        status: b.status ?? "draft",
        version: 1,
        variants,
        images: [],
      };
      fake.seed(p, { productId: pid, version: 1, attributeValueIds: [], tagIds: [] });
      return { status: 201, body: p };
    }
    case "PATCH products": {
      const b = body as UpdateProductRequest;
      const p = fake.products.get(id ?? "");
      if (p === undefined) return { status: 404, body: { error: "not_found", request_id: "e2e" } };
      if (p.version !== b.version) return conflict;
      fake.products.set(p.id, {
        ...p,
        ...(b.name === undefined ? {} : { name: b.name }),
        ...(b.description === undefined ? {} : { description: b.description }),
        ...(b.status === undefined ? {} : { status: b.status }),
      });
      return { status: 200, body: fake.bump(p.id) };
    }
    case "PUT products/classification": {
      const b = body as SetClassificationRequest;
      const p = fake.products.get(id ?? "");
      if (p?.version !== b.version) return conflict;
      const next = fake.bump(p.id);
      const c: ProductClassification = {
        productId: p.id,
        version: next.version,
        ...(b.categoryId === null || b.categoryId === undefined ? {} : { categoryId: b.categoryId }),
        attributeValueIds: b.attributeValueIds,
        tagIds: b.tagIds,
      };
      fake.classes.set(p.id, c);
      return { status: 200, body: c };
    }
    case "POST products/variants": {
      const b = body as AddVariantRequest;
      const p = fake.products.get(id ?? "");
      if (p?.version !== b.productVersion) return conflict;
      if (fake.skus.has(b.sku.toUpperCase())) return skuTaken;
      const v = variantFrom(p.id, b, p.variants.length);
      fake.skus.add(v.sku);
      fake.products.set(p.id, { ...p, variants: [...p.variants, v] });
      fake.bump(p.id);
      return { status: 201, body: v };
    }
    case "PATCH variants": {
      const b = body as UpdateVariantRequest;
      for (const p of fake.products.values()) {
        const v = p.variants.find((x) => x.id === id);
        if (v === undefined) continue;
        if (v.version !== b.version) return conflict;
        if (b.sku !== undefined && b.sku.toUpperCase() !== v.sku && fake.skus.has(b.sku.toUpperCase())) return skuTaken;
        const nv: Variant = {
          ...v,
          ...(b.sku === undefined ? {} : { sku: b.sku.toUpperCase() }),
          ...(b.price === undefined ? {} : { price: b.price }),
          ...(b.status === undefined ? {} : { status: b.status }),
          version: v.version + 1,
        };
        fake.products.set(p.id, { ...p, variants: p.variants.map((x) => (x.id === id ? nv : x)) });
        fake.bump(p.id);
        return { status: 200, body: nv };
      }
      return { status: 404, body: { error: "not_found", request_id: "e2e" } };
    }
    default:
      return { status: 501, body: { error: "not_implemented", request_id: "e2e" } };
  }
}

function v(i: number, color: string, size: string, sku: string, extra: Partial<Variant> = {}): Variant {
  return {
    id: `0195f3b2-0000-7000-8000-0000000002${String(i).padStart(2, "0")}`,
    productId: PID,
    sku,
    barcode: `20000000000${String(i).padStart(2, "0")}`,
    color,
    size,
    price: 350000,
    costPrice: 180000,
    status: "active",
    version: 1,
    ...extra,
  };
}

/** Mẫu của trạng thái A trong mockup: tên 80 ký tự, 2 màu × 3 size, Be · L ngừng bán. */
function mockupProduct(): { p: Product; c: ProductClassification } {
  const p: Product = {
    id: PID,
    code: "DAM0123",
    name: NAME_80,
    description: DESC,
    article: [],
    articleHtml: "",
    status: "active",
    version: 12,
    variants: [
      v(1, "Đen", "S", "DAM0123-DEN-S"),
      v(2, "Đen", "M", "DAM0123-DEN-M"),
      v(3, "Đen", "L", "DAM0123-DEN-L", { price: 420000 }),
      v(4, "Be", "S", "DAM0123-BE-S"),
      v(5, "Be", "M", "DAM0123-BE-M"),
      v(6, "Be", "L", "DAM0123-BE-L", { price: 420000, costPrice: 190000, status: "discontinued" }),
    ],
    images: [],
  };
  const c: ProductClassification = {
    productId: PID,
    version: 12,
    categoryId: VAY_DAI,
    attributeValueIds: [ATTRS[0]?.values[0]?.id ?? "", ATTRS[1]?.values[0]?.id ?? "", ATTRS[2]?.values[0]?.id ?? ""],
    tagIds: [MOI, CONG_SO],
  };
  return { p, c };
}

async function noHorizontalScroll(page: Page) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(over, "trang không cuộn ngang").toBeLessThanOrEqual(0);
}

/** Mở trang sửa của mẫu trạng thái A (người có catalog.manage), máy chủ giả đã có mẫu đó. */
async function openMockup(page: Page, api: FakeApi): Promise<{ fake: FakeCatalog; p: Product }> {
  const fake = new FakeCatalog();
  const { p, c } = mockupProduct();
  fake.seed(p, c);
  await api.live({ grants: MANAGER });
  await serveCatalog(api, fake);
  await page.goto(`/san-pham/${PID}`);
  return { fake, p };
}

const grid = (page: Page) => page.locator("table.vtbl");

forEachViewport((vp) => {
  test("tạo mẫu 2 màu × 3 size → 6 biến thể, một createProduct, sang trang sửa", async ({ page, api }) => {
    const fake = new FakeCatalog();
    await api.live({ grants: MANAGER });
    await serveCatalog(api, fake);
    await page.goto("/san-pham/moi");
    await expect(page.getByRole("heading", { name: "Thêm mẫu" })).toBeVisible();
    await page.getByRole("textbox", { name: "Mã mẫu *" }).fill("DAM0200");
    await page.getByRole("textbox", { name: "Tên mẫu *" }).fill(NAME_80);
    for (const c of ["Đen", "Be"]) {
      await page.getByRole("textbox", { name: "Thêm màu" }).fill(c);
      await page.getByRole("textbox", { name: "Thêm màu" }).press("Enter");
    }
    for (const s of ["S", "M", "L"]) {
      await page.getByRole("textbox", { name: "Thêm size" }).fill(s);
      await page.getByRole("textbox", { name: "Thêm size" }).press("Enter");
    }
    await expect(grid(page).locator("tbody tr")).toHaveCount(6);
    await expect(page.getByText("2 màu × 3 size = 6 biến thể")).toBeVisible();
    for (const name of ["Đen · S", "Đen · M", "Đen · L", "Be · S", "Be · M", "Be · L"])
      await page.getByRole("textbox", { name: `Giá bán của ${name}` }).fill("350.000");
    await page.getByRole("combobox", { name: "Thêm nhãn" }).selectOption(MOI);
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Lưu mẫu" }).last().click();
    await expect(page).toHaveURL(/\/san-pham\/0195f3b2-/);
    await expect(page.getByRole("heading", { name: NAME_80 })).toBeVisible();
    await expect(grid(page).locator("tbody tr")).toHaveCount(6);
    await expect(page.getByRole("textbox", { name: "SKU của Be · L" })).toHaveValue("DAM0200-BE-L");
    expect(fake.products.size).toBe(1);
    expect(fake.keys.filter((k) => k.op === "POST products").length).toBe(1);
    expect([...fake.classes.values()][0]?.tagIds).toEqual([MOI]);
    await noHorizontalScroll(page);
  });

  test("mạng rớt lúc lưu → gửi lại cùng khóa, chỉ một mẫu được tạo", async ({ page, api }) => {
    const fake = new FakeCatalog();
    fake.dropReply.add("POST products");
    await api.live({ grants: MANAGER });
    await serveCatalog(api, fake);
    await page.goto("/san-pham/moi");
    await page.getByRole("textbox", { name: "Mã mẫu *" }).fill("DAM0300");
    await page.getByRole("textbox", { name: "Tên mẫu *" }).fill("Đầm thử mạng");
    await page.getByRole("textbox", { name: "Thêm màu" }).fill("Đen");
    await page.getByRole("textbox", { name: "Thêm màu" }).press("Enter");
    await page.getByRole("textbox", { name: "Thêm size" }).fill("S");
    await page.getByRole("textbox", { name: "Thêm size" }).press("Enter");
    await page.getByRole("textbox", { name: "Giá bán của Đen · S" }).fill("350000");
    await page.getByRole("button", { name: "Lưu mẫu" }).last().click();
    await expect(page).toHaveURL(/\/san-pham\/0195f3b2-/);
    const posts = fake.keys.filter((k) => k.op === "POST products");
    expect(posts.length).toBe(2);
    expect(posts[0]?.key).toBe(posts[1]?.key);
    expect(fake.products.size).toBe(1);
  });

  test("trạng thái A: tên 80 ký tự không tràn, 6 biến thể, phân loại đang gán; ảnh chụp so mockup", async ({
    page,
    api,
  }, info) => {
    await openMockup(page, api);
    const h1 = page.getByRole("heading", { name: NAME_80 });
    await expect(h1).toBeVisible();
    const box = await h1.boundingBox();
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(vp.width);
    await expect(page.getByRole("combobox", { name: "Danh mục" })).toHaveValue(VAY_DAI);
    await expect(page.getByRole("button", { name: "Bỏ nhãn Công sở" })).toBeVisible();
    await expect(grid(page).locator("tbody tr")).toHaveCount(6);
    await expect(grid(page).locator("tbody tr.is-muted")).toHaveCount(1);
    await expect(page.getByRole("textbox", { name: "Giá bán của Đen · L" })).toHaveValue("420.000");
    // Màu, size đã có biến thể lưu: không có nút bỏ.
    await expect(page.getByRole("button", { name: "Bỏ màu Đen" })).toHaveCount(0);
    await noHorizontalScroll(page);
    if (vp.width < 1000) {
      await expect(page.getByRole("button", { name: "Lưu mẫu" })).toHaveCount(1);
      await expect(page.getByRole("button", { name: "Hủy thay đổi" })).toBeInViewport();
    } else {
      await expect(page.getByRole("button", { name: "Lưu mẫu" }).first()).toBeInViewport();
    }
    await page.screenshot({ path: info.outputPath(`trang-thai-a-${String(vp.width)}.png`), fullPage: true });
  });

  test("SKU trùng khi sửa → Mã hàng đã có ở đúng dòng; sửa lại thì lưu được", async ({ page, api }) => {
    const { fake } = await openMockup(page, api);
    const sku = page.getByRole("textbox", { name: "SKU của Đen · L" });
    await sku.fill("AO0456-DEN-L");
    await page.getByRole("button", { name: "Lưu mẫu" }).last().click();
    const row = grid(page).locator("tbody tr", { hasText: "Đen · L" });
    await expect(row.getByText("Mã hàng đã có")).toBeVisible();
    await expect(row).toHaveClass(/is-err/);
    await expect(sku).toHaveClass(/is-err/);
    await noHorizontalScroll(page);
    await sku.fill("DAM0123-DEN-L2");
    await page.getByRole("button", { name: "Lưu mẫu" }).last().click();
    await expect(row.getByText("Mã hàng đã có")).toHaveCount(0);
    await expect(sku).toHaveValue("DAM0123-DEN-L2");
    expect(fake.products.get(PID)?.variants.find((x) => x.color === "Đen" && x.size === "L")?.sku).toBe(
      "DAM0123-DEN-L2",
    );
  });

  test("người khác vừa lưu → Mẫu đã thay đổi, tải lại; Tải lại mẫu hiện bản mới", async ({ page, api }) => {
    const { fake, p } = await openMockup(page, api);
    await expect(page.getByRole("heading", { name: NAME_80 })).toBeVisible();
    // Người khác lưu tên mới (version 13).
    fake.products.set(PID, { ...p, name: "Đầm suông bản người khác vừa lưu" });
    fake.bump(PID);
    await page.getByRole("textbox", { name: "Tên mẫu *" }).fill("Tên của tôi");
    await page.getByRole("button", { name: "Lưu mẫu" }).last().click();
    await expect(page.getByText("Mẫu đã thay đổi, tải lại.")).toBeVisible();
    await expect(page.getByText("Có người vừa lưu mẫu này. Thay đổi của bạn chưa được lưu.")).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Tải lại mẫu" }).click();
    await expect(page.getByRole("heading", { name: "Đầm suông bản người khác vừa lưu" })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Tên mẫu *" })).toHaveValue("Đầm suông bản người khác vừa lưu");
    expect(fake.products.get(PID)?.name).toBe("Đầm suông bản người khác vừa lưu");
  });
});
