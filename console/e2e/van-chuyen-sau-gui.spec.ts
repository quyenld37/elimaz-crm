// Khối Vận chuyển sau khi gửi hãng (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng thái D, E,
// F, G) ở 1512 × 767 và 390 × 844, API giả. Hành vi (hủy, đổi địa chỉ, cập nhật tay, COD, dòng thời gian) kiểm ở vitest
// src/screens/shipping; ở đây kiểm bố cục không tràn (tên khách 60 ký tự, mã vận đơn dài, 12 sự kiện, tiền 9 chữ số) và
// chụp test-results/van-chuyen-sau-gui-*.png để so với docs/mockups/m3/png/don-van-chuyen-{1512,390}.png.
import type { Page } from "@playwright/test";
import type { Order, Shipment, ShipmentEvent } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000f${String(n).padStart(4, "0")}`;
const WH = id(1);
const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

const all = (permission: string) => ({ permission, scope: { type: "all" as const, id: "" } });
const atWh = (permission: string) => ({ permission, scope: { type: "warehouse" as const, id: WH } });
const GRANTS = [
  ...OWNER_GRANTS,
  ...["order.read", "order.cancel", "customer.read", "cash.cod.read"].map(all),
  ...[
    "fulfillment.read",
    "fulfillment.ship",
    "fulfillment.cancel",
    "fulfillment.manual_status",
    "fulfillment.print",
  ].map(atWh),
];

// Khóa xếp khác van-chuyen.spec.ts (đơn đã gửi hãng, dữ liệu riêng của Task 134).
function order(p: Partial<Order> = {}): Order {
  const money = { subtotal: 2_130_000, shippingFee: 30_000, prepaidTotal: 200_000 };
  return {
    ...money,
    grandTotal: money.subtotal + money.shippingFee,
    codAmount: money.subtotal + money.shippingFee - money.prepaidTotal,
    lineDiscountTotal: 0,
    orderDiscount: 0,
    couponDiscount: 0,
    discountTotal: 0,
    id: id(10),
    code: "DH0001240",
    status: "confirmed",
    label: "shipping",
    version: 6,
    channel: "manual",
    createdAt: "2026-11-21T03:20:00Z",
    warehouseId: WH,
    currentShipmentId: id(70),
    customerId: id(20),
    customer: { id: id(20), rootId: id(20), code: "KH000231", fullName: LONG_NAME },
    addressComplete: true,
    shipAddress: {
      provinceCode: "01",
      provinceName: "Hà Nội",
      wardCode: "00166",
      wardName: "Phường Trung Hòa",
      line: "12 ngõ 45 Trần Duy Hưng",
      name: "Trần Thị Thu Hà",
      phone: "+84912345678",
    },
    items: [
      {
        sku: "DAM0201-BE-L",
        name: "Đầm lụa be dáng suông",
        qty: 3,
        unitPrice: 710_000,
        netAmount: 2_130_000,
        id: id(31),
        variantId: id(41),
        position: 1,
      },
    ],
    returns: [],
    ...p,
  };
}

function shipment(p: Partial<Shipment> = {}): Shipment {
  return {
    id: id(70),
    orderId: id(10),
    code: "E261121K7Q2MX",
    carrierKey: "vtp",
    carrierAccountId: id(61),
    accountName: "Viettel Post — Kho chính",
    mode: "api",
    status: "shipping",
    createState: "created",
    label: "Đang chuyển",
    trackingCode: "1234567890123",
    service: "Chuyển phát nhanh",
    fee: 32_000,
    weightGrams: 900,
    codAmount: 1_960_000,
    createdAt: "2026-11-21T03:42:00Z",
    lastCarrierEventAt: "2026-11-22T07:20:00Z",
    lastSyncedAt: "2026-11-22T07:21:00Z",
    version: 3,
    ...p,
  };
}

/** 12 sự kiện của mockup D: nguồn Hãng / Nhân viên / Hệ thống, hai sự kiện bị bỏ qua. */
const EVENTS: ShipmentEvent[] = [
  ["2026-11-22T07:20:00Z", "Đang chuyển", "carrier_api", ""],
  ["2026-11-22T07:05:00Z", "Đang chuyển", "carrier_api", "duplicate_state"],
  ["2026-11-22T01:30:00Z", "Đến bưu cục phát Cầu Giấy", "carrier_api", ""],
  ["2026-11-22T00:58:00Z", "Chờ thu gom", "carrier_api", "older"],
  ["2026-11-21T15:10:00Z", "Rời bưu cục gửi", "carrier_api", ""],
  ["2026-11-21T10:45:00Z", "Hãng đã lấy hàng → Đang chuyển", "carrier_api", ""],
  ["2026-11-21T09:00:00Z", "Đã đóng gói", "staff", ""],
  ["2026-11-21T08:12:00Z", "In nhãn", "staff", ""],
  ["2026-11-21T04:05:00Z", "Đổi địa chỉ: hãng đã nhận", "carrier_api", ""],
  ["2026-11-21T03:58:00Z", "Yêu cầu đổi địa chỉ", "staff", ""],
  ["2026-11-21T03:43:00Z", "Hãng đã tạo vận đơn · Chờ thu gom", "system", ""],
  ["2026-11-21T03:42:00Z", "Gửi hãng", "staff", ""],
].map(([at, label, source, ignored], i) => ({
  id: id(100 + i),
  occurredAt: at ?? "",
  recordedAt: at ?? "",
  source: source === "staff" ? "staff" : source === "system" ? "system" : "carrier_api",
  rawStatus: `S${String(i)}`,
  label: label ?? "",
  applied: ignored === "",
  ...(ignored === "" ? {} : { ignoredReason: ignored ?? "" }),
}));

const SETTINGS = {
  version: 1,
  stuckHours: { shipping: 72 },
  pollMinutes: {},
  callbackDeadlineHours: 24,
  codOverdueDays: 14,
  agedDays: { held: 20, inTransit: 180 },
  quoteValidMinutes: 30,
  defaultWeightGramsPerItem: 300,
  returnRisk: { highRateBp: 3000, highMinReturned: 2, watchMinReturned: 1 },
};
const ORDER_SETTINGS = {
  draftTtlMinutes: 1440,
  duplicateWindowMinutes: 1440,
  discountApprovalThresholdBp: 1000,
  returnWarningDays: 7,
  version: 1,
};

async function shippedApi(api: FakeApi, o: Order, s: Shipment, cod = { toCollect: 1_960_000, carrierHolding: 0 }) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/orders\/[^/]+\/timeline$/, (r) => json(r, 200, { items: [] }));
  await api.on(/\/api\/v1\/orders\/[^/?]+$/, (r) => json(r, 200, o));
  await api.on(/\/api\/v1\/orders\/[^/]+\/shipments/, (r) =>
    json(r, 200, {
      items: [
        s,
        shipment({
          id: id(71),
          code: "E261121P3D8RA",
          carrierKey: "best",
          accountName: "Best Express — Kho chính",
          status: "cancelled",
          cancelKind: "replace",
          lastStatusAt: "2026-11-21T03:30:00Z",
        }),
      ],
    }),
  );
  await api.on(/\/api\/v1\/shipments\/[^/?]+$/, (r) => json(r, 200, s));
  await api.on(/\/api\/v1\/shipments\/[^/]+\/events/, (r) => json(r, 200, { items: EVENTS }));
  await api.on(/\/api\/v1\/shipments\/[^/]+\/address-changes/, (r) => json(r, 200, { items: [] }));
  await api.on(/\/api\/v1\/shipments\/[^/]+\/cod/, (r) =>
    json(r, 200, { balances: { ...cod, shopReceived: 0, carrierFee: 0, salesClearing: 0 }, transfers: [] }),
  );
  await api.on("**/api/v1/fulfillment-settings", (r) => json(r, 200, SETTINGS));
  await api.on(/\/api\/v1\/customers\/[^/]+\/delivery-stats/, (r) =>
    json(r, 200, { delivered: 9, returned: 1, returnRateBp: 1000, risk: "watch" }),
  );
  await api.on("**/api/v1/traffic-sources", (r) => json(r, 200, { items: [] }));
  await api.on("**/api/v1/admin-units*", (r) =>
    json(r, 200, { items: [{ code: "01", name: "Hà Nội", level: "province" }] }),
  );
  await api.on("**/api/v1/order-settings", (r) => json(r, 200, ORDER_SETTINGS));
}

/** Không cuộn ngang; mọi con của các thẻ names nằm trọn trong thẻ (không chữ tràn); rồi chụp test-results/<file>.png. */
async function checkAndShot(page: Page, names: readonly string[], file: string) {
  await noHorizontalScroll(page);
  for (const name of names) {
    const spill = await page
      .getByRole("region", { name, exact: true })
      .evaluate((card) =>
        Array.from(card.querySelectorAll("*")).some(
          (child) => child.getBoundingClientRect().right - card.getBoundingClientRect().right > 1,
        ),
      );
    expect(spill, `tràn trong thẻ ${name}`).toBe(false);
  }
  await page.screenshot({ path: `test-results/${file}.png`, fullPage: true });
}

const CARDS = ["Vận chuyển", "Tiền thu hộ (COD)", "Dòng thời gian vận đơn", "Vận đơn của đơn này"];

forEachViewport((vp) => {
  test("D: đã gửi, đang chuyển — mã, cập nhật lần cuối, bốn nút, COD bốn ngăn, 12 sự kiện có nguồn", async ({
    page,
    api,
  }) => {
    await shippedApi(api, order(), shipment());
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển", exact: true });
    await expect(panel).toContainText("Cập nhật lần cuối lúc 14:20 22/11 (hãng) · CRM hỏi lúc 14:21");
    await expect(panel.getByRole("button", { name: "Hủy / đổi hãng" })).toBeDisabled();
    await expect(page.getByRole("list", { name: "Sự kiện vận đơn" }).getByRole("listitem")).toHaveCount(12);
    await expect(page.getByRole("region", { name: "Tiền thu hộ (COD)" })).toContainText("1.960.000 ₫");
    await expect(page.getByText("Nhận 9 · Hoàn 1 (10%)")).toBeVisible();
    await checkAndShot(page, CARDS, `van-chuyen-sau-gui-d-${String(vp.width)}`);
  });

  test("E: kẹt 80 giờ, hãng đang giữ 112.450.000 ₫; đang yêu cầu hủy", async ({ page, api }) => {
    await page.clock.setFixedTime(new Date("2026-11-22T07:00:00Z"));
    await shippedApi(
      api,
      order(),
      shipment({
        carrierKey: "best",
        accountName: "Best Express — Kho chính",
        trackingCode: "BE0098812736VN",
        lastCarrierEventAt: "2026-11-18T23:00:00Z",
        stuckSince: "2026-11-21T23:00:00Z",
        lastSyncError: "timeout",
        codAmount: 112_450_000,
      }),
      { toCollect: 0, carrierHolding: 112_450_000 },
    );
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển", exact: true });
    await expect(panel).toContainText("Hãng chưa cập nhật 80 giờ (ngưỡng 72 giờ)");
    await expect(page.getByRole("region", { name: "Tiền thu hộ (COD)" })).toContainText("112.450.000 ₫");
    await checkAndShot(page, CARDS, `van-chuyen-sau-gui-e-${String(vp.width)}`);
  });

  test("E: đang yêu cầu hủy — băng chờ hãng, nút hủy mờ", async ({ page, api }) => {
    await shippedApi(
      api,
      order({ status: "cancel_requested" }),
      shipment({ status: "cancel_requested", label: "Đang yêu cầu hủy", trackingCode: "1234567890999" }),
    );
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển", exact: true });
    await expect(panel).toContainText("Đang chờ hãng xác nhận hủy — hàng chưa được trả về kho.");
    await expect(panel.getByRole("button", { name: "Hủy / đổi hãng" })).toBeDisabled();
    await checkAndShot(page, ["Vận chuyển"], `van-chuyen-sau-gui-e2-${String(vp.width)}`);
  });

  test("F: hộp Hủy / đổi hãng và hộp Đổi địa chỉ", async ({ page, api }) => {
    await shippedApi(api, order(), shipment({ status: "awaiting_pickup", label: "Chờ thu gom" }));
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển", exact: true });
    await panel.getByRole("button", { name: "Hủy / đổi hãng" }).click();
    const cancel = page.getByRole("dialog", { name: "Hủy vận đơn 1234567890123" });
    await cancel.getByLabel("Lý do *").fill("Viettel Post báo không lấy hàng được hôm nay");
    await expect(cancel.getByRole("button", { name: "Gửi yêu cầu hủy" })).toBeEnabled();
    await checkAndShot(page, [], `van-chuyen-sau-gui-f-huy-${String(vp.width)}`);
    await cancel.getByRole("button", { name: "Đóng" }).first().click();
    await panel.getByRole("button", { name: "Đổi địa chỉ" }).click();
    const addr = page.getByRole("dialog", { name: "Đổi địa chỉ giao" });
    await expect(addr).toContainText("Đang giao tới: 12 ngõ 45 Trần Duy Hưng, Phường Trung Hòa, Hà Nội");
    await addr.getByLabel("Địa chỉ mới *").fill("Tầng 5, 89 Láng Hạ");
    await checkAndShot(page, [], `van-chuyen-sau-gui-f-dia-chi-${String(vp.width)}`);
  });

  test("G: vận đơn nhập mã tay — Cập nhật trạng thái đòi lý do", async ({ page, api }) => {
    await shippedApi(
      api,
      order(),
      shipment({
        mode: "manual",
        carrierKey: "ghn",
        accountName: "GHN — nhập mã tay",
        status: "awaiting_pickup",
        label: "Chờ thu gom",
        trackingCode: "GHN5KX92MQ",
        service: "",
      }),
    );
    await page.goto(`/don/${id(10)}`);
    const panel = page.getByRole("region", { name: "Vận chuyển", exact: true });
    await expect(panel).toContainText("Hãng không báo trạng thái cho CRM. Cập nhật tay khi biết.");
    await panel.getByRole("button", { name: "Cập nhật trạng thái" }).click();
    const d = page.getByRole("dialog", { name: "Cập nhật trạng thái" });
    await expect(d.getByRole("button", { name: "Lưu" })).toBeDisabled();
    await d.getByLabel("Lý do *").fill("Shipper GHN báo qua Zalo");
    await expect(d.getByRole("button", { name: "Lưu" })).toBeEnabled();
    await checkAndShot(page, [], `van-chuyen-sau-gui-g-${String(vp.width)}`);
  });
});
