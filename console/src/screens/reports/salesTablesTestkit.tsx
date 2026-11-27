// API giả (fetch) cho test vitest của ba màn Báo cáo đơn, Hãng vận chuyển, Bán chạy (Task 240, M6-17). Chỉ test nhập tệp
// này. Số của kịch bản chín đơn (docs/m6/tieu-chi.md; mockup don.html trạng thái A: An 4, Bình 3, Chưa phân công 1,
// tổng 8, doanh số 1.910.000, TB 272.857; van-chuyen.html; ban-chay.html trạng thái B). Tỷ lệ là số máy chủ trả, làm
// tròn xuống: 2/3 = 6666 (66,66%).
import { fireEvent, screen } from "@testing-library/react";
import { expect } from "vitest";
import type {
  SalesBestSellerRow,
  SalesBestSellersReport,
  SalesCarriersReport,
  SalesOrdersDrilldownItem,
  SalesOrdersReport,
  SalesOrdersRow,
  SalesReportFilters,
} from "../../api";
import { ORDER_PAGE, renderScreens, reply } from "../../app/screenTestkit";
import {
  LONG_NAME,
  OWNER,
  amounts,
  fakeReports,
  orderRow,
  readRoute,
  stubApi,
  writeRoute,
  type ReportsFake,
} from "./reportsTestkit";
import { screens } from "./route";

export { LONG_NAME, OWNER, ORDER_PAGE, amounts, orderRow };
export { BACKGROUND, ONE_PAGE, busySteps, installFakeNow, settleRun } from "./reportsTestkit";

export const P1 = "0195f3b2-0000-7000-8000-00000000b001";
export const P2 = "0195f3b2-0000-7000-8000-00000000b002";
export const AN = "0195f3b2-0000-7000-8000-00000000c001";
export const BINH = "0195f3b2-0000-7000-8000-00000000c002";

export const FILTERS: SalesReportFilters = {
  sources: [{ id: "0195f3b2-0000-7000-8000-00000000d001", name: "Live trưa P1" }],
  staff: [
    { id: AN, name: "Nguyễn Thị An" },
    { id: BINH, name: LONG_NAME },
  ],
  pages: [
    { id: P1, name: "Page ELIMAZ Công Sở" },
    { id: P2, name: "Page ELIMAZ Dạo Phố" },
  ],
  carrierKeys: ["ghn", "vtp", "best", "spx", "self"],
};

const orderRowOf = (key: string, label: string, p: Partial<SalesOrdersRow>): SalesOrdersRow => ({
  key,
  label,
  orders: 0,
  placed: 0,
  active: 0,
  cancelled: 0,
  goodsAmount: 0,
  shippingFeeCharged: 0,
  avgGoodsAmount: null,
  delivered: 0,
  returned: 0,
  successRateBp: null,
  ...p,
});

const PERIOD = { from: "2026-11-27", to: "2026-11-27" };

/** Cơ sở ngày lên đơn, nhóm theo nhân viên phụ trách (mockup don.html trạng thái A). */
export const CARE_BY: SalesOrdersReport = {
  status: "ready",
  period: PERIOD,
  basis: "placed",
  groupBy: "care_by",
  rows: [
    orderRowOf(AN, "Nguyễn Thị An", {
      orders: 4,
      placed: 4,
      active: 4,
      goodsAmount: 1150000,
      shippingFeeCharged: 120000,
      avgGoodsAmount: 287500,
      delivered: 2,
      returned: 1,
      successRateBp: 6666,
    }),
    orderRowOf(BINH, LONG_NAME, {
      orders: 3,
      placed: 3,
      active: 2,
      cancelled: 1,
      goodsAmount: 400000,
      avgGoodsAmount: 200000,
    }),
    orderRowOf("unknown", "Chưa phân công", {
      orders: 1,
      placed: 1,
      active: 1,
      goodsAmount: 360000,
      shippingFeeCharged: 30000,
      avgGoodsAmount: 360000,
    }),
  ],
  totals: orderRowOf("", "Tổng", {
    orders: 8,
    placed: 8,
    active: 7,
    cancelled: 1,
    goodsAmount: 1910000,
    shippingFeeCharged: 150000,
    avgGoodsAmount: 272857,
    delivered: 2,
    returned: 1,
    successRateBp: 6666,
  }),
};

/** Cơ sở ngày hoàn tất, nhóm theo kênh: placed, active, cancelled là null (mockup don.html trạng thái C). */
export const COMPLETED_CHANNEL: SalesOrdersReport = {
  status: "ready",
  period: PERIOD,
  basis: "completed",
  groupBy: "channel",
  rows: [
    orderRowOf("facebook", "Facebook", {
      orders: 3,
      placed: null,
      active: null,
      cancelled: null,
      goodsAmount: 750000,
      shippingFeeCharged: 60000,
      avgGoodsAmount: 375000,
      delivered: 2,
      returned: 1,
      successRateBp: 6666,
    }),
    orderRowOf("unknown", "Không xác định", { placed: null, active: null, cancelled: null }),
  ],
  totals: orderRowOf("", "Tổng", {
    orders: 3,
    placed: null,
    active: null,
    cancelled: null,
    goodsAmount: 750000,
    shippingFeeCharged: 60000,
    avgGoodsAmount: 375000,
    delivered: 2,
    returned: 1,
    successRateBp: 6666,
  }),
};

const carrierRow = (key: string, p: Partial<SalesCarriersReport["totals"]>): SalesCarriersReport["totals"] => ({
  carrierKey: key,
  created: 0,
  cancelled: 0,
  inProgress: 0,
  returning: 0,
  deliveryFailed: 0,
  delivered: 0,
  returned: 0,
  successRateBp: null,
  feeTotal: 0,
  feeAvg: null,
  codTotal: 0,
  ...p,
});

export const LONG_ACCOUNT = "GHN — Kho Hà Nội — tài khoản chính dùng cho đơn live tối và đơn website khu vực miền Bắc";

/** Báo cáo hãng (mockup van-chuyen.html trạng thái A, rút gọn): GHN hai tài khoản, VTP 66,66%, Best chưa có đơn kết thúc. */
export const CARRIERS: SalesCarriersReport = {
  status: "ready",
  period: { from: "2026-11-21", to: "2026-11-27" },
  rows: [
    carrierRow("ghn", {
      carrierAccountId: "acc-ghn-1",
      accountName: LONG_ACCOUNT,
      created: 39,
      cancelled: 1,
      inProgress: 7,
      deliveryFailed: 1,
      delivered: 29,
      returned: 2,
      successRateBp: 9354,
      feeTotal: 960000,
      feeAvg: 24615,
      codTotal: 12020000,
    }),
    carrierRow("ghn", {
      carrierAccountId: "acc-ghn-2",
      accountName: "GHN — Kho phụ",
      created: 11,
      cancelled: 1,
      inProgress: 2,
      delivered: 8,
      returned: 1,
      successRateBp: 8888,
      feeTotal: 288000,
      feeAvg: 26181,
      codTotal: 3590000,
    }),
    carrierRow("vtp", {
      carrierAccountId: "acc-vtp-1",
      accountName: "Viettel Post — Kho chính",
      created: 30,
      cancelled: 1,
      inProgress: 5,
      returning: 1,
      delivered: 16,
      returned: 8,
      successRateBp: 6666,
      feeTotal: 806000,
      feeAvg: 26866,
      codTotal: 9020000,
    }),
    carrierRow("best", {
      carrierAccountId: "acc-best-1",
      accountName: "Best Express — Kho 1",
      created: 4,
      inProgress: 4,
      feeTotal: 88000,
      feeAvg: 22000,
      codTotal: 1160000,
    }),
    carrierRow("self", {
      carrierAccountId: "acc-self",
      accountName: "Shop tự giao",
      created: 3,
      delivered: 3,
      successRateBp: 10000,
      feeTotal: 0,
      feeAvg: 0,
      codTotal: 870000,
    }),
  ],
  totals: carrierRow("", {
    created: 87,
    cancelled: 3,
    inProgress: 18,
    returning: 1,
    deliveryFailed: 1,
    delivered: 56,
    returned: 11,
    successRateBp: 8358,
    feeTotal: 2142000,
    feeAvg: 24620,
    codTotal: 26660000,
  }),
};

/** Tên sản phẩm 120 ký tự (mockup ban-chay.html "ten-120"). */
export const LONG_LEN_PRODUCT =
  "Áo sơ mi lụa cổ đức tay bồng phối nơ cổ, dáng suông dài qua hông, chất lụa Hàn mềm mát không nhăn, mặc đi làm, đi chơi ạ";

const hex = (n: number) => n.toString(16).padStart(12, "0");

export function skuRow(n: number, p: Partial<SalesBestSellerRow> = {}): SalesBestSellerRow {
  return {
    productId: `0195f3b2-0000-7000-8000-${hex(0xa000 + n)}`,
    variantId: `0195f3b2-0000-7000-8000-${hex(0xb000 + n)}`,
    sku: `SKU-${String(n).padStart(5, "0")}`,
    name: `Áo mẫu ${String(n)}`,
    color: "Đen",
    size: "M",
    quantity: 1000 - n,
    orders: 900 - n,
    goodsAmount: (1000 - n) * 299000,
    returnedQuantity: 0,
    ...p,
  };
}

const productRow = (name: string, quantity: number, orders: number, goodsAmount: number, returnedQuantity: number) => ({
  productId: `0195f3b2-0000-7000-8000-${hex(0xc000 + quantity * 10 + orders)}`,
  name,
  quantity,
  orders,
  goodsAmount,
  returnedQuantity,
});

/** Mức mẫu của kịch bản chín đơn (mockup ban-chay.html trạng thái B): Áo A 7, Quần B 2 trước Váy C 2. */
export const PRODUCTS_BEST: SalesBestSellerRow[] = [
  productRow("Áo A", 7, 4, 1050000, 1),
  productRow("Quần B", 2, 2, 500000, 0),
  productRow("Váy C", 2, 1, 360000, 0),
];

export interface SalesTablesFake extends ReportsFake {
  /** Trả lời báo cáo đơn theo tham số (mặc định: CARE_BY, hay COMPLETED_CHANNEL khi basis=completed). */
  orders: (q: URLSearchParams) => unknown;
  filters: SalesReportFilters;
  carriers: unknown;
  /** Mọi dòng bán chạy theo mức; trả theo limit. */
  skuRows: SalesBestSellerRow[];
  productRows: SalesBestSellerRow[];
  /** Danh sách chi tiết theo "metric|cellKey" (cellKey rỗng khi cả bảng). */
  cells: Record<string, SalesOrdersDrilldownItem[]>;
  /** Có thì trả lỗi này cho mọi GET báo cáo của ba màn. */
  readError: { status: number; body: unknown } | null;
}

const defaultOrders = (q: URLSearchParams): unknown => {
  if (q.get("basis") === "completed") return COMPLETED_CHANNEL;
  return { ...CARE_BY, groupBy: q.get("groupBy") ?? "care_by" };
};

export function fakeSales(p: Partial<SalesTablesFake> = {}): SalesTablesFake {
  return {
    ...fakeReports(),
    orders: defaultOrders,
    filters: FILTERS,
    carriers: CARRIERS,
    skuRows: Array.from({ length: 1200 }, (_, i) => skuRow(i + 1)),
    productRows: PRODUCTS_BEST,
    cells: {
      [`placed|${AN}`]: [1, 2, 8, 9].map((n) => orderRow(n)),
      [`placed_active|${AN}`]: [1, 2, 8, 9].map((n) => orderRow(n)),
      [`delivered|${AN}`]: [orderRow(1, { statusLabel: "delivered" }), orderRow(2, { statusLabel: "delivered" })],
    },
    readError: null,
    ...p,
  };
}

function bestReply(f: SalesTablesFake, q: URLSearchParams): SalesBestSellersReport {
  const level = q.get("level") === "product" ? "product" : "sku";
  const rows = level === "product" ? f.productRows : f.skuRows;
  const limit = Number(q.get("limit") ?? "50");
  return {
    status: "ready",
    period: { from: q.get("from") ?? "", to: q.get("to") ?? "" },
    level,
    basis: q.get("basis") === "delivered" ? "delivered" : "placed",
    rows: rows.slice(0, limit),
    totalRows: rows.length,
  };
}

function route(f: SalesTablesFake, path: string, q: URLSearchParams): Response {
  if (path === "/api/v1/reports/sales/filters") return reply(f.filters);
  const mine = path.startsWith("/api/v1/reports/sales/") && !path.includes("/runs");
  if (mine && path !== "/api/v1/reports/sales/dashboard" && f.readError !== null) {
    f.gets.push(`${path}?${q.toString()}`);
    return reply(f.readError.body, f.readError.status);
  }
  if (path === "/api/v1/reports/sales/orders") {
    f.gets.push(`${path}?${q.toString()}`);
    const step = f.steps.shift();
    if (step !== undefined) {
      const headers = new Headers();
      if (step.retryAfter !== undefined) headers.set("Retry-After", step.retryAfter);
      return Response.json(step.body, { status: step.status, headers });
    }
    return reply(f.orders(q));
  }
  if (path === "/api/v1/reports/sales/carriers") {
    f.gets.push(`${path}?${q.toString()}`);
    const step = f.steps.shift();
    if (step !== undefined) return Response.json(step.body, { status: step.status });
    return reply(f.carriers);
  }
  if (path === "/api/v1/reports/sales/best-sellers") {
    f.gets.push(`${path}?${q.toString()}`);
    const step = f.steps.shift();
    if (step !== undefined) return Response.json(step.body, { status: step.status });
    return reply(bestReply(f, q));
  }
  if (path === "/api/v1/reports/sales/orders/drilldown") {
    f.gets.push(`${path}?${q.toString()}`);
    const key = `${q.get("metric") ?? ""}|${q.get("cellKey") ?? ""}`;
    const items = f.cells[key] ?? [];
    return reply({ status: "ready", total: items.length, items });
  }
  return readRoute(f, path, q);
}

/** Dựng các màn Báo cáo ở đường path với API giả f (kèm trang giả "/don/:id"). */
export function renderSales(f: SalesTablesFake, path: string) {
  stubApi(f, route, writeRoute);
  return renderScreens(screens, path, ORDER_PAGE);
}

/** Bấm "Xuất Excel", chờ thông báo, kiểm đúng một POST …/runs có Idempotency-Key; trả thân của POST đó. */
export async function exportExcel(f: SalesTablesFake): Promise<Record<string, unknown> | undefined> {
  fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
  await screen.findByText("Đang tạo tệp, xem ở Tệp xuất");
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0]?.key).toBeTruthy();
  return f.writes[0]?.body;
}
