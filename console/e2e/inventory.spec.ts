// Màn Tồn kho (kế hoạch M1 Task 51; mockup docs/mockups/m1/kho-ton.html) ở 1512 × 767 và 390 × 844, API giả
// (e2e/fixtures.ts). Luật thật ở API (Task 334: phạm vi kho, lọc thiếu); ở đây kiểm màn gửi đúng yêu cầu, hiện đúng
// bốn ngăn và có thể bán, dòng âm ghi rõ bằng chữ, ô chọn kho chỉ có kho API trả, trạng thái trống.
import type { Page, Route } from "@playwright/test";
import type { Balance, BalancePage, Grant, Warehouse } from "../src/api";
import { expect, forEachViewport, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const KHO_CHINH = "0195f3b2-0000-7000-8000-00000000a001";
const KHO_PHU = "0195f3b2-0000-7000-8000-00000000a002";
const LONG_NAME = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";

const MAIN: Warehouse = { id: KHO_CHINH, code: "MAIN", name: "Kho chính", status: "active" };
const WAREHOUSES: Warehouse[] = [MAIN, { id: KHO_PHU, code: "PHU", name: "Kho phụ", status: "active" }];

function bal(n: number, sku: string, name: string, color: string, size: string, s: Partial<Balance>): Balance {
  const b: Balance = {
    variantId: `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`,
    sku,
    barcode: `20000000${String(n).padStart(5, "0")}`,
    productCode: sku.split("-")[0] ?? sku,
    productName: name,
    color,
    size,
    productStatus: "active",
    variantStatus: "active",
    onHand: 0,
    held: 0,
    inTransit: 0,
    damaged: 0,
    available: 0,
    ...s,
  };
  b.available = b.onHand - b.damaged - b.held - b.inTransit;
  return b;
}

// Bảy dòng của trạng thái A trong mockup.
const ROWS: Balance[] = [
  bal(15, "DAM0123-DEN-S", LONG_NAME, "Đen", "S", { onHand: 10, damaged: 1, held: 2, inTransit: 3 }),
  bal(213, "QU0789-DEN-M", "Quần âu ống đứng cạp cao", "Đen", "M", { onHand: 2, held: 4 }),
  bal(305, "AO0321-HONG-M", "Áo kiểu cổ nơ tay phồng", "Hồng phấn", "M", { onHand: 1, held: 1, inTransit: 1 }),
  bal(411, "SET0012-XAM-M", "Set vest blazer và chân váy chữ A", "Xám", "M", { onHand: 10, damaged: 2 }),
  bal(121, "AO0456-TRANG-S", "Áo sơ mi lụa cổ đức tay dài", "Trắng", "S", { onHand: 12 }),
  bal(503, "VAY0101-DEN-S", "Chân váy bút chì xẻ sau", "Đen", "S", {}),
  bal(602, "DAM0055-HOA-M", "Đầm xòe hoa nhí thu đông 2025", "Hoa nhí", "M", {
    onHand: 3,
    productStatus: "discontinued",
  }),
];

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

interface Fake {
  /** Tham số của mọi lần gọi listBalances. */
  calls: URLSearchParams[];
}

/** API giả: listWarehouses trả `warehouses`; listBalances lọc theo kho, onlyShort, q, trang 7 dòng một lần khi `pages`. */
async function stockApi(
  api: FakeApi,
  opts: { grants?: Grant[]; warehouses?: Warehouse[]; byWarehouse?: Record<string, Balance[]>; pageSize?: number } = {},
): Promise<Fake> {
  const fake: Fake = { calls: [] };
  await api.live(opts.grants === undefined ? {} : { grants: opts.grants });
  await api.on("**/api/v1/inventory/warehouses", (r) => json(r, 200, opts.warehouses ?? WAREHOUSES));
  const data = opts.byWarehouse ?? { [KHO_CHINH]: ROWS, [KHO_PHU]: [] };
  await api.on(/\/api\/v1\/inventory\/balances\?/, (r) => {
    const p = new URL(r.request().url()).searchParams;
    fake.calls.push(p);
    const wh = p.get("warehouseId") ?? "";
    if (!(wh in data)) return json(r, 403, { error: "forbidden", request_id: "e2e" });
    let items = data[wh] ?? [];
    if (p.get("onlyShort") === "true") items = items.filter((b) => b.available < 0);
    const q = (p.get("q") ?? "").toUpperCase();
    if (q !== "") items = items.filter((b) => b.sku.startsWith(q) || b.barcode === q);
    const size = opts.pageSize ?? Number(p.get("limit") ?? "50");
    const start = Number(p.get("cursor") ?? "0");
    const page: BalancePage = { items: items.slice(start, start + size) };
    if (start + size < items.length) page.nextCursor = String(start + size);
    return json(r, 200, page);
  });
  return fake;
}

const rowOf = (page: Page, sku: string) => page.getByRole("row").filter({ has: page.getByText(sku, { exact: false }) });

forEachViewport((vp) => {
  test("bảng tồn bốn ngăn và có thể bán; dòng âm ghi rõ bằng chữ; ngừng bán, hết hàng", async ({ page, api }) => {
    const fake = await stockApi(api);
    await page.goto("/kho/ton");
    await expect(page.getByRole("heading", { name: "Tồn kho", exact: true })).toBeVisible();
    await expect(page.getByRole("combobox", { name: "Kho" })).toHaveValue(KHO_CHINH);
    if (vp.width >= 1000) {
      const link = page.locator("aside.side").getByRole("link", { name: "Tồn kho" });
      await expect(link).toHaveAttribute("aria-current", "page");
    }
    const first = rowOf(page, "DAM0123-DEN-S");
    await expect(first.getByText("DAM0123-DEN-S · 2000000000015")).toBeVisible();
    await expect(first.getByText(`${LONG_NAME} · Đen · S`)).toBeVisible();
    for (const [label, v] of [
      ["Tồn", "10"],
      ["Lỗi", "1"],
      ["Đang giữ", "2"],
      ["Đang giao", "3"],
      ["Có thể bán", "4"],
    ] as const) {
      await expect(first.locator(`td[data-label="${label}"]`)).toHaveText(v);
    }
    const short = rowOf(page, "QU0789-DEN-M");
    await expect(short).toHaveClass(/is-err/);
    await expect(short.locator('td[data-label="Có thể bán"]')).toContainText("−2");
    await expect(short.getByText("Thiếu 2 so với phần giữ")).toBeVisible();
    await expect(rowOf(page, "AO0321-HONG-M").getByText("Thiếu 1 so với phần giữ")).toBeVisible();
    await expect(rowOf(page, "VAY0101-DEN-S").getByText("Hết hàng")).toBeVisible();
    const off = rowOf(page, "DAM0055-HOA-M");
    await expect(off).toHaveClass(/is-muted/);
    await expect(off.getByText("Ngừng bán")).toBeVisible();
    await expect(page.getByText("Đang hiện cả 7 mã hàng ở Kho chính.")).toBeVisible();
    // Tên dài không tràn: tối đa hai dòng, không cuộn ngang.
    const name = first.locator(".clamp2");
    const lh = await name.evaluate((el) => parseFloat(getComputedStyle(el).lineHeight));
    expect((await name.boundingBox())?.height ?? 0).toBeLessThanOrEqual(lh * 2 + 1);
    await noHorizontalScroll(page);
    expect(fake.calls[0]?.get("warehouseId")).toBe(KHO_CHINH);
    expect(fake.calls[0]?.get("onlyShort")).toBeNull();
    await page.screenshot({ path: `test-results/kho-ton-${String(vp.width)}.png`, fullPage: true });
  });

  test('lọc "Thiếu so với phần giữ" gửi onlyShort=true, có hộp giải thích; Tắt lọc khi không còn dòng thiếu', async ({
    page,
    api,
  }) => {
    const fake = await stockApi(api, { byWarehouse: { [KHO_CHINH]: ROWS, [KHO_PHU]: ROWS.slice(3) } });
    await page.goto("/kho/ton");
    await expect(rowOf(page, "DAM0123-DEN-S")).toBeVisible();
    const toggle = page.getByRole("button", { name: /Thiếu so với phần giữ/ });
    await expect(toggle).toHaveAttribute("aria-pressed", "false");
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByText("Các mã dưới đang thiếu so với hàng đã giữ cho đơn.")).toBeVisible();
    await expect(page.locator("tbody tr")).toHaveCount(2);
    expect(fake.calls.at(-1)?.get("onlyShort")).toBe("true");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/kho-ton-thieu-${String(vp.width)}.png`, fullPage: true });

    // Kho phụ không có dòng thiếu → trạng thái C, Tắt lọc về danh sách đủ.
    await page.getByRole("combobox", { name: "Kho" }).selectOption(KHO_PHU);
    await expect(page.getByRole("heading", { name: "Không có mã hàng nào thiếu so với phần giữ" })).toBeVisible();
    expect(fake.calls.at(-1)?.get("warehouseId")).toBe(KHO_PHU);
    await page.getByRole("button", { name: "Tắt lọc" }).click();
    await expect(page.locator("tbody tr")).toHaveCount(4);
    expect(fake.calls.at(-1)?.get("onlyShort")).toBeNull();
  });

  test("nhân viên phạm vi kho chính không thấy kho phụ, không gọi tồn kho phụ", async ({ page, api }) => {
    const fake = await stockApi(api, {
      grants: [
        { permission: "inventory.read", scope: { type: "warehouse", id: KHO_CHINH } },
        { permission: "realtime.subscribe", scope: { type: "all", id: "" } },
      ],
      warehouses: [MAIN],
      byWarehouse: { [KHO_CHINH]: ROWS },
    });
    await page.goto("/kho/ton");
    await expect(rowOf(page, "DAM0123-DEN-S")).toBeVisible();
    const select = page.getByRole("combobox", { name: "Kho" });
    await expect(select.locator("option")).toHaveText(["Kho chính"]);
    expect(fake.calls.every((p) => p.get("warehouseId") === KHO_CHINH)).toBe(true);
  });

  test("kho chưa có hàng; tìm theo SKU; tải thêm theo con trỏ", async ({ page, api }) => {
    const many = Array.from({ length: 60 }, (_, i) =>
      bal(1000 + i, `AO${String(i).padStart(4, "0")}-DEN-S`, "Áo thun", "Đen", "S", { onHand: 5 }),
    );
    const fake = await stockApi(api, { byWarehouse: { [KHO_CHINH]: many, [KHO_PHU]: [] } });
    await page.goto("/kho/ton");
    await expect(page.locator("tbody tr")).toHaveCount(50);
    await expect(page.getByText("Đang hiện 50 mã hàng đầu ở Kho chính.")).toBeVisible();
    await page.getByRole("button", { name: "Tải thêm 50" }).click();
    await expect(page.locator("tbody tr")).toHaveCount(60);
    expect(fake.calls.at(-1)?.get("cursor")).toBe("50");
    await expect(page.getByRole("button", { name: "Tải thêm 50" })).toHaveCount(0);

    await page.getByRole("searchbox", { name: "Tìm mã hàng" }).fill("ao0007");
    await expect(page.locator("tbody tr")).toHaveCount(1);
    expect(fake.calls.at(-1)?.get("q")).toBe("ao0007");

    await page.getByRole("searchbox", { name: "Tìm mã hàng" }).fill("");
    await page.getByRole("combobox", { name: "Kho" }).selectOption(KHO_PHU);
    await expect(page.getByRole("heading", { name: "Kho phụ chưa có hàng" })).toBeVisible();
    await expect(page.getByText("Tồn kho tăng khi ghi sổ phiếu nhập.")).toBeVisible();
    await noHorizontalScroll(page);
  });
});
