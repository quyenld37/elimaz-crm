// Màn Phiếu kho (kế hoạch M1 Task 328; mockup docs/mockups/m1/kho-phieu.html) ở 1512 × 767 và 390 × 844, API giả có
// trạng thái (e2e/fixtures.ts). Luật thật ở API (Task 29: ghi sổ đổi tồn, thiếu hàng không ghi dòng nào); ở đây kiểm
// màn gửi đúng yêu cầu theo đúng thứ tự, hiện đúng kết quả: phiếu nhập 10 với nhà cung cấp thêm nhanh → ghi sổ → bảng
// tồn có tồn 10; phiếu xuất không có ô nhà cung cấp; phiếu xuất vượt phần giữ → "Không đủ hàng: còn 2", vẫn nháp.
import type { Page, Route } from "@playwright/test";
import type {
  Balance,
  Grant,
  StockDocument,
  StockDocumentLine,
  Supplier,
  VariantSearchHit,
  Warehouse,
} from "../src/api";
import { expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const KHO = "0195f3b2-0000-7000-8000-00000000a001";
const V_DAM = "0195f3b2-0000-7000-8000-00000000b015";
const V_QU = "0195f3b2-0000-7000-8000-00000000b213";
const LONG_NAME = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
const LONG_SUPPLIER =
  "Công ty TNHH Sản xuất Thương mại và Dịch vụ May mặc Xuất khẩu Thanh Bình Hưng Thịnh Phát, chi nhánh Khu công nghiệp Phố Nối A, tỉnh Hưng Yên, xưởng số 3 chuyên hàng lụa và voan cao cấp cho thương hiệu";

const WAREHOUSES: Warehouse[] = [{ id: KHO, code: "MAIN", name: "Kho chính", status: "active" }];
const OWNER: Grant[] = [
  "realtime.subscribe",
  "catalog.read",
  "inventory.read",
  "inventory.documents.create",
  "inventory.documents.post",
  "inventory.suppliers.manage",
].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

const hit = (variantId: string, sku: string, barcode: string, name: string, color: string, size: string) =>
  ({
    variantId,
    productId: variantId.replace("b", "c"),
    sku,
    barcode,
    productCode: sku.split("-")[0] ?? sku,
    productName: name,
    color,
    size,
    price: 450000,
    sellable: true,
  }) satisfies VariantSearchHit;

const HITS: VariantSearchHit[] = [
  hit(V_DAM, "DAM0123-DEN-S", "2000000000015", LONG_NAME, "Đen", "S"),
  hit(V_QU, "QU0789-DEN-M", "2000000000213", "Quần âu ống đứng cạp cao", "Đen", "M"),
];

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const isLine = (v: unknown): v is StockDocumentLine =>
  isRecord(v) && typeof v.variantId === "string" && typeof v.quantity === "number";

interface Store {
  docs: StockDocument[];
  suppliers: Supplier[];
  /** Tồn và phần giữ theo biến thể. */
  stock: Map<string, { onHand: number; held: number }>;
  writes: { method: string; path: string; key: string | null; body: unknown }[];
}

/** API giả có trạng thái: phiếu, nhà cung cấp, số dư; ghi sổ đổi tồn và từ chối khi có thể bán không đủ. */
async function docsApi(api: FakeApi, store: Store) {
  let seq = 0;
  await api.live({ grants: OWNER });
  await api.on("**/api/v1/inventory/warehouses", (r) => json(r, 200, WAREHOUSES));
  const balances = (): Balance[] =>
    HITS.filter((h) => store.stock.has(h.variantId)).map((h) => {
      const s = store.stock.get(h.variantId) ?? { onHand: 0, held: 0 };
      return {
        ...h,
        productStatus: "active",
        variantStatus: "active",
        onHand: s.onHand,
        held: s.held,
        inTransit: 0,
        damaged: 0,
        available: s.onHand - s.held,
      };
    });
  await api.on(/\/api\/v1\/inventory\/balances\?/, (r) => json(r, 200, { items: balances() }));
  await api.on(/\/api\/v1\/catalog\/variants\/search\?/, (r) => {
    const q = (new URL(r.request().url()).searchParams.get("q") ?? "").toUpperCase();
    return json(
      r,
      200,
      HITS.filter((h) => h.sku.startsWith(q) || h.barcode === q),
    );
  });
  const record = (r: Route): Record<string, unknown> => {
    const req = r.request();
    const raw: unknown = req.postData() === null ? null : req.postDataJSON();
    store.writes.push({
      method: req.method(),
      path: new URL(req.url()).pathname,
      key: req.headers()["idempotency-key"] ?? null,
      body: raw,
    });
    return isRecord(raw) ? raw : {};
  };
  await api.on(/\/api\/v1\/inventory\/suppliers(\?|$)/, (r) => {
    if (r.request().method() === "GET") {
      const words = (new URL(r.request().url()).searchParams.get("q") ?? "").toLowerCase().split(/\s+/);
      return json(
        r,
        200,
        store.suppliers.filter((s) => words.every((w) => s.name.toLowerCase().includes(w))),
      );
    }
    const b = record(r);
    const s: Supplier = {
      id: `0195f3b2-0000-7000-8000-0000000f000${String(++seq)}`,
      name: String(b.name),
      phone: "",
      note: "",
      status: "active",
      version: 1,
    };
    store.suppliers.push(s);
    return json(r, 201, s);
  });
  await api.on(/\/api\/v1\/inventory\/documents(\?|$)/, (r) => {
    if (r.request().method() === "GET") {
      return json(r, 200, { items: [...store.docs].reverse().map((d) => ({ ...d, lines: [] })) });
    }
    const b = record(r);
    const sup = store.suppliers.find((s) => s.id === b.supplierId);
    const kind = b.kind === "import" ? "import" : "export";
    const d: StockDocument = {
      id: `0195f3b2-0000-7000-8000-0000000d${String(++seq).padStart(4, "0")}`,
      code: kind === "import" ? "PN-000124" : "PX-000045",
      warehouseId: KHO,
      kind,
      status: "draft",
      supplierName: sup?.name ?? "",
      ...(sup !== undefined && { supplierId: sup.id }),
      note: typeof b.note === "string" ? b.note : "",
      version: 1,
      createdAt: "2026-11-18T08:10:00Z",
      lines: [],
    };
    store.docs.push(d);
    return json(r, 201, d);
  });
  await api.on(/\/api\/v1\/inventory\/documents\/[^/?]+(\/(lines|post|cancel))?$/, (r) => {
    const parts = new URL(r.request().url()).pathname.split("/");
    const action = parts.length > 6 ? parts[6] : undefined;
    const id = parts[5];
    const i = store.docs.findIndex((d) => d.id === id);
    const d = store.docs[i];
    if (d === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    if (action === undefined) return json(r, 200, d);
    const b = record(r);
    if (b.version !== d.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
    let next: StockDocument;
    if (action === "lines") {
      next = { ...d, lines: Array.isArray(b.lines) ? b.lines.filter(isLine) : [], version: d.version + 1 };
    } else if (action === "cancel") {
      next = { ...d, status: "cancelled", version: d.version + 1 };
    } else {
      const sign = d.kind === "import" ? 1 : -1;
      for (const l of d.lines) {
        const s = store.stock.get(l.variantId) ?? { onHand: 0, held: 0 };
        if (sign < 0 && s.onHand - s.held < l.quantity) {
          return json(r, 409, {
            error: "insufficient_stock",
            request_id: "e2e",
            variantId: l.variantId,
            available: Math.max(s.onHand - s.held, 0),
          });
        }
      }
      for (const l of d.lines) {
        const s = store.stock.get(l.variantId) ?? { onHand: 0, held: 0 };
        store.stock.set(l.variantId, { ...s, onHand: s.onHand + sign * l.quantity });
      }
      next = { ...d, status: "posted", postedAt: "2026-11-18T08:15:00Z", version: d.version + 1 };
    }
    store.docs[i] = next;
    return json(r, 200, next);
  });
}

async function scan(page: Page, code: string) {
  const input = page.getByRole("textbox", { name: "Thêm dòng" });
  await input.fill(code);
  await input.press("Enter");
  await expect(input).toHaveValue("");
}

const newStore = (stock: [string, number, number][] = []): Store => ({
  docs: [],
  suppliers: [
    {
      id: "0195f3b2-0000-7000-8000-0000000f0100",
      name: "Xưởng may Hoa Sen",
      phone: "",
      note: "",
      status: "active",
      version: 1,
    },
  ],
  stock: new Map(stock.map(([v, onHand, held]) => [v, { onHand, held }])),
  writes: [],
});

forEachViewport((vp) => {
  test("phiếu nhập 10 cái, thêm nhanh Xưởng Hoa trong ô chọn → ghi sổ → tồn 10, phiếu hiện tên nhà cung cấp", async ({
    page,
    api,
  }) => {
    const store = newStore();
    await docsApi(api, store);
    await page.goto("/kho/phieu");
    await expect(page.getByRole("heading", { name: "Phiếu kho", exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Tạo phiếu" }).click();
    await expect(page.getByRole("heading", { name: "Phiếu nhập mới" })).toBeVisible();

    const combo = page.getByRole("combobox", { name: "Nhà cung cấp" });
    await combo.fill("Xưởng Hoa");
    await expect(page.getByRole("option", { name: "Xưởng may Hoa Sen" })).toBeVisible();
    await page.screenshot({ path: `test-results/kho-phieu-ncc-${String(vp.width)}.png`, fullPage: true });
    await page.getByRole("option", { name: '+ Thêm nhà cung cấp "Xưởng Hoa"' }).click();
    await expect(combo).toHaveValue("Xưởng Hoa");

    await scan(page, "2000000000015");
    const row = page.getByRole("row").filter({ hasText: "DAM0123-DEN-S" });
    await row.getByRole("textbox", { name: "Số lượng" }).fill("10");
    await row.getByRole("textbox", { name: "Giá nhập" }).fill("180.000");
    await expect(row.getByText("1.800.000 ₫")).toBeVisible();
    await expect(row.getByText(LONG_NAME, { exact: false })).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Lưu nháp" }).click();

    await expect(page.getByRole("heading", { name: "Phiếu nhập PN-000124" })).toBeVisible();
    await page.screenshot({ path: `test-results/kho-phieu-soan-${String(vp.width)}.png`, fullPage: true });
    await page.getByRole("button", { name: "Ghi sổ…" }).click();
    const dlg = page.getByRole("dialog", { name: "Ghi sổ phiếu PN-000124?" });
    await expect(dlg).toContainText("Tồn ở Kho chính tăng 10 cái (1 mã). Nhà cung cấp: Xưởng Hoa.");
    await page.screenshot({ path: `test-results/kho-phieu-xac-nhan-${String(vp.width)}.png` });
    await dlg.getByRole("button", { name: "Ghi sổ" }).dblclick();
    await expect(page.locator(".chip", { hasText: "Đã ghi sổ" })).toBeVisible();
    await expect(page.getByText("Xưởng Hoa", { exact: true })).toBeVisible();

    const paths = store.writes.map((w) => `${w.method} ${w.path.replace(/0195[0-9a-f-]+/, ":id")}`);
    expect(paths).toEqual([
      "POST /api/v1/inventory/suppliers",
      "POST /api/v1/inventory/documents",
      "PUT /api/v1/inventory/documents/:id/lines",
      "POST /api/v1/inventory/documents/:id/post",
    ]);
    expect(store.writes.every((w) => w.key !== null)).toBe(true);
    expect(store.writes[1]?.body).toMatchObject({ kind: "import", supplierId: store.suppliers.at(-1)?.id });
    expect(store.writes[2]?.body).toEqual({
      version: 1,
      lines: [{ variantId: V_DAM, quantity: 10, unitCost: 180000 }],
    });

    // Bảng tồn: tồn 10.
    await page.goto("/kho/ton");
    const bal = page.getByRole("row").filter({ hasText: "DAM0123-DEN-S" });
    await expect(bal.locator('td[data-label="Tồn"]')).toHaveText("10");
    // Danh sách: phiếu đã ghi sổ hiện tên nhà cung cấp.
    await page.goto("/kho/phieu");
    const listed = page.getByRole("row").filter({ hasText: "PN-000124" });
    await expect(listed).toContainText("Xưởng Hoa");
    await expect(listed).toContainText("Đã ghi sổ");
  });

  test("phiếu xuất: không có ô nhà cung cấp; vượt phần giữ → Không đủ hàng: còn 2, phiếu vẫn nháp", async ({
    page,
    api,
  }) => {
    const store = newStore([[V_QU, 6, 4]]);
    await docsApi(api, store);
    await page.goto("/kho/phieu/moi");
    await page.getByRole("combobox", { name: "Loại phiếu" }).selectOption("export");
    await expect(page.getByRole("heading", { name: "Phiếu xuất mới" })).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Nhà cung cấp" })).toHaveCount(0);
    await page.getByRole("textbox", { name: "Ghi chú" }).fill("Xuất mẫu cho buổi chụp ảnh");
    await scan(page, "QU0789-DEN-M");
    await scan(page, "QU0789-DEN-M");
    await scan(page, "QU0789-DEN-M");
    const row = page.getByRole("row").filter({ hasText: "QU0789-DEN-M" });
    await expect(row.getByRole("textbox", { name: "Số lượng" })).toHaveValue("3");
    await expect(row.locator('td[data-label="Có thể bán"]')).toHaveText("2");
    await page.getByRole("button", { name: "Lưu nháp" }).click();
    await expect(page.getByRole("heading", { name: "Phiếu xuất PX-000045" })).toBeVisible();
    // Chỉ trong phần màn: menu bên có mục "Nhà cung cấp" (Task 327).
    await expect(page.getByRole("main").getByText("Nhà cung cấp", { exact: true })).toHaveCount(0);

    await page.getByRole("button", { name: "Ghi sổ…" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Ghi sổ" }).click();
    await expect(page.getByText("Không đủ hàng: còn 2.")).toBeVisible();
    await expect(row.getByText("Không đủ hàng: còn 2")).toBeVisible();
    await expect(row).toHaveClass(/is-err/);
    await expect(page.locator(".chip", { hasText: "Nháp" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Lưu nháp" })).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/kho-phieu-xuat-${String(vp.width)}.png`, fullPage: true });
    expect(store.stock.get(V_QU)).toEqual({ onHand: 6, held: 4 });
  });

  test("danh sách: tên nhà cung cấp 200 ký tự không tràn; phiếu đã ghi sổ chỉ xem", async ({ page, api }) => {
    const store = newStore([[V_DAM, 120, 0]]);
    store.docs.push({
      id: "0195f3b2-0000-7000-8000-0000000d0123",
      code: "PN-000123",
      warehouseId: KHO,
      kind: "import",
      status: "posted",
      supplierId: "0195f3b2-0000-7000-8000-0000000f0200",
      supplierName: LONG_SUPPLIER,
      note: "",
      version: 3,
      createdAt: "2026-11-18T02:02:00Z",
      postedAt: "2026-11-18T02:15:00Z",
      lines: [{ variantId: V_DAM, quantity: 120, unitCost: 180000 }],
    });
    await docsApi(api, store);
    await page.goto("/kho/phieu");
    await expect(page.getByRole("row").filter({ hasText: "PN-000123" })).toContainText("Đã ghi sổ");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/kho-phieu-${String(vp.width)}.png`, fullPage: true });
    await page.getByRole("link", { name: "PN-000123" }).click();
    await expect(page.getByRole("heading", { name: "Phiếu nhập PN-000123" })).toBeVisible();
    await expect(page.getByText(LONG_SUPPLIER)).toBeVisible();
    await expect(page.getByText("Phiếu đã ghi sổ không sửa được. Sửa sai bằng phiếu điều chỉnh.")).toBeVisible();
    await expect(page.getByRole("textbox")).toHaveCount(0);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/kho-phieu-da-ghi-${String(vp.width)}.png`, fullPage: true });
  });
});
