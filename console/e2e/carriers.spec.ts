// Màn Kết nối vận chuyển, trang sửa tài khoản, Cài đặt vận chuyển (kế hoạch M3 Task 132; mockup
// docs/mockups/m3/ket-noi-van-chuyen.html) ở 1512 × 767, 1280 × 800 và 390 × 844, API giả. Hành vi kiểm ở test vitest
// của src/screens/carriers; ở đây kiểm bố cục (không cuộn ngang, tên 60 ký tự nằm trong ô, ô nhập trong khung) và chụp
// test-results/ket-noi-*.png để so với docs/mockups/m3/png/ket-noi-van-chuyen-*.png.
import type { Page } from "@playwright/test";
import type { CarrierAccount, FulfillmentSettings, WebCarrierSettings, WebCarrierStat } from "../src/api";
import { OWNER_GRANTS, expect, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000c${String(n).padStart(4, "0")}`;
const WH = id(9001);
const WH2 = id(9002);
const LONG = "BestExpressTaiKhoanPhuChoDonSiCuaKhoChinhChiNhanhMienBacSo02";
const GRANTS = [
  ...OWNER_GRANTS,
  ...[
    "fulfillment.read",
    "fulfillment.carrier_accounts.read",
    "fulfillment.carrier_accounts.manage",
    "fulfillment.settings.manage",
    "customer.read",
  ].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

const acc = (n: number, p: Partial<CarrierAccount>): CarrierAccount => ({
  id: id(n),
  carrierKey: "vtp",
  mode: "api",
  environment: "production",
  name: `Tài khoản ${String(n)}`,
  status: "active",
  warehouseId: WH,
  isDefaultForWarehouse: false,
  showOnWeb: false,
  webSort: 0,
  hasCredentials: true,
  needsRewrap: false,
  version: 3,
  lastCheckAt: "2026-11-21T02:12:00Z",
  lastCheckResult: "ok",
  ...p,
});

// Chín tài khoản của trạng thái A (đủ sáu trạng thái, tên 60 ký tự, thử, nhập tay, tự giao).
const ACCOUNTS: CarrierAccount[] = [
  acc(1, {
    name: "Viettel Post — Kho chính",
    isDefaultForWarehouse: true,
    showOnWeb: true,
    webSort: 1,
    webhookUrl: "https://crm.elimaz.com.vn/hooks/vtp/q7Hk2mP0sVx9LrT4bNc8WdYe1Za6Uf3G",
    needsRewrap: true,
    capabilities: {
      quote: true,
      create: true,
      cancel: true,
      track: true,
      webhook: true,
      labels: true,
      labelsBatch: true,
      updateAddress: true,
      redeliver: false,
      locations: true,
      statement: true,
    },
  }),
  acc(2, { carrierKey: "best", name: "Best Express — Kho chính", showOnWeb: true, webSort: 2 }),
  acc(3, {
    carrierKey: "best",
    name: LONG,
    status: "auth_failed",
    lastCheckAt: "2026-11-21T01:00:00Z",
    lastCheckResult: "best_unauthorized",
  }),
  acc(4, {
    carrierKey: "best",
    name: "Best Express — Kho Hà Nội",
    status: "paused",
    warehouseId: WH2,
    isDefaultForWarehouse: true,
    lastCheckAt: "2026-11-10T08:40:00Z",
  }),
  acc(5, { carrierKey: "spx", name: "SPX — Kho chính", status: "revoked", lastCheckAt: "2026-11-02T02:00:00Z" }),
  acc(6, { carrierKey: "spx", name: "SPX — nhập mã tay", mode: "manual", hasCredentials: false }),
  acc(7, { carrierKey: "self", name: "Shop tự giao (nội thành)", mode: "manual", showOnWeb: true, webSort: 3 }),
  acc(8, { carrierKey: "spx", name: "SPX — thử nghiệm", environment: "sandbox", status: "checking" }),
  acc(9, {
    name: "Viettel Post — Kho Hà Nội",
    status: "draft",
    hasCredentials: false,
    warehouseId: WH2,
    lastCheckAt: undefined,
    lastCheckResult: undefined,
  }),
];

const SETTINGS: FulfillmentSettings = {
  stuckHours: { awaiting_pickup: 48, shipping: 72, delivery_failed: 24, returning: 168, cancel_requested: 24 },
  pollMinutes: { awaiting_pickup: 120, shipping: 60, delivery_failed: 30, returning: 360, cancel_requested: 15 },
  callbackDeadlineHours: 24,
  defaultWeightGramsPerItem: 300,
  quoteValidMinutes: 30,
  returnRisk: { watchMinReturned: 1, highMinReturned: 2, highRateBp: 3000 },
  codOverdueDays: 14,
  agedDays: { held: 20, inTransit: 180 },
  version: 0,
};

// Cài đặt hãng trên website (khối "Hãng trên website" của danh sách, Task 198/488): carriers[] là các tài khoản
// showOnWeb của ACCOUNTS theo webSort, hiddenCarriers là phần còn lại (trừ revoked), xếp theo tên như máy chủ dựng.
const stat = (a: CarrierAccount, shipments30d: number): WebCarrierStat => ({
  accountId: a.id,
  carrierKey: a.carrierKey,
  name: a.name,
  status: a.status,
  webSort: a.webSort,
  preferred: false,
  shipments30d,
});
const WEB_SETTINGS: WebCarrierSettings = {
  version: 2,
  carriers: ACCOUNTS.filter((a) => a.showOnWeb && a.status !== "revoked")
    .sort((a, b) => a.webSort - b.webSort)
    .map((a, i) => stat(a, 40 - i * 10)),
  hiddenCarriers: ACCOUNTS.filter((a) => !a.showOnWeb && a.status !== "revoked")
    .sort((a, b) => a.name.localeCompare(b.name, "vi"))
    .map((a) => stat(a, 0)),
};

async function carriersApi(api: FakeApi) {
  await api.live({ grants: GRANTS });
  await api.on("**/api/v1/inventory/warehouses", (r) =>
    json(r, 200, [
      { id: WH, code: "MAIN", name: "Kho chính", status: "active" },
      { id: WH2, code: "HN", name: "Kho Hà Nội", status: "active" },
    ]),
  );
  await api.on(/\/api\/v1\/carrier-accounts(\?|$)/, (r) => json(r, 200, { items: ACCOUNTS }));
  await api.on(/\/api\/v1\/carrier-locations\/unmapped/, (r) =>
    json(r, 200, {
      items: [
        { level: "ward", ourCode: "26734", name: "Phường Vĩnh Hưng", parentCode: "01" },
        { level: "ward", ourCode: "31120", name: "Xã Đảo Thạnh An", parentCode: "79" },
        { level: "ward", ourCode: "00412", name: "Đặc khu Bạch Long Vĩ", parentCode: "31" },
      ],
    }),
  );
  await api.on(/\/api\/v1\/admin-units/, (r) =>
    json(r, 200, {
      items: [
        { code: "01", name: "Hà Nội", level: "province" },
        { code: "31", name: "Hải Phòng", level: "province" },
        { code: "79", name: "TP Hồ Chí Minh", level: "province" },
      ],
    }),
  );
  await api.on(/\/api\/v1\/inventory\/warehouses\/[^/]+\/pickup/, (r) =>
    json(r, 200, {
      warehouseId: WH,
      version: 4,
      name: "Kho ELIMAZ",
      phone: "+84987654321",
      line: "18 ngõ 120 Trường Chinh",
      provinceCode: "01",
      wardCode: "00070",
    }),
  );
  await api.on("**/api/v1/fulfillment-settings", (r) => json(r, 200, SETTINGS));
  await api.on("**/api/v1/web-carrier-settings", (r) => json(r, 200, WEB_SETTINGS));
}

/** Mọi ô nhập, nút nằm trong khung nhìn ngang; tên dài không tràn khỏi ô. */
async function inside(page: Page) {
  const bad = await page.evaluate(() => {
    const w = window.innerWidth;
    return [...document.querySelectorAll<HTMLElement>("main input, main select, main button, main .clamp2")]
      .filter((el) => el.offsetParent !== null)
      .filter((el) => {
        const r = el.getBoundingClientRect();
        return r.left < -1 || r.right > w + 1 || (el.matches(".clamp2") && el.scrollWidth > el.clientWidth + 1);
      })
      .map((el) => el.outerHTML.slice(0, 80));
  });
  expect(bad).toEqual([]);
}

for (const vp of [
  { width: 1512, height: 767 },
  { width: 1280, height: 800 },
  { width: 390, height: 844 },
]) {
  test.describe(`${String(vp.width)} × ${String(vp.height)}`, () => {
    test.use({ viewport: vp });

    test("danh sách 9 tài khoản, hộp thêm: không tràn", async ({ page, api }) => {
      await carriersApi(api);
      await page.goto("/settings/carriers");
      // Tên dài có cả ở bảng tài khoản và ở khối "Hãng trên website" (hiddenCarriers): kiểm ở bảng.
      await expect(page.getByRole("table", { name: "Tài khoản hãng" }).getByText(LONG)).toBeVisible();
      if (vp.width >= 1000) {
        const nav = page.getByRole("navigation", { name: "Menu chính" });
        await expect(nav.getByRole("link", { name: "Kết nối vận chuyển" })).toHaveAttribute("aria-current", "page");
      }
      await noHorizontalScroll(page);
      await inside(page);
      await page.screenshot({ path: `test-results/ket-noi-danh-sach-${String(vp.width)}.png`, fullPage: true });
      await page.getByRole("button", { name: "+ Thêm tài khoản" }).click();
      await page.getByRole("button", { name: "Nối API của hãng" }).click();
      await expect(page.getByLabel("Mật khẩu API")).toHaveAttribute("type", "password");
      await page.screenshot({ path: `test-results/ket-noi-them-${String(vp.width)}.png`, fullPage: true });
    });

    test("trang sửa tài khoản: không tràn", async ({ page, api }) => {
      await carriersApi(api);
      await page.goto(`/settings/carriers/${id(1)}`);
      await expect(page.getByLabel("URL webhook")).toBeVisible();
      await expect(page.getByText("3 địa giới chưa khớp")).toBeVisible();
      await expect(page.getByLabel("Người giao hàng cho hãng *")).toHaveValue("Kho ELIMAZ");
      await noHorizontalScroll(page);
      await inside(page);
      await page.screenshot({ path: `test-results/ket-noi-sua-${String(vp.width)}.png`, fullPage: true });
    });

    test("cài đặt vận chuyển, lỗi 0%: không tràn", async ({ page, api }) => {
      await carriersApi(api);
      await page.goto("/settings/fulfillment");
      await page.getByLabel("và tỷ lệ hoàn từ (%)").fill("0");
      await expect(page.getByText("Nhập từ 1% đến 100%")).toBeVisible();
      await noHorizontalScroll(page);
      await inside(page);
      await page.screenshot({ path: `test-results/ket-noi-cai-dat-${String(vp.width)}.png`, fullPage: true });
    });
  });
}
