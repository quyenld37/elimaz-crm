// Khối Vận chuyển trên chi tiết đơn (kế hoạch M3 Task 133; mockup docs/mockups/m3/don-van-chuyen.html trạng thái A, C,
// G) ở 1512 × 767 và 390 × 844, API giả. Hành vi (so phí, gửi một lần, quote_stale, không rõ) kiểm ở vitest
// src/screens/shipping; ở đây kiểm bố cục không tràn (tên tài khoản 60 ký tự, 5 dòng so phí) và chụp
// test-results/van-chuyen-*.png để so với docs/mockups/m3/png/don-van-chuyen-{1512,390}.png.
import type { Page } from "@playwright/test";
import type { CarrierAccount, Order, Shipment, ShippingQuote } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000e${String(n).padStart(4, "0")}`;
const WH = id(1);
const LONG_ACCOUNT = "BestExpressTaiKhoanPhuChoDonSiCuaKhoChinhChiNhanhMienBacSo02";

const GRANTS = [
  ...OWNER_GRANTS,
  ...["order.read", "order.update", "order.confirm", "order.cancel", "customer.read"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
  ...["fulfillment.read", "fulfillment.quote", "fulfillment.ship", "fulfillment.carrier_accounts.read"].map(
    (permission) => ({ permission, scope: { type: "warehouse" as const, id: WH } }),
  ),
];

function order(p: Partial<Order> = {}): Order {
  return {
    id: id(10),
    code: "DH0001240",
    status: "confirmed",
    label: "confirmed",
    version: 4,
    customerId: id(20),
    customer: { id: id(20), rootId: id(20), code: "KH000877", fullName: "Trần Thị Thu Hà" },
    warehouseId: WH,
    subtotal: 2_130_000,
    lineDiscountTotal: 0,
    orderDiscount: 0,
    couponDiscount: 0,
    discountTotal: 0,
    shippingFee: 30_000,
    grandTotal: 2_160_000,
    prepaidTotal: 200_000,
    codAmount: 1_960_000,
    createdAt: "2026-11-21T03:20:00Z",
    channel: "manual",
    shipAddress: {
      name: "Trần Thị Thu Hà",
      phone: "+84912345678",
      line: "12 ngõ 45 Trần Duy Hưng",
      provinceCode: "01",
      provinceName: "Hà Nội",
      wardCode: "00166",
      wardName: "Phường Trung Hòa",
    },
    addressComplete: true,
    carrierNote: "Gọi trước khi giao, giao giờ hành chính",
    items: [
      {
        id: id(31),
        position: 1,
        variantId: id(41),
        sku: "DAM0201-BE-L",
        name: "Đầm lụa be dáng suông",
        qty: 2,
        unitPrice: 990_000,
        netAmount: 1_980_000,
      },
      {
        id: id(32),
        position: 2,
        variantId: id(42),
        sku: "AO0101-DEN-M",
        name: "Áo thun cổ tròn basic",
        qty: 1,
        unitPrice: 150_000,
        netAmount: 150_000,
      },
    ],
    editScope: {
      lines: true,
      discounts: true,
      coupon: true,
      address: true,
      shipping: true,
      carrierNote: true,
      careBy: true,
      trafficSource: true,
    },
    returns: [],
    ...p,
  };
}

const valid = () => new Date(Date.now() + 25 * 60_000).toISOString();
const QUOTES = (): ShippingQuote[] => [
  {
    quoteId: id(51),
    carrierAccountId: id(61),
    accountName: "Viettel Post — Kho chính",
    carrierKey: "vtp",
    status: "ok",
    service: "VCN",
    serviceName: "Chuyển phát nhanh",
    feeTotal: 32_000,
    expectedDays: 2,
    validUntil: valid(),
  },
  { carrierAccountId: id(62), accountName: "Best Express — Kho chính", carrierKey: "best", status: "pending" },
  { carrierAccountId: id(63), accountName: LONG_ACCOUNT, carrierKey: "best", status: "error", errorCode: "auth" },
];

function acct(n: number, name: string, carrierKey: string): CarrierAccount {
  return {
    id: id(n),
    carrierKey,
    mode: "manual",
    environment: "production",
    name,
    status: "active",
    warehouseId: WH,
    isDefaultForWarehouse: false,
    showOnWeb: false,
    webSort: 0,
    hasCredentials: false,
    needsRewrap: false,
    version: 1,
  };
}
const ACCOUNTS = [acct(64, "GHN — nhập mã tay", "ghn"), acct(65, "Shop tự giao (nội thành)", "self")];

function shipment(p: Partial<Shipment>): Shipment {
  return {
    id: id(70),
    orderId: id(10),
    code: "E261121K7Q2MX",
    carrierKey: "vtp",
    carrierAccountId: id(61),
    accountName: "Viettel Post — Kho chính",
    status: "creating",
    createState: "unknown",
    codAmount: 1_960_000,
    createdAt: "2026-11-21T03:42:00Z",
    ...p,
  };
}

async function shipApi(api: FakeApi, o: Order) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/orders\/[^/]+\/timeline$/, (r) => json(r, 200, { items: [] }));
  await api.on(/\/api\/v1\/orders\/[^/?]+$/, (r) => json(r, 200, o));
  await api.on(/\/api\/v1\/orders\/[^/]+\/shipping-quotes/, (r) =>
    json(r, 200, { requestId: id(80), items: QUOTES() }),
  );
  await api.on(/\/api\/v1\/orders\/[^/]+\/shipments/, (r) => json(r, 200, { items: [] }));
  await api.on(/\/api\/v1\/shipments\/[^/?]+$/, (r) => json(r, 200, shipment({})));
  // Dòng thời gian vận đơn ở cột trái (Task 134).
  await api.on(/\/api\/v1\/shipments\/[^/]+\/events/, (r) => json(r, 200, { items: [] }));
  await api.on(/\/api\/v1\/carrier-accounts/, (r) => json(r, 200, { items: ACCOUNTS }));
  await api.on("**/api/v1/fulfillment-settings", (r) =>
    json(r, 200, {
      stuckHours: {},
      callbackDeadlineHours: 24,
      pollMinutes: {},
      defaultWeightGramsPerItem: 300,
      quoteValidMinutes: 30,
      returnRisk: { watchMinReturned: 1, highMinReturned: 2, highRateBp: 3000 },
      codOverdueDays: 14,
      agedDays: { held: 20, inTransit: 180 },
      version: 1,
    }),
  );
  await api.on(/\/api\/v1\/customers\/[^/]+\/delivery-stats/, (r) =>
    json(r, 200, {
      delivered: 3,
      returned: 2,
      returnRateBp: 4000,
      risk: "high",
      hint: "Nên xin cọc hoặc gọi xác nhận trước khi gửi.",
    }),
  );
  await api.on("**/api/v1/traffic-sources", (r) => json(r, 200, { items: [] }));
  await api.on("**/api/v1/order-settings", (r) =>
    json(r, 200, {
      version: 1,
      returnWarningDays: 7,
      discountApprovalThresholdBp: 1000,
      duplicateWindowMinutes: 1440,
      draftTtlMinutes: 1440,
    }),
  );
}

async function shot(page: Page, name: string) {
  await noHorizontalScroll(page);
  await page.screenshot({ path: `test-results/${name}.png`, fullPage: true });
}

/** Mọi con của khối nằm trọn trong khối (không chữ tràn ra ngoài thẻ). */
async function noOverflow(page: Page) {
  const out = await page.getByRole("region", { name: "Vận chuyển" }).evaluate((el) => {
    const box = el.getBoundingClientRect();
    return [...el.querySelectorAll("*")].filter((c) => c.getBoundingClientRect().right > box.right + 1).length;
  });
  expect(out).toBe(0);
}

forEachViewport((vp) => {
  test("A: so phí 5 tài khoản (giá, đang hỏi, cần cấp quyền lại, nhập tay, tự giao), khách hay hoàn", async ({
    page,
    api,
  }) => {
    await shipApi(api, order());
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển" });
    await expect(panel.getByRole("button", { name: "Gửi hãng · Viettel Post · 32.000 ₫" })).toBeEnabled();
    await expect(panel.getByRole("radio")).toHaveCount(5);
    await expect(panel).toContainText("Khách hay hoàn");
    await expect(panel).toContainText("Tài khoản cần cấp quyền lại");
    await noOverflow(page);
    await shot(page, `van-chuyen-a-${String(vp.width)}`);
  });

  test("G: chọn tài khoản nhập mã tay → ô dán mã", async ({ page, api }) => {
    await shipApi(api, order());
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển" });
    await panel.getByRole("radio", { name: /GHN — nhập mã tay/ }).check();
    await expect(panel.getByRole("button", { name: "Lưu mã vận đơn" })).toBeDisabled();
    await panel.getByLabel(/Mã vận đơn của GHN/).fill("GHN5KX92MQ");
    await expect(panel.getByRole("button", { name: "Lưu mã vận đơn" })).toBeEnabled();
    await noOverflow(page);
    await shot(page, `van-chuyen-g-${String(vp.width)}`);
  });

  test("C: không rõ hãng đã tạo chưa — hai nút, không Gửi lại", async ({ page, api }) => {
    await shipApi(api, order({ currentShipmentId: id(70), label: "sending" }));
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển" });
    await expect(panel.getByRole("button", { name: "Hãng đã tạo" })).toBeDisabled();
    await expect(panel.getByRole("button", { name: "Gửi lại" })).toHaveCount(0);
    await noOverflow(page);
    await shot(page, `van-chuyen-c-${String(vp.width)}`);
  });
});
