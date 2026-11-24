// Màn Yêu cầu về đơn hàng (kế hoạch M5 Task 207, Task 486; mockup docs/mockups/m5/order-requests.html) ở 1512 × 767, 390 × 844 và
// 360, API giả. Hành vi (lọc, trả lời, đổi trạng thái, 409, quyền) kiểm ở test vitest của src/screens/order-requests; ở
// đây kiểm bố cục không tràn ngang với dữ liệu khó (mã dài, lời nhắn dài không dấu cách, đủ 8 trạng thái, 3 ảnh) và
// chụp test-results/order-requests-*.png để so với mockup.
import type { Page } from "@playwright/test";
import type { Order, OrderRequest, OrderRequestKind, OrderRequestStatus, OrderRequestSummary } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000e${String(n).padStart(4, "0")}`;
const ORD = id(500);
const GRANTS = [
  ...OWNER_GRANTS,
  ...["order.read", "order.requests.read", "order.requests.manage"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
];
const LONG = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

const STATUSES: OrderRequestStatus[] = [
  "received",
  "in_review",
  "needs_info",
  "confirmed",
  "items_received",
  "resolved",
  "rejected",
  "cancelled",
];
const KINDS: OrderRequestKind[] = ["exchange", "return", "cancel", "question"];
const REASONS = ["SIZE", "QUALITY", "WRONG_ITEM", "CHANGED_MIND", "OTHER"];

/** 11 dòng: đủ 8 trạng thái và 4 loại; tên khách 60 ký tự, số ảnh, hạn gửi trả (Task 486). */
const ROWS: OrderRequestSummary[] = Array.from({ length: 11 }, (_, i) => ({
  id: id(i + 1),
  ref: `YC${String(148 - i).padStart(7, "0")}`,
  orderId: ORD,
  orderCode: `DH${String(1312 - i).padStart(7, "0")}`,
  kind: KINDS[i % KINDS.length] ?? "exchange",
  status: STATUSES[i % STATUSES.length] ?? "received",
  reasonCode: REASONS[i % REASONS.length] ?? "OTHER",
  customerName: i === 0 ? LONG : "Phạm Minh Anh",
  photoCount: i % 4,
  ...(STATUSES[i % STATUSES.length] === "confirmed" ? { shipBackDeadline: "2026-11-25T16:59:59Z" } : {}),
  createdAt: "2026-11-21T03:42:00Z",
  updatedAt: "2026-11-21T03:52:00Z",
  version: 1,
}));

const DETAIL: OrderRequest = {
  id: id(1),
  ref: "YC0000148",
  orderId: ORD,
  orderCode: "DH0001312",
  kind: "exchange",
  status: "in_review",
  reasonCode: "SIZE",
  customerNote: `Váy mặc chật ở ngực, cho mình đổi sang size L nhé. <script>alert('x')</script> ${LONG}`,
  conditionConfirmed: true,
  lines: [
    {
      variantId: id(601),
      qty: 1,
      reason: "TOO_SMALL",
      wantVariantId: id(602),
      sku: "DS2201-DEN-M",
      name: LONG,
      want: {
        sku: "DS2201-DEN-L",
        productName: LONG,
        color: "Đen",
        size: "L",
        available: 4,
        warehouseName: "Kho chính",
      },
    },
    {
      variantId: id(603),
      qty: 2,
      reason: "DEFECT",
      note: "Bung đường may ở vai",
      sku: "SM1408-TRA-S",
      name: "Áo sơ mi lụa",
    },
  ],
  messages: [
    {
      id: "m1",
      authorType: "staff",
      authorName: "Lê Hồng Nhung",
      body: "Chào chị, ELIMAZ đã nhận yêu cầu, bên em kiểm tra ảnh và báo lại trong hôm nay ạ.",
      visibleToCustomer: true,
      createdAt: "2026-11-21T03:50:00Z",
    },
    {
      id: "m2",
      authorType: "staff",
      authorName: "Trần Minh Thư",
      body: `Kho còn 4 chiếc Đen / L. ${LONG}`,
      visibleToCustomer: false,
      createdAt: "2026-11-21T03:52:00Z",
    },
  ],
  files: [
    { fileId: "f1", status: "ready", url: "https://files.elimaz.test/priv/a.jpg?sig=1" },
    { fileId: "f2", status: "processing" },
    { fileId: "f3", status: "rejected" },
  ],
  history: [
    { fromStatus: "received", toStatus: "in_review", actorType: "staff", actorName: LONG, at: "2026-11-21T03:51:00Z" },
  ],
  createdAt: "2026-11-21T03:42:00Z",
  updatedAt: "2026-11-21T03:52:00Z",
  version: 2,
};

const COUNTS = {
  statuses: STATUSES.map((status) => ({ status, count: ROWS.filter((r) => r.status === status).length })),
  open: ROWS.filter((r) => !["resolved", "rejected", "cancelled"].includes(r.status)).length,
  total: ROWS.length,
};

const ORDER: Order = {
  id: ORD,
  code: "DH0001312",
  status: "delivered",
  version: 7,
  customerId: id(81),
  customer: {
    id: id(81),
    rootId: id(81),
    code: "KH000877",
    fullName: LONG,
    stats: {
      orderCount: 3,
      deliveredCount: 3,
      returnedCount: 0,
      totalSpent: 3_810_000,
      lastOrderAt: "2026-11-18T06:05:00Z",
      returnRateBp: 0,
    },
  },
  items: [],
  subtotal: 1_270_000,
  lineDiscountTotal: 0,
  orderDiscount: 0,
  couponDiscount: 0,
  discountTotal: 0,
  shippingFee: 0,
  grandTotal: 1_270_000,
  prepaidTotal: 0,
  codAmount: 1_270_000,
  createdAt: "2026-11-18T06:05:00Z",
  channel: "web",
  addressComplete: true,
  returns: [],
};

async function requestApi(api: FakeApi, detail: OrderRequest = DETAIL) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/order-requests(\?|$)/, (r) => json(r, 200, { items: ROWS, nextCursor: "c2" }));
  await api.on(/\/api\/v1\/order-requests\/[^/?]+$/, (r) => json(r, 200, detail));
  // Đăng ký sau đường chi tiết: route đăng ký sau được xét trước.
  await api.on(/\/api\/v1\/order-requests\/counts$/, (r) => json(r, 200, COUNTS));
  await api.on(`**/api/v1/orders/${ORD}`, (r) => json(r, 200, ORDER));
}

const shot = (page: Page, name: string, w: number) =>
  page.screenshot({ path: `test-results/order-requests-${name}-${String(w)}.png`, fullPage: true });

forEachViewport((vp) => {
  test("danh sách: đủ trạng thái và loại, còn trang sau, không tràn ngang", async ({ page, api }) => {
    await requestApi(api);
    await page.goto("/don/yeu-cau");
    await page.getByRole("button", { name: /^Tất cả/ }).click();
    await expect(page.getByRole("link", { name: "YC0000148" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Tải thêm" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", vp.width);
  });

  test("chi tiết đổi hàng: tên dài, 3 ảnh, tin hai phía, thẻ xử lý không tràn ngang", async ({ page, api }) => {
    await requestApi(api);
    await page.goto(`/don/yeu-cau/${DETAIL.id}`);
    await expect(page.getByText("<script>alert('x')</script>", { exact: false })).toBeVisible();
    await expect(page.getByRole("button", { name: "Lưu xử lý" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "chi-tiet", vp.width);
  });

  test("Đã xác nhận đổi: khung hướng dẫn gửi trả không tràn ngang", async ({ page, api }) => {
    await requestApi(api);
    await page.goto(`/don/yeu-cau/${DETAIL.id}`);
    await page.getByRole("combobox", { name: "Trạng thái tiếp theo" }).selectOption("confirmed");
    await expect(page.getByRole("textbox", { name: "Mã ghi trên kiện hàng *" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "xac-nhan-doi", vp.width);
  });

  test("hộp Từ chối không tràn ngang", async ({ page, api }) => {
    await requestApi(api);
    await page.goto(`/don/yeu-cau/${DETAIL.id}`);
    await page.getByRole("combobox", { name: "Trạng thái tiếp theo" }).selectOption("rejected");
    await page.getByRole("button", { name: "Lưu xử lý" }).click();
    await expect(page.getByRole("dialog", { name: "Từ chối yêu cầu này?" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "tu-choi", vp.width);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("danh sách và chi tiết đã đóng không tràn ngang với chữ dài", async ({ page, api }) => {
    await requestApi(api, {
      ...DETAIL,
      status: "rejected",
      outcome: { reason: `Đã quá 7 ngày kể từ ngày nhận. ${LONG}` },
    });
    await page.goto("/don/yeu-cau");
    await expect(page.getByRole("link", { name: "YC0000148" })).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "danh-sach", 360);
    await page.goto(`/don/yeu-cau/${DETAIL.id}`);
    await expect(page.getByText(/Đã quá 7 ngày/)).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "da-dong", 360);
  });
});
