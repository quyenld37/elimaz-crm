// Màn Lên đơn nhanh (kế hoạch M2 Task M2-27; mockup docs/mockups/m2/len-don.html trạng thái A, B) ở 1512 × 767,
// 390 × 844 và 360 (tên 120 ký tự), API giả. Hành vi (phím, gửi lại cùng khóa, lỗi) kiểm ở OrderForm.test.tsx; ở đây
// kiểm bố cục không tràn và chụp ảnh test-results/len-don-*.png để so với docs/mockups/m2/png/len-don-{1512,390,360}.png.
import type { Page, Route } from "@playwright/test";
import type { Customer, CustomerListItem, VariantSearchHit } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000e${String(n).padStart(4, "0")}`;
const LONG =
  "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa thủ công tay phồng nhẹ dáng xòe xếp ly nhiều tầng màu xanh ngọc bích Thu 26";

const GRANTS = [
  ...OWNER_GRANTS,
  ...["order.create", "order.read", "customer.read"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
];

const stats = { orderCount: 12, deliveredCount: 9, returnedCount: 2, totalSpent: 8450000, returnRateBp: 1800 };
const HA: CustomerListItem = {
  id: id(1),
  code: "KH000231",
  fullName: "Trần Thị Thu Hà",
  phones: ["+84912345678"],
  matchedBy: "phone_tail",
  stats: { ...stats, lastOrderAt: "2026-11-18T03:00:00Z" },
};
const MY: CustomerListItem = {
  id: id(2),
  code: "KH004410",
  fullName: "Lê Hà My",
  phones: ["+84988005678"],
  matchedBy: "phone_tail",
  stats: { orderCount: 1, deliveredCount: 0, returnedCount: 0, totalSpent: 0, returnRateBp: 0 },
};
const TUNG: CustomerListItem = {
  id: id(3),
  code: "KH001877",
  fullName: "Nguyễn Văn Tùng",
  phones: ["+84987654321"],
  matchedBy: "phone",
  stats: { ...stats, orderCount: 3 },
};
const HA_DETAIL: Customer = {
  id: HA.id,
  rootId: HA.id,
  code: HA.code,
  fullName: HA.fullName,
  lifecycle: "customer",
  stats: HA.stats,
  memberIds: [HA.id],
  identities: [
    {
      id: id(11),
      customerId: HA.id,
      kind: "phone",
      value: "+84912345678",
      source: "staff",
      createdAt: "2026-01-01T00:00:00Z",
    },
  ],
  addresses: [
    {
      id: id(12),
      customerId: HA.id,
      isDefault: true,
      version: 1,
      address: {
        name: HA.fullName,
        phone: "+84912345678",
        line: "Số 5 Láng Hạ",
        provinceCode: "01",
        wardCode: "00004",
      },
    },
    {
      id: id(13),
      customerId: HA.id,
      isDefault: false,
      version: 1,
      address: { name: "Nguyễn Văn Tùng", phone: "+84987654321", line: "Số 12", provinceCode: "01", wardCode: "00004" },
    },
  ],
  tags: [
    { tag: { id: id(21), name: "Khách quen", color: "line", version: 1 }, source: "rule" },
    { tag: { id: id(22), name: "Hay hoàn", color: "err", version: 1 }, source: "rule" },
  ],
  version: 4,
  createdAt: "2026-01-01T00:00:00Z",
};

const hit = (n: number, sku: string, productName: string, color: string, size: string, price: number) => ({
  variantId: id(100 + n),
  productId: id(200 + n),
  sku,
  barcode: `20000000000${String(n)}`,
  productCode: sku.split("-")[0] ?? sku,
  productName,
  color,
  size,
  price,
  sellable: true,
});
const HITS: VariantSearchHit[] = [
  hit(1, "DAM0123-BE-M", LONG, "Be", "M", 690000),
  hit(2, "AO0456-TRANG-S", "Áo sơ mi lụa cổ đức", "Trắng", "S", 350000),
  hit(3, "TUI0090-NAU", "Túi hộp da mini", "Nâu", "", 290000),
];
const AVAILABLE = [5, 1, 23];

/** Tham số truy vấn của yêu cầu tới API giả ("" khi thiếu). */
const param = (r: Route, name: string) => new URL(r.request().url()).searchParams.get(name) ?? "";

async function orderApi(api: FakeApi) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/customers(\?|$)/, (r) => {
    const q = param(r, "q").replace(/\D/g, "");
    if (q.length >= 9) return json(r, 200, { items: [TUNG] });
    return json(r, 200, { items: [HA, MY] });
  });
  await api.on(/\/api\/v1\/customers\/[^/?]+$/, (r) => json(r, 200, HA_DETAIL));
  await api.on(/\/api\/v1\/admin-units/, (r) => {
    const parent = param(r, "parent") === "" ? null : param(r, "parent");
    return json(r, 200, {
      items:
        parent === null
          ? [
              { code: "01", name: "Hà Nội", level: "province" },
              { code: "79", name: "TP. Hồ Chí Minh", level: "province" },
            ]
          : [
              { code: "00004", name: "Cầu Giấy", level: "ward", parentCode: "01" },
              { code: "00005", name: "Nghĩa Đô", level: "ward", parentCode: "01" },
            ],
    });
  });
  await api.on(/\/api\/v1\/catalog\/variants\/search/, (r) => {
    const prefix = param(r, "q").toUpperCase();
    return json(
      r,
      200,
      HITS.filter((h) => h.sku.startsWith(prefix)),
    );
  });
  await api.on("**/api/v1/inventory/warehouses", (r) =>
    json(r, 200, [{ id: id(50), code: "MAIN", name: "Kho chính", status: "active" }]),
  );
  await api.on(/\/api\/v1\/inventory\/balances/, (r) =>
    json(r, 200, {
      items: HITS.map((h, i) => {
        const n = AVAILABLE[i] ?? 0;
        // Thân thừa vài trường của VariantSearchHit (price, sellable…): màn chỉ đọc variantId, available.
        return {
          ...h,
          productStatus: "active",
          variantStatus: "active",
          onHand: n,
          held: 0,
          inTransit: 0,
          damaged: 0,
          available: n,
        };
      }),
    }),
  );
  await api.on(/\/api\/v1\/coupon-check/, (r) => json(r, 200, { ok: true, discount: 150000, remainingUses: 12 }));
  await api.on(/\/api\/v1\/order-duplicate-check/, (r) => json(r, 200, { items: [] }));
  await api.on("**/api/v1/traffic-sources", (r) =>
    json(r, 200, {
      items: [
        { id: id(61), name: "Điện thoại", active: true, position: 1, version: 1 },
        { id: id(62), key: "manual", name: "Nhập tay", active: true, position: 5, version: 1 },
      ],
    }),
  );
  await api.on("**/api/v1/order-settings", (r) =>
    json(r, 200, {
      draftTtlMinutes: 1440,
      duplicateWindowMinutes: 1440,
      discountApprovalThresholdBp: 1000,
      returnWarningDays: 7,
      version: 1,
    }),
  );
}

async function addProduct(page: Page, sku: string) {
  const box = page.getByRole("combobox", { name: "Thêm sản phẩm theo mã, tên hoặc barcode" });
  await box.fill(sku);
  await expect(page.getByRole("listbox", { name: "Kết quả tìm sản phẩm" })).toContainText(sku);
  await box.press("Enter");
  await expect(page.getByRole("textbox", { name: `Số lượng ${sku}` })).toBeVisible();
}

/** Trạng thái A của mockup: khách đã chọn, SĐT người nhận thuộc hồ sơ khác, 3 dòng (tên 120 ký tự), mã đã áp. */
async function composeA(page: Page) {
  await page.goto("/don/moi");
  const cust = page.getByRole("combobox", { name: "Tìm khách theo đuôi SĐT, tên hoặc mã KH" });
  await cust.fill("5678");
  await expect(page.getByRole("listbox", { name: "Kết quả tìm khách" })).toContainText("Lê Hà My");
  await cust.press("Enter");
  await expect(page.locator(".picked")).toContainText("Hay hoàn");
  await page.getByRole("textbox", { name: "Người nhận *", exact: true }).fill("Nguyễn Văn Tùng");
  await page.getByRole("textbox", { name: "SĐT người nhận *" }).fill("0987 654 321");
  await expect(page.getByText("Có thể là cùng một khách.")).toBeVisible();
  await page.getByRole("combobox", { name: "Tỉnh/thành *" }).selectOption("01");
  const ward = page.getByRole("combobox", { name: "Xã/phường *" });
  await ward.fill("cau");
  await expect(page.getByRole("option", { name: "Cầu Giấy" })).toBeVisible();
  await ward.press("Enter");
  await page.getByRole("textbox", { name: "Số nhà, đường *" }).fill("Số 12 ngõ 68 Trần Thái Tông");
  for (const sku of ["DAM0123-BE-M", "AO0456-TRANG-S", "TUI0090-NAU"]) await addProduct(page, sku);
  await page.getByRole("button", { name: "Thêm 1 DAM0123-BE-M" }).click();
  await page.getByRole("textbox", { name: "Giảm dòng DAM0123-BE-M (đồng)" }).fill("100.000");
  await page.getByRole("textbox", { name: "Phí ship báo khách" }).fill("30.000");
  const code = page.getByRole("textbox", { name: "Mã giảm giá" });
  await code.fill("THU10");
  await code.press("Enter");
  await expect(page.getByText("Áp được: giảm 150.000 ₫ · còn 12 lượt")).toBeVisible();
  await page
    .getByRole("textbox", { name: "Ghi chú cho hãng vận chuyển" })
    .fill("Gọi trước khi giao, giao giờ hành chính.");
}

forEachViewport((vp) => {
  test("trạng thái B: form trống, nút mờ kèm lý do", async ({ page, api }) => {
    await orderApi(api);
    await page.goto("/don/moi");
    await expect(page.getByRole("button", { name: "Lưu và xác nhận" })).toBeDisabled();
    await expect(page.getByText("Chưa lưu được: chọn khách và thêm ít nhất một sản phẩm.")).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/len-don-b-${String(vp.width)}.png`, fullPage: true });
  });

  test("trạng thái A: khách, địa chỉ, 3 dòng tên dài, mã, tổng 1.800.000", async ({ page, api }) => {
    await orderApi(api);
    await composeA(page);
    const sum = page.getByRole("region", { name: "Tiền" });
    await expect(sum).toContainText("2.020.000 ₫");
    await expect(sum).toContainText("1.800.000 ₫");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/len-don-${String(vp.width)}.png`, fullPage: true });
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("tên 120 ký tự, ô số không tràn", async ({ page, api }) => {
    await orderApi(api);
    await composeA(page);
    await noHorizontalScroll(page);
    await page.screenshot({ path: "test-results/len-don-360.png", fullPage: true });
  });
});
