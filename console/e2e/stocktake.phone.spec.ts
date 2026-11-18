// Màn Kiểm kho (kế hoạch M1 Task 329; mockup đã duyệt docs/mockups/m1/kho-kiem-kho.html). Đuôi .phone.spec.ts: chạy
// cả WebKit (iPhone, Safari) và Chromium. Khổ chính 390 × 844 (chạm như điện thoại thật), thêm 360 và 1512.
// API giả có trạng thái (e2e/fixtures.ts): phiên, dòng đếm, áp dụng tính đúng như máy chủ (Task 35): trên kệ theo sổ =
// Tồn − Đang giao lúc ghi; chênh = đếm − trên kệ; có thể bán sau = có thể bán + chênh. Luật thật kiểm ở API (Task 35);
// ở đây kiểm màn gửi đúng yêu cầu, hiện đúng số và chữ của mockup.
import type { Page, Route } from "@playwright/test";
import type {
  ApplyStocktakeResult,
  Balance,
  Grant,
  Stocktake,
  StocktakeLine,
  VariantSearchHit,
  Warehouse,
} from "../src/api";
import {
  DESKTOP,
  PHONE,
  expect,
  json,
  noHorizontalScroll,
  test,
  writeRecorder,
  type FakeApi,
  type WriteLog,
} from "./fixtures";

const KHO_CHINH = "0195f3b2-0000-7000-8000-00000000a001";
const KHO_PHU = "0195f3b2-0000-7000-8000-00000000a002";
const ST_ID = "0195f3b2-0000-7000-8000-0000000c0007";
const LONG_NAME = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
const WAREHOUSES: Warehouse[] = [
  { id: KHO_CHINH, code: "MAIN", name: "Kho chính", status: "active" },
  { id: KHO_PHU, code: "PHU", name: "Kho phụ", status: "active" },
];

/** [số, SKU, tên, màu, size, tồn, đang giữ, đang giao] → dòng Tồn kho; có thể bán = tồn − giữ − giao (không có lỗi). */
type Row = [number, string, string, string, string, number, number, number];
const toBalance = ([n, sku, productName, color, size, onHand, held, inTransit]: Row): Balance => ({
  variantId: `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`,
  barcode: `20000000${String(n).padStart(5, "0")}`,
  sku,
  productCode: sku.slice(0, sku.indexOf("-")),
  productName,
  color,
  size,
  productStatus: "active",
  variantStatus: "active",
  onHand,
  held,
  inTransit,
  damaged: 0,
  available: onHand - held - inTransit,
});

// Năm mã của trạng thái B, C trong mockup. KH05: DAM sổ 10, đang giao 3. KH07: QU sổ 5, đang giữ 4.
const DAM = toBalance([15, "DAM0123-DEN-S", LONG_NAME, "Đen", "S", 10, 0, 3]);
const QU = toBalance([213, "QU0789-DEN-M", "Quần âu ống đứng cạp cao", "Đen", "M", 5, 4, 0]);
const SET = toBalance([411, "SET0012-XAM-M", "Set vest blazer và chân váy chữ A", "Xám", "M", 10, 0, 0]);
const AO = toBalance([121, "AO0456-TRANG-S", "Áo sơ mi lụa cổ đức tay dài", "Trắng", "S", 12, 0, 0]);
const VAY = toBalance([101, "VAY0101-BE-M", "Chân váy bút chì xẻ sau", "Be", "M", 9, 0, 0]);
const CATALOG = [DAM, QU, SET, AO, VAY];

const ALL = { type: "all", id: "" } as const;
const OWNER: Grant[] = [
  "realtime.subscribe",
  "catalog.read",
  "inventory.read",
  "inventory.stocktake",
  "inventory.stocktake.apply",
].map((permission) => ({ permission, scope: ALL }));
// Nhân viên kho: đếm được, không áp dụng được (trạng thái F).
const COUNTER: Grant[] = OWNER.filter((g) => g.permission !== "inventory.stocktake.apply");

const hitOf = (b: Balance): VariantSearchHit => ({
  variantId: b.variantId,
  productId: `p-${b.productCode}`,
  sku: b.sku,
  barcode: b.barcode,
  productCode: b.productCode,
  productName: b.productName,
  color: b.color,
  size: b.size,
  price: 450_000,
  sellable: true,
});

/** Máy chủ giả có trạng thái của kiểm kho. */
class FakeStock {
  stocktake: Stocktake | null = null;
  log: WriteLog = { writes: [], failNext: null };
  private readonly rec = writeRecorder(this.log);
  /** Lần mở phiên kế tiếp trả 409 stocktake_open (kho đã có phiên của máy khác). */
  openElsewhere = false;
  /** Chờ trước khi trả lời áp dụng (ms), để thử bấm đúp. */
  applyDelayMs = 0;

  constructor(readonly balances: Balance[]) {}

  open(lines: [Balance, number][] = []): Stocktake {
    this.stocktake = {
      id: ST_ID,
      code: "KK-0007",
      warehouseId: KHO_CHINH,
      status: "open",
      version: 1,
      createdAt: "2026-11-18T08:30:00Z",
      lines: [],
    };
    for (const [b, n] of lines) this.count(b.variantId, n);
    return this.stocktake;
  }

  count(variantId: string, counted: number): StocktakeLine {
    const st = this.stocktake;
    const b = this.balances.find((x) => x.variantId === variantId);
    if (st === null || b === undefined) throw new Error("không có phiên hoặc mã");
    const old = st.lines.find((l) => l.variantId === variantId);
    const expected = b.onHand - b.inTransit;
    const l: StocktakeLine = {
      variantId,
      expectedOnShelf: expected,
      counted,
      delta: counted - expected,
      countedAt: "2026-11-18T09:00:00Z",
      version: (old?.version ?? 0) + 1,
      onHand: b.onHand,
      held: b.held,
      inTransit: b.inTransit,
      damaged: b.damaged,
      available: b.available,
      availableAfter: b.available + counted - expected,
    };
    // Máy chủ trả dòng theo variant_id tăng dần (queries/inventory/stocktakes.sql).
    st.lines = [...st.lines.filter((x) => x.variantId !== variantId), l].sort((a, b) =>
      a.variantId.localeCompare(b.variantId),
    );
    st.version++;
    return l;
  }

  record(r: Route) {
    return this.rec.record(r);
  }

  writesTo(suffix: string) {
    return this.log.writes.filter((w) => w.path.endsWith(suffix));
  }

  async install(api: FakeApi, grants: Grant[] = OWNER) {
    await api.live({ grants });
    await api.on("**/api/v1/inventory/warehouses", (r) => json(r, 200, WAREHOUSES));
    await api.on(/\/api\/v1\/catalog\/variants\/search\?/, (r) => {
      const q = (new URL(r.request().url()).searchParams.get("q") ?? "").toUpperCase();
      const hits = this.balances.filter((b) => b.barcode === q || b.sku.startsWith(q)).map(hitOf);
      return json(r, 200, hits);
    });
    await api.on(/\/api\/v1\/inventory\/balances\?/, (r) => {
      return json(r, 200, { items: this.balances });
    });
    await api.on("**/api/v1/inventory/stocktakes", (r) => {
      this.record(r);
      if (this.openElsewhere) return json(r, 409, { error: "stocktake_open", request_id: "e2e" });
      return json(r, 201, this.open());
    });
    await api.on(/\/api\/v1\/inventory\/stocktakes\/[^/]+$/, (r) => {
      if (this.stocktake === null) return json(r, 404, { error: "not_found", request_id: "e2e" });
      return json(r, 200, this.stocktake);
    });
    await api.on(/\/api\/v1\/inventory\/stocktakes\/[^/]+\/lines\/[^/]+$/, (r) => {
      const body = this.record(r) as { counted: number; lineVersion?: number };
      const variantId = new URL(r.request().url()).pathname.split("/").at(-1) ?? "";
      const old = this.stocktake?.lines.find((l) => l.variantId === variantId);
      if ((old?.version ?? undefined) !== body.lineVersion) {
        return json(r, 409, { error: "version_conflict", request_id: "e2e" });
      }
      return json(r, 200, this.count(variantId, body.counted));
    });
    await api.on(/\/api\/v1\/inventory\/stocktakes\/[^/]+\/apply$/, async (r) => {
      this.record(r);
      await new Promise((res) => setTimeout(res, this.applyDelayMs));
      const st = this.stocktake;
      if (st === null) return json(r, 404, { error: "not_found", request_id: "e2e" });
      if (st.status !== "open") return json(r, 409, { error: "stocktake_not_open", request_id: "e2e" });
      st.status = "applied";
      st.appliedAt = "2026-11-18T09:40:00Z";
      st.version++;
      const changed = st.lines.filter((l) => l.delta !== 0);
      const result: ApplyStocktakeResult = {
        stocktake: st,
        adjusted: changed.length,
        shortages: changed
          .filter((l) => l.availableAfter < 0)
          .map((l) => ({ variantId: l.variantId, available: l.availableAfter })),
      };
      return json(r, 200, result);
    });
    await api.on(/\/api\/v1\/inventory\/stocktakes\/[^/]+\/cancel$/, (r) => {
      this.record(r);
      const st = this.stocktake;
      if (st === null) return json(r, 404, { error: "not_found", request_id: "e2e" });
      st.status = "cancelled";
      st.version++;
      return json(r, 200, st);
    });
  }
}

const SHOTS = process.env.CRM_E2E_SHOTS;
async function shot(page: Page, name: string) {
  if (SHOTS !== undefined) await page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true });
}

/** Quét một mã như máy quét dạng bàn phím: gõ chuỗi rồi Enter vào ô quét. */
async function scan(page: Page, code: string) {
  const box = page.getByLabel("Quét barcode hoặc gõ mã");
  await box.fill(code);
  await box.press("Enter");
}

/** Phiên KK-0007 đang mở ở Kho chính với các dòng đã đếm, rồi mở trang của phiên. */
async function openSession(page: Page, api: FakeApi, lines: [Balance, number][], grants: Grant[] = OWNER) {
  const fake = new FakeStock(CATALOG);
  fake.open(lines);
  await fake.install(api, grants);
  await page.goto(`/kho/kiem-kho/${ST_ID}`);
  return fake;
}

const lineOf = (page: Page, sku: string) => page.locator(".line").filter({ has: page.getByText(sku, { exact: true }) });

test.describe("điện thoại 390", () => {
  test.use({ viewport: PHONE, hasTouch: true });

  test("KH05: mở phiên, quét 2000000000015 bảy lần → đếm 7, lưu một lần; xem trước 'không chỉnh' khi đang giao 3", async ({
    page,
    api,
  }) => {
    const fake = new FakeStock(CATALOG);
    await fake.install(api);
    // Đồng hồ giả: dừng giờ trong lúc quét để kiểm đúng mốc 800 ms, không phụ thuộc tốc độ máy chạy test.
    await page.clock.install();
    await page.goto("/kho/kiem-kho");
    await expect(page.getByRole("heading", { name: "Kiểm kho", exact: true })).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Kho" })).toHaveValue(KHO_CHINH);
    await shot(page, "a-390");
    await page.getByRole("button", { name: "Bắt đầu kiểm Kho chính" }).tap();
    await expect(page).toHaveURL(new RegExp(`/kho/kiem-kho/${ST_ID}$`));
    expect(fake.writesTo("/stocktakes")).toHaveLength(1);
    expect(fake.writesTo("/stocktakes")[0]?.body).toEqual({ warehouseId: KHO_CHINH });
    await expect(page.getByText("KK-0007 · Kho chính")).toBeVisible();

    await page.clock.pauseAt(Date.now() + 2000);
    for (let i = 0; i < 7; i++) await scan(page, "2000000000015");
    const last = page.locator(".last-scan");
    await expect(last.getByText("Vừa quét · 2000000000015")).toBeVisible();
    await expect(last.locator(".count-big")).toHaveText("7");
    await expect(last.getByRole("textbox", { name: "Số đếm" })).toHaveValue("7");
    await expect(last.getByText("Đang lưu…")).toBeVisible();
    await page.clock.runFor(799);
    expect(fake.writesTo(`/lines/${DAM.variantId}`)).toHaveLength(0);
    await page.clock.runFor(1);
    await expect(last.getByText("Đã lưu")).toBeVisible();
    await page.clock.resume();
    const puts = fake.writesTo(`/lines/${DAM.variantId}`);
    expect(puts).toHaveLength(1);
    expect(puts[0]?.method).toBe("PUT");
    expect(puts[0]?.body).toEqual({ counted: 7 });
    expect(puts[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
    await expect(page.getByRole("heading", { name: "Đã đếm (1)" })).toBeVisible();
    await expect(lineOf(page, "DAM0123-DEN-S").getByRole("textbox", { name: "Số đếm DAM0123-DEN-S" })).toHaveValue("7");
    await noHorizontalScroll(page);

    await page.getByRole("button", { name: "Xem chênh lệch" }).tap();
    const row = lineOf(page, "DAM0123-DEN-S");
    await expect(row.getByText("Không chỉnh")).toBeVisible();
    await expect(row.getByText("Tồn 10, đang giao 3")).toBeVisible();
    await expect(row.locator(".diff > span")).toHaveText(["Trên kệ theo sổ7", "Đếm7", "Chênh0"]);
    await expect(page.getByText("sẽ thiếu so với hàng đã giữ")).toHaveCount(0);
  });

  test("KH07: đếm thiếu khi có đơn đang giữ → xem trước 'Thiếu so với phần giữ'; áp dụng xong hiện danh sách thiếu", async ({
    page,
    api,
  }) => {
    const fake = await openSession(page, api, [
      [DAM, 7],
      [SET, 11],
      [AO, 12],
      [VAY, 9],
    ]);
    // Dòng của máy khác: tên đọc từ Tồn kho.
    await expect(page.getByRole("heading", { name: "Đã đếm (4)" })).toBeVisible();
    await expect(lineOf(page, "SET0012-XAM-M").getByText("Set vest blazer và chân váy chữ A · Xám · M")).toBeVisible();
    await scan(page, "2000000000213");
    await scan(page, "QU0789-DEN-M");
    await expect(page.locator(".last-scan .count-big")).toHaveText("2");
    await shot(page, "b-390");

    await page.getByRole("button", { name: "Xem chênh lệch" }).tap();
    await expect.poll(() => fake.writesTo(`/lines/${QU.variantId}`).length).toBe(1);
    const qu = lineOf(page, "QU0789-DEN-M");
    await expect(qu.getByText("Thiếu so với phần giữ")).toBeVisible();
    await expect(qu.locator(".diff > span")).toHaveText(["Trên kệ theo sổ5", "Đếm2", "Chênh−3"]);
    await expect(lineOf(page, "SET0012-XAM-M").getByText("Thừa")).toBeVisible();
    await expect(lineOf(page, "VAY0101-BE-M, AO0456-TRANG-S").getByText("2 mã khớp sổ")).toBeVisible();
    const box = page.locator(".box-soft");
    await expect(box).toContainText("1 mã sẽ thiếu so với hàng đã giữ cho đơn.");
    await expect(box).toContainText("QU0789-DEN-M: đang giữ 4, đếm được 2 → có thể bán −2.");
    await noHorizontalScroll(page);
    await shot(page, "c-390");

    await page.getByRole("button", { name: "Áp dụng…" }).tap();
    const dlg = page.getByRole("dialog", { name: "Áp dụng phiên KK-0007?" });
    await expect(dlg).toContainText("Sửa Tồn của 2 mã: −3 và +1. 3 mã khớp không đổi. 1 mã sẽ thiếu so với phần giữ.");
    await expect(dlg).toContainText("Phiên đã áp dụng không mở lại được. Đếm sai thì mở phiên mới.");
    await shot(page, "d-390");
    await dlg.getByRole("button", { name: "Áp dụng", exact: true }).tap();

    await expect(page.getByText("Đã áp dụng KK-0007.")).toBeVisible();
    await expect(page.locator(".box-ok")).toContainText("2 mã đã chỉnh, 3 mã khớp. Áp dụng lúc 16:40 18/11.");
    const short = page.locator(".box-soft");
    await expect(short.getByText("Thiếu so với phần giữ (1)")).toBeVisible();
    await expect(lineOf(page, "QU0789-DEN-M").getByText("−2")).toBeVisible();
    await expect(short).toContainText("Đã gửi một cảnh báo thường (chỉ ghi mã phiên và số mã thiếu).");
    await expect(page.getByRole("link", { name: "Xem ở Tồn kho" })).toHaveAttribute("href", "/kho/ton");
    const apply = fake.writesTo("/apply");
    expect(apply).toHaveLength(1);
    expect(apply[0]?.body).toEqual({ version: 6 });
    await shot(page, "e-390");
  });

  test("bấm đúp Áp dụng → chỉ một yêu cầu áp dụng", async ({ page, api }) => {
    const fake = await openSession(page, api, [[SET, 11]]);
    fake.applyDelayMs = 400;
    await page.getByRole("button", { name: "Xem chênh lệch" }).tap();
    await page.getByRole("button", { name: "Áp dụng…" }).tap();
    const ok = page.getByRole("dialog").getByRole("button", { name: "Áp dụng", exact: true });
    await ok.dblclick();
    await expect(page.getByText("Đã áp dụng KK-0007.")).toBeVisible();
    expect(fake.writesTo("/apply")).toHaveLength(1);
  });

  test("F: chỉ có quyền đếm → nút Áp dụng không bấm được, có lý do", async ({ page, api }) => {
    await openSession(page, api, [[SET, 11]], COUNTER);
    await page.getByRole("button", { name: "Xem chênh lệch" }).tap();
    await expect(page.getByRole("button", { name: "Áp dụng…" })).toBeDisabled();
    await expect(
      page.getByText("Cần quyền áp dụng kiểm kho (Chủ hoặc Quản lý). Đếm xong thì báo người duyệt vào phiên KK-0007."),
    ).toBeVisible();
    await page.getByRole("button", { name: "Đếm tiếp" }).tap();
    await expect(page.getByLabel("Quét barcode hoặc gõ mã")).toBeVisible();
  });

  test("mã không có trong danh mục → hộp báo; sửa số bằng ô và nút ±", async ({ page, api }) => {
    const fake = await openSession(page, api, []);
    await scan(page, "2000000009999");
    await expect(page.locator(".box-err")).toContainText(
      "Không thấy mã 2000000009999. Mã này không có trong danh mục. Kiểm lại tem, hoặc gõ SKU.",
    );
    await scan(page, "SET0012-XAM-M");
    await expect(page.locator(".box-err")).toHaveCount(0);
    const last = page.locator(".last-scan");
    await last.getByRole("button", { name: "Thêm 1" }).tap();
    await expect(last.locator(".count-big")).toHaveText("2");
    await last.getByRole("textbox", { name: "Số đếm" }).fill("11");
    await expect(last.getByText("Đã lưu")).toBeVisible();
    await expect.poll(() => fake.stocktake?.lines.find((l) => l.variantId === SET.variantId)?.counted).toBe(11);
    await last.getByRole("button", { name: "Bớt 1" }).tap();
    await expect(lineOf(page, "SET0012-XAM-M").getByRole("textbox", { name: "Số đếm SET0012-XAM-M" })).toHaveValue(
      "10",
    );
    await expect.poll(() => fake.stocktake?.lines.find((l) => l.variantId === SET.variantId)?.counted).toBe(10);
  });

  test("A: kho đã có phiên của máy khác → hộp thông tin; phiên máy này đã mở → nút Vào phiên", async ({
    page,
    api,
  }) => {
    const fake = new FakeStock(CATALOG);
    fake.openElsewhere = true;
    await fake.install(api);
    await page.goto("/kho/kiem-kho");
    await page.getByRole("button", { name: "Bắt đầu kiểm Kho chính" }).tap();
    await expect(page.locator(".box-info")).toContainText(
      "Kho chính đang có phiên kiểm chưa xong. Mọi người cùng đếm vào một phiên: nhờ người mở gửi đường dẫn phiên.",
    );

    fake.openElsewhere = false;
    await page.getByRole("button", { name: "Bắt đầu kiểm Kho chính" }).tap();
    await expect(page).toHaveURL(new RegExp(`/kho/kiem-kho/${ST_ID}$`));
    await page.goto("/kho/kiem-kho");
    const info = page.locator(".box-info");
    await expect(info).toContainText(
      "Kho chính đang có phiên KK-0007 mở lúc 15:30 18/11. Mọi người cùng đếm vào một phiên.",
    );
    await shot(page, "a2-390");
    await info.getByRole("button", { name: "Vào phiên KK-0007" }).tap();
    await expect(page).toHaveURL(new RegExp(`/kho/kiem-kho/${ST_ID}$`));
  });

  test("Hủy phiên: xác nhận rồi hủy, gửi version; phiên đã hủy không đếm được nữa", async ({ page, api }) => {
    const fake = await openSession(page, api, [[SET, 11]]);
    await page.getByRole("button", { name: "Hủy phiên" }).tap();
    const dlg = page.getByRole("dialog", { name: "Hủy phiên KK-0007?" });
    await dlg.getByRole("button", { name: "Hủy phiên" }).tap();
    await expect(page.getByText("Phiên KK-0007 đã hủy. Tồn không đổi.")).toBeVisible();
    expect(fake.writesTo("/cancel")[0]?.body).toEqual({ version: 2 });
    await expect(page.getByLabel("Quét barcode hoặc gõ mã")).toHaveCount(0);
  });
});

test.describe("điện thoại hẹp 360", () => {
  test.use({ viewport: { width: 360, height: 740 }, hasTouch: true });

  test("tên dài, nhiều dòng, xem trước: không cuộn ngang", async ({ page, api }) => {
    await openSession(page, api, [
      [QU, 2],
      [SET, 11],
      [AO, 12],
      [VAY, 9],
    ]);
    for (let i = 0; i < 7; i++) await scan(page, "2000000000015");
    await expect(page.locator(".last-scan .count-big")).toHaveText("7");
    await expect(page.locator(".last-scan").getByText(LONG_NAME)).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "b-360");
    await page.getByRole("button", { name: "Xem chênh lệch" }).tap();
    await expect(lineOf(page, "QU0789-DEN-M").getByText("Thiếu so với phần giữ")).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "c-360");
  });
});

test.describe("máy tính 1512", () => {
  test.use({ viewport: DESKTOP });

  test("cùng màn trong cột hẹp; mục Kiểm kho trên menu nhóm Kho", async ({ page, api }) => {
    await openSession(page, api, [[SET, 11]]);
    const link = page.locator("aside.side").getByRole("link", { name: "Kiểm kho" });
    await expect(link).toHaveAttribute("aria-current", "page");
    await scan(page, "2000000000015");
    await expect(page.locator(".last-scan .count-big")).toHaveText("1");
    expect((await page.locator(".phone-col").boundingBox())?.width ?? 0).toBeLessThanOrEqual(640);
    await shot(page, "b-1512");
    await page.getByRole("button", { name: "Xem chênh lệch" }).click();
    await page.getByRole("button", { name: "Áp dụng…" }).click();
    await expect(page.getByRole("dialog", { name: "Áp dụng phiên KK-0007?" })).toBeVisible();
    await shot(page, "d-1512");
  });
});
