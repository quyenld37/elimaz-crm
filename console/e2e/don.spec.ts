// Màn Đơn (kế hoạch M2 Task M2-28; mockup docs/mockups/m2/don-danh-sach.html, don-chi-tiet.html, don-trung.html) ở
// 1512 × 767, 390 × 844 và 360 (tên công ty dài), API giả. Hành vi (báo hiệu, version, trả hàng) kiểm ở các test
// vitest của src/screens/don; ở đây kiểm bố cục không tràn và chụp test-results/don-*.png để so với
// docs/mockups/m2/png/don-{danh-sach,chi-tiet,trung}-{1512,390,360}.png.
import type { Page } from "@playwright/test";
import type { Order, OrderDuplicate, OrderItem, OrderListItem, OrderSettings, Return, TimelineItem } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000f${String(n).padStart(4, "0")}`;
const COMPANY = "Công ty TNHH Thương mại Dịch vụ Thời trang Hoàng Gia Minh Châu";
const LONG =
  "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa thủ công tay phồng nhẹ dáng xòe xếp ly nhiều tầng màu xanh ngọc bích Thu 26";

const GRANTS = [
  ...OWNER_GRANTS,
  ...[
    "order.create",
    "order.read",
    "order.update",
    "order.confirm",
    "order.cancel",
    "order.discount.approve",
    "order.return",
    "order.return.receive",
    "order.duplicates.resolve",
    "customer.read",
  ].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

const SRC = { zalo: id(61), phone: id(62), live: id(63), msg: id(64) };
const SOURCE_NAMES: [string, string][] = [
  [SRC.zalo, "Zalo"],
  [SRC.phone, "Điện thoại"],
  [SRC.live, "Live tối"],
  [SRC.msg, "Messenger"],
];
const SETTINGS: OrderSettings = {
  version: 1,
  returnWarningDays: 7,
  discountApprovalThresholdBp: 1000,
  duplicateWindowMinutes: 1440,
  draftTtlMinutes: 1440,
};
const LE = { id: id(71), name: "Lê Văn Đức" };
const TRANG = { id: id(72), name: "Phạm Thu Trang" };

function row(p: Partial<OrderListItem> & Pick<OrderListItem, "id" | "code">): OrderListItem {
  return {
    label: "new",
    status: "new",
    customerId: id(80),
    customerName: "Trần Thị Thu Hà",
    shipPhone: "+84987654321",
    grandTotal: 1_800_000,
    codAmount: 1_800_000,
    itemCount: 4,
    trafficSourceId: SRC.phone,
    careBy: TRANG,
    createdAt: "2026-11-18T07:31:00Z",
    duplicateSuspect: false,
    ...p,
  };
}

const ROWS: OrderListItem[] = [
  row({ id: id(1), code: "DH0001236", duplicateSuspect: true }),
  row({
    id: id(2),
    code: "DH0001235",
    customerName: COMPANY,
    shipPhone: "+84903111222",
    label: "awaiting_discount_approval",
    pendingReason: "discount_approval",
    grandTotal: 11_060_000,
    codAmount: 11_060_000,
    itemCount: 64,
    trafficSourceId: SRC.zalo,
    careBy: LE,
    createdAt: "2026-11-18T06:05:00Z",
  }),
  row({
    id: id(3),
    code: "DH0001198",
    shipPhone: "+84912345678",
    grandTotal: 1_330_000,
    codAmount: 1_330_000,
    itemCount: 2,
    trafficSourceId: SRC.live,
    careBy: undefined,
    createdAt: "2026-11-18T02:12:00Z",
    duplicateSuspect: true,
  }),
  row({
    id: id(4),
    code: "DH0001190",
    customerName: "Ngô Bảo Anh",
    shipPhone: "+84388000999",
    label: "awaiting_customer",
    pendingReason: "awaiting_customer",
    grandTotal: 450_000,
    codAmount: 450_000,
    itemCount: 1,
    trafficSourceId: SRC.msg,
    createdAt: "2026-11-17T15:47:00Z",
  }),
  row({
    id: id(5),
    code: "DH0001187",
    customerName: "Vũ Minh",
    shipPhone: "+84977123456",
    label: "out_of_stock",
    pendingReason: "out_of_stock",
    grandTotal: 720_000,
    codAmount: 720_000,
    itemCount: 2,
    trafficSourceId: SRC.live,
    careBy: LE,
    createdAt: "2026-11-17T14:30:00Z",
  }),
];

const at = (i: number): OrderListItem => {
  const r = ROWS[i];
  if (r === undefined) throw new Error(`thiếu dòng ${String(i)}`);
  return r;
};
const D1150 = row({
  id: id(11),
  code: "DH0001150",
  customerName: "Nguyễn Hoàng Phương Thảo Nguyên",
  label: "confirmed",
  status: "confirmed",
  grandTotal: 430_000,
  codAmount: 430_000,
  createdAt: "2026-11-17T14:02:00Z",
  trafficSourceId: SRC.live,
});
const D1151 = row({
  id: id(12),
  code: "DH0001151",
  customerId: id(82),
  customerName: "Thảo Nguyên (khách mới)",
  grandTotal: 430_000,
  codAmount: 430_000,
  createdAt: "2026-11-17T14:14:00Z",
  trafficSourceId: SRC.live,
});

const item = (n: number, sku: string, name: string, qty: number, price: number, disc: number): OrderItem => ({
  id: id(100 + n),
  position: n,
  variantId: id(200 + n),
  sku,
  name,
  qty,
  unitPrice: price,
  lineDiscount: disc,
  netAmount: qty * price - disc,
});

const ALL_SCOPE = {
  lines: true,
  discounts: true,
  coupon: true,
  address: true,
  shipping: true,
  carrierNote: true,
  careBy: true,
  trafficSource: true,
};

const DETAIL: Order = {
  id: id(2),
  code: "DH0001235",
  status: "new",
  label: "awaiting_discount_approval",
  pendingReason: "discount_approval",
  version: 4,
  customerId: id(81),
  customer: {
    id: id(81),
    rootId: id(81),
    code: "KH000877",
    fullName: COMPANY,
    stats: {
      orderCount: 23,
      deliveredCount: 21,
      returnedCount: 1,
      totalSpent: 96_400_000,
      lastOrderAt: "2026-11-18T06:05:00Z",
      returnRateBp: 500,
    },
  },
  items: [
    item(1, "AO0101-DEN-M", "Áo thun cổ tròn basic", 20, 150_000, 300_000),
    item(2, "AO0101-TRANG-M", "Áo thun cổ tròn basic", 20, 150_000, 300_000),
    item(3, "QU0220-DEN-27", "Quần âu ống suông", 10, 320_000, 400_000),
    item(4, "DAM0123-BE-M", LONG, 4, 690_000, 0),
    item(5, "KHAN0012-HOA", "Khăn lụa vuông họa tiết", 10, 120_000, 0),
  ],
  subtotal: 13_160_000,
  lineDiscountTotal: 1_000_000,
  orderDiscount: 1_100_000,
  couponDiscount: 0,
  discountTotal: 2_100_000,
  shippingFee: 0,
  grandTotal: 11_060_000,
  prepaidTotal: 0,
  codAmount: 11_060_000,
  createdAt: "2026-11-18T06:05:00Z",
  createdBy: LE,
  careBy: LE,
  channel: "manual",
  trafficSourceId: SRC.zalo,
  shipAddress: {
    name: "Chị Châu",
    phone: "+84903111222",
    line: "Tầng 3, 45 Lê Lợi",
    provinceCode: "79",
    provinceName: "TP. Hồ Chí Minh",
    wardCode: "26734",
    wardName: "Phường Bến Thành",
  },
  addressComplete: true,
  carrierNote: "Hàng sỉ, kiểm số lượng khi nhận.",
  editScope: ALL_SCOPE,
  returns: [],
};

const DELIVERED: Order = {
  ...DETAIL,
  id: id(9),
  code: "DH0001102",
  status: "delivered",
  label: "delivered",
  pendingReason: undefined,
  orderDiscount: 0,
  discountTotal: 0,
  lineDiscountTotal: 0,
  items: [item(1, "DAM0123-BE-M", LONG, 3, 620_000, 0), item(2, "TUI0090-NAU", "Túi hộp da mini", 1, 290_000, 0)],
  subtotal: 2_150_000,
  grandTotal: 2_150_000,
  codAmount: 2_150_000,
  editScope: {
    ...ALL_SCOPE,
    lines: false,
    discounts: false,
    coupon: false,
    address: false,
    shipping: false,
    carrierNote: false,
    trafficSource: false,
  },
  returns: [
    { id: id(301), code: "TH0000012", kind: "customer_return", status: "requested" },
    { id: id(302), code: "TH0000009", kind: "exchange", status: "restocked" },
  ],
};

const RETURNS: Return[] = [
  {
    id: id(301),
    code: "TH0000012",
    orderId: DELIVERED.id,
    kind: "customer_return",
    status: "requested",
    refundAmount: 1_240_000,
    items: [{ id: id(311), orderItemId: id(101), qty: 2, refundAmount: 1_240_000 }],
    createdAt: "2026-11-18T03:00:00Z",
    version: 1,
  },
  {
    id: id(302),
    code: "TH0000009",
    orderId: DELIVERED.id,
    kind: "exchange",
    status: "restocked",
    refundAmount: 290_000,
    replacementOrderId: id(10),
    items: [{ id: id(312), orderItemId: id(102), qty: 1, refundAmount: 290_000, condition: "sellable" }],
    createdAt: "2026-11-15T03:00:00Z",
    version: 2,
  },
];

const TIMELINE: TimelineItem[] = [
  {
    at: "2026-11-18T06:22:00Z",
    kind: "edit",
    refId: id(401),
    actor: LE,
    summary: { action: "order.update", lines: "changed", orderDiscount: "changed", shipAddress: "changed" },
  },
  {
    at: "2026-11-18T06:06:00Z",
    kind: "note",
    refId: id(402),
    actor: LE,
    note: "Khách sỉ, xin giảm thêm vì lấy 64 món; đã báo chị Lan.",
  },
  {
    at: "2026-11-18T06:05:00Z",
    kind: "status",
    refId: id(403),
    actor: LE,
    summary: { to: "new", toPending: "discount_approval", reason: "manual_discount_over_threshold" },
  },
  { at: "2026-11-10T03:00:00Z", kind: "status", refId: id(404), summary: { from: "confirmed", to: "delivered" } },
];

const PAIRS: OrderDuplicate[] = [
  { order: at(2), otherOrder: at(0), matchedBy: "customer", createdAt: "2026-11-18T07:32:00Z" },
  { order: D1150, otherOrder: D1151, matchedBy: "phone", createdAt: "2026-11-17T14:15:00Z" },
];

function pairOrder(r: OrderListItem, items: OrderItem[]): Order {
  return { ...DETAIL, id: r.id, code: r.code, status: r.status, label: r.label, items, grandTotal: r.grandTotal };
}

async function orderApi(api: FakeApi, list: OrderListItem[] = ROWS) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/orders(\?|$)/, (r) => json(r, 200, { items: list, nextCursor: "c2" }));
  const orders = new Map<string, Order>([
    [DETAIL.id, DETAIL],
    [DELIVERED.id, DELIVERED],
    [
      id(3),
      pairOrder(at(2), [item(1, "DAM0123-BE-M", LONG, 1, 690_000, 0), item(6, "AO0789-DEN-M", "Áo", 1, 640_000, 0)]),
    ],
    [
      id(1),
      pairOrder(at(0), [
        item(1, "DAM0123-BE-M", LONG, 2, 640_000, 0),
        item(7, "AO0456-TRANG-S", "Áo", 1, 350_000, 0),
        item(8, "TUI0090-NAU", "Túi", 1, 290_000, 0),
      ]),
    ],
    [
      id(11),
      pairOrder(D1150, [
        item(9, "VAY0330-DEN-S", "Váy", 1, 280_000, 0),
        item(5, "KHAN0012-HOA", "Khăn", 1, 120_000, 0),
      ]),
    ],
    [
      id(12),
      pairOrder(D1151, [
        item(9, "VAY0330-DEN-S", "Váy", 1, 280_000, 0),
        item(5, "KHAN0012-HOA", "Khăn", 1, 120_000, 0),
      ]),
    ],
  ]);
  await api.on(/\/api\/v1\/orders\/[^/]+\/timeline$/, (r) => json(r, 200, { items: TIMELINE }));
  await api.on(/\/api\/v1\/orders\/[^/?]+$/, (r) => {
    const oid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    const o = orders.get(oid);
    return o === undefined ? json(r, 404, { error: "not_found", request_id: "e2e" }) : json(r, 200, o);
  });
  await api.on(/\/api\/v1\/returns\/[^/?]+$/, (r) => {
    const rid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    return json(
      r,
      200,
      RETURNS.find((x) => x.id === rid),
    );
  });
  await api.on(/\/api\/v1\/order-duplicates(\?|$)/, (r) => json(r, 200, { items: PAIRS }));
  await api.on("**/api/v1/traffic-sources", (r) =>
    json(r, 200, {
      items: SOURCE_NAMES.map(([sid, name], i) => ({ id: sid, name, active: true, position: i + 1, version: 1 })),
    }),
  );
  await api.on("**/api/v1/order-settings", (r) => json(r, 200, SETTINGS));
}

async function shot(page: Page, name: string) {
  await noHorizontalScroll(page);
  await page.screenshot({ path: `test-results/${name}.png`, fullPage: true });
}

forEachViewport((vp) => {
  test("danh sách: nhãn Mới, tên công ty dài, nghi trùng, tải thêm", async ({ page, api }) => {
    await orderApi(api);
    await page.goto("/don");
    await expect(page.getByRole("row", { name: /DH0001235/ })).toContainText("Chờ duyệt giảm giá");
    await expect(page.getByRole("button", { name: "Tải thêm 50 đơn" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Đơn trùng (2)" })).toBeVisible();
    await shot(page, `don-danh-sach-${String(vp.width)}`);
  });

  test("danh sách trống: tìm không ra", async ({ page, api }) => {
    await orderApi(api, []);
    await page.goto("/don");
    await page.getByRole("searchbox", { name: "Tìm đơn" }).fill("DH0009999");
    await expect(page.getByRole("button", { name: "Tìm trong mọi ngày" })).toBeVisible();
    await shot(page, `don-danh-sach-trong-${String(vp.width)}`);
  });

  test("chi tiết A: chờ duyệt giảm giá, người xem có quyền duyệt", async ({ page, api }) => {
    await orderApi(api);
    await page.goto(`/don/${DETAIL.id}`);
    await expect(page.getByRole("button", { name: "Duyệt giảm giá và xác nhận" })).toBeVisible();
    await expect(page.getByText("Cần duyệt giảm giá trước")).toBeVisible();
    await expect(page.getByRole("list", { name: "Lịch sử thao tác" })).toContainText("Sản phẩm: đã đổi");
    await shot(page, `don-chi-tiet-${String(vp.width)}`);
  });

  test("chi tiết B: thành công có phiếu trả; hộp trả/đổi quá 7 ngày", async ({ page, api }) => {
    await orderApi(api);
    await page.goto(`/don/${DELIVERED.id}`);
    await expect(page.getByRole("row", { name: /TH0000012/ })).toContainText("Chờ nhận hàng");
    await shot(page, `don-chi-tiet-b-${String(vp.width)}`);
    await page.getByRole("button", { name: "Tạo phiếu trả/đổi" }).click();
    const dlg = page.getByRole("dialog", { name: "Trả/đổi hàng · DH0001102" });
    await dlg
      .getByRole("row", { name: /DAM0123-BE-M/ })
      .getByRole("checkbox")
      .check();
    await expect(dlg).toContainText("620.000 ₫");
    await expect(dlg).toContainText("quá 7 ngày đổi trả của shop");
    await page.screenshot({ path: `test-results/don-tra-${String(vp.width)}.png` });
  });

  test("đơn trùng: hai cặp, sản phẩm chung tô nền", async ({ page, api }) => {
    await orderApi(api);
    await page.goto("/don/trung");
    await expect(page.getByRole("article", { name: "DH0001198 và DH0001236" })).toContainText("Chung 1 sản phẩm");
    await expect(page.getByRole("article", { name: "DH0001150 và DH0001151" })).toContainText("Chung 2 sản phẩm");
    await shot(page, `don-trung-${String(vp.width)}`);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("danh sách, chi tiết, đơn trùng không tràn ngang", async ({ page, api }) => {
    await orderApi(api);
    await page.goto("/don");
    await expect(page.getByRole("row", { name: /DH0001235/ })).toBeVisible();
    await shot(page, "don-danh-sach-360");
    await page.goto(`/don/${DETAIL.id}`);
    await expect(page.getByRole("button", { name: "Sửa đơn" })).toBeVisible();
    await shot(page, "don-chi-tiet-360");
    await page.goto("/don/trung");
    await expect(page.getByRole("article", { name: "DH0001150 và DH0001151" })).toBeVisible();
    await shot(page, "don-trung-360");
  });
});
