// Màn Khách (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-danh-sach.html, khach-ho-so.html, khach-gop.html) ở
// 1512 × 767, 390 × 844 và 360 (tên công ty dài), API giả. Hành vi (tìm, nhãn, gộp, hoàn tác, nhập) kiểm ở các test
// vitest của src/screens/khach; ở đây kiểm bố cục không tràn và chụp test-results/khach-*.png để so với
// docs/mockups/m2/png/khach-{danh-sach,ho-so,gop}-{1512,390,360}.png.
import type { Page } from "@playwright/test";
import type { Customer, CustomerListItem, CustomerTag, TimelineItem } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000c${String(n).padStart(4, "0")}`;
const COMPANY = "Công ty TNHH Thương mại Dịch vụ Thời trang Hoàng Gia Minh Châu";

const GRANTS = [
  ...OWNER_GRANTS,
  ...[
    "customer.read",
    "customer.write",
    "customer.merge",
    "customer.tags.manage",
    "customer.import",
    "files.upload",
    "order.create",
  ].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

const zero = { orderCount: 0, deliveredCount: 0, returnedCount: 0, totalSpent: 0, returnRateBp: 0 };
const HA_STATS = {
  orderCount: 12,
  deliveredCount: 9,
  returnedCount: 2,
  totalSpent: 8_450_000,
  lastOrderAt: "2026-11-18T07:31:00Z",
  returnRateBp: 1818,
};

const ROWS: CustomerListItem[] = [
  { id: id(1), code: "KH000231", fullName: "Trần Thị Thu Hà", phones: ["+84912345678"], stats: HA_STATS },
  {
    id: id(2),
    code: "KH000877",
    fullName: COMPANY,
    phones: ["+84903111222"],
    stats: { ...HA_STATS, orderCount: 23, totalSpent: 96_400_000 },
  },
  { id: id(3), code: "KH004511", fullName: "Mai (fb)", phones: [], stats: zero },
  {
    id: id(4),
    code: "KH004410",
    fullName: "Lê Hà My",
    phones: ["+84355125678"],
    stats: { ...zero, orderCount: 1, lastOrderAt: "2026-11-16T03:00:00Z" },
  },
];

const TAGS: CustomerTag[] = [
  { id: id(51), name: "Khách quen", color: "gray", version: 1 },
  { id: id(52), name: "Hay hoàn", color: "red", version: 1 },
  { id: id(53), name: "Khách sỉ", color: "blue", version: 1 },
];
const tag = (i: number) => {
  const t = TAGS[i];
  if (t === undefined) throw new Error("thiếu nhãn");
  return t;
};

const HA: Customer = {
  id: id(1),
  rootId: id(1),
  code: "KH000231",
  fullName: "Trần Thị Thu Hà",
  lifecycle: "customer",
  birthday: "1990-08-12",
  stats: HA_STATS,
  memberIds: [id(1), id(9)],
  identities: [
    { id: id(61), customerId: id(1), kind: "phone", value: "+84912345678", source: "order", createdAt: "" },
    { id: id(62), customerId: id(9), kind: "phone", value: "+84355999000", source: "staff", createdAt: "" },
    { id: id(63), customerId: id(1), kind: "fb_psid", value: "psid", source: "chat", createdAt: "" },
  ],
  addresses: [
    {
      id: id(71),
      customerId: id(1),
      isDefault: true,
      version: 1,
      address: {
        name: "Trần Thị Thu Hà",
        phone: "+84912345678",
        line: "Số 12 ngõ 68 Trần Thái Tông",
        wardName: "Phường Cầu Giấy",
        provinceName: "Hà Nội",
      },
    },
    {
      id: id(72),
      customerId: id(1),
      isDefault: false,
      version: 1,
      address: { name: "Nguyễn Văn Tùng", phone: "+84987654321", line: "Tầng 5, 18 Phạm Hùng", provinceName: "Hà Nội" },
    },
  ],
  tags: [
    { tag: tag(0), source: "staff" },
    { tag: tag(1), source: "rule" },
  ],
  version: 4,
  createdAt: "2026-03-01T00:00:00Z",
};
const TUNG: Customer = {
  ...HA,
  id: id(5),
  rootId: id(5),
  code: "KH001877",
  fullName: "Nguyễn Văn Tùng",
  memberIds: [id(5)],
  identities: [{ id: id(64), customerId: id(5), kind: "phone", value: "+84987654321", source: "order", createdAt: "" }],
  addresses: [],
  tags: [],
  stats: { ...zero, orderCount: 2, deliveredCount: 2, totalSpent: 980_000 },
};

const LAN = { id: id(90), name: "Nguyễn Thị Lan" };
const TIMELINE: TimelineItem[] = [
  {
    at: "2026-11-18T07:31:00Z",
    kind: "order_created",
    refId: id(81),
    actor: { id: id(91), name: "Phạm Thu Trang" },
    summary: { code: "DH0001236", status: "new", grand_total: "1800000" },
  },
  {
    at: "2026-11-18T03:02:00Z",
    kind: "note",
    refId: id(82),
    actor: { id: id(91), name: "Phạm Thu Trang" },
    note: "Khách hay đổi size, tư vấn kỹ số đo trước khi chốt.",
  },
  {
    at: "2026-11-02T13:40:00Z",
    kind: "order_status",
    refId: id(83),
    summary: { order_id: id(84), code: "DH0001010", from_status: "delivered", to_status: "returned" },
  },
  {
    at: "2026-11-01T08:00:00Z",
    kind: "merge",
    refId: id(85),
    actor: LAN,
    summary: { survivor_id: id(1), merged_id: id(9), matched_by: "phone" },
  },
  {
    at: "2026-10-02T02:30:00Z",
    kind: "merge",
    refId: id(86),
    actor: LAN,
    summary: { survivor_id: id(1), merged_id: id(8), matched_by: "staff" },
  },
  { at: "2026-10-20T04:20:00Z", kind: "tag", refId: id(52), summary: { name: "Hay hoàn", source: "rule" } },
];

async function customerApi(api: FakeApi, rows: CustomerListItem[] = ROWS) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/customers(\?|$)/, (r) => {
    const q = new URL(r.request().url()).searchParams.get("q");
    if (q === null) return json(r, 200, { items: rows, nextCursor: "c2" });
    const d = q.replace(/\D/g, "");
    const hits = rows
      .filter((c) => d !== "" && c.phones.some((p) => p.endsWith(d)))
      .map((c) => ({ ...c, matchedBy: "phone_tail" }));
    return json(r, 200, { items: hits });
  });
  await api.on(/\/api\/v1\/customers\/[^/]+\/timeline/, (r) => json(r, 200, { items: TIMELINE }));
  await api.on(/\/api\/v1\/customers\/[^/]+\/merge-candidates$/, (r) =>
    json(r, 200, {
      items: [
        {
          rootId: TUNG.rootId,
          code: TUNG.code,
          fullName: TUNG.fullName,
          matchedPhones: ["+84987654321"],
          stats: TUNG.stats,
        },
      ],
    }),
  );
  await api.on(/\/api\/v1\/customers\/[^/?]+$/, (r) => {
    const cid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    return json(r, 200, cid === TUNG.id ? TUNG : HA);
  });
  await api.on("**/api/v1/customer-tags", (r) => json(r, 200, { items: TAGS }));
  await api.on(/\/api\/v1\/customer-tags\/[^/]+\/rule$/, (r) =>
    r.request().url().includes(tag(1).id)
      ? json(r, 200, {
          tagId: tag(1).id,
          enabled: true,
          version: 1,
          conditions: [
            { field: "return_rate_bp", op: "gte", value: 3000 },
            { field: "returned_count", op: "gte", value: 2 },
          ],
        })
      : json(r, 404, { error: "not_found", request_id: "e2e" }),
  );
}

/** Kiểm không tràn ngang rồi chụp cả trang vào test-results/khach-<tên>.png. */
async function snap(page: Page, name: string) {
  await noHorizontalScroll(page);
  await page.screenshot({ fullPage: true, path: `test-results/khach-${name}.png` });
}

forEachViewport((vp) => {
  test("danh sách: tên dài, chưa có SĐT, tải thêm; tìm 5678", async ({ page, api }) => {
    await customerApi(api);
    await page.goto("/khach");
    await expect(page.getByRole("row", { name: /KH000877/ })).toContainText("96.400.000 ₫");
    await expect(page.getByRole("button", { name: "Tải thêm" })).toBeVisible();
    await snap(page, `danh-sach-${String(vp.width)}`);
    await page.getByRole("searchbox", { name: "Tìm khách" }).fill("5678");
    await expect(page.getByRole("row", { name: /Đuôi SĐT/ })).toHaveCount(2);
    await snap(page, `tim-${String(vp.width)}`);
  });

  test("danh sách: hộp nhãn và quy tắc; hộp nhập Excel", async ({ page, api }) => {
    await customerApi(api);
    await page.goto("/khach");
    await page.getByRole("button", { name: "Nhãn" }).click();
    const dlg = page.getByRole("dialog", { name: "Nhãn khách" });
    await dlg.getByRole("button", { name: "Sửa quy tắc" }).click();
    await expect(dlg.getByRole("textbox", { name: "Giá trị 1" })).toHaveValue("30");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/khach-nhan-${String(vp.width)}.png` });
    await dlg.getByRole("button", { name: "Xong" }).click();
    await page.getByRole("button", { name: "Nhập Excel" }).click();
    await expect(page.getByRole("dialog", { name: "Nhập khách từ Excel" })).toContainText("định dạng Văn bản");
    await page.screenshot({ path: `test-results/khach-nhap-${String(vp.width)}.png` });
  });

  test("hồ sơ: gộp 2 hồ sơ, nhãn tay và theo quy tắc, gợi ý cùng một khách", async ({ page, api }) => {
    await customerApi(api);
    await page.goto(`/khach/${HA.id}`);
    await expect(page.getByRole("heading", { name: "Trần Thị Thu Hà", level: 1 })).toBeVisible();
    await expect(page.getByText("Hay hoàn · theo quy tắc")).toBeVisible();
    await expect(page.getByRole("link", { name: "So sánh và gộp" })).toBeVisible();
    await expect(page.getByRole("link", { name: "DH0001236" })).toBeVisible();
    await snap(page, `ho-so-${String(vp.width)}`);
  });

  test("gộp: hai hồ sơ, chọn giữ lại, xác nhận; các lần gộp", async ({ page, api }) => {
    await customerApi(api);
    await page.goto(`/khach/${HA.id}/gop?voi=${TUNG.id}`);
    await page.getByRole("radio", { name: "Giữ hồ sơ này làm đại diện" }).first().check();
    await expect(page.getByRole("button", { name: "Gộp vào KH000231…" })).toBeEnabled();
    await expect(page.getByText("Quá 30 ngày, không hoàn tác được.")).toBeVisible();
    await snap(page, `gop-${String(vp.width)}`);
    await page.getByRole("button", { name: "Gộp vào KH000231…" }).click();
    await expect(page.getByRole("dialog", { name: "Gộp KH001877 vào KH000231?" })).toBeVisible();
    await page.screenshot({ path: `test-results/khach-gop-xac-nhan-${String(vp.width)}.png` });
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("danh sách, hồ sơ, gộp không tràn ngang", async ({ page, api }) => {
    await customerApi(api);
    await page.goto("/khach");
    await expect(page.getByRole("row", { name: /KH000877/ })).toBeVisible();
    await snap(page, "danh-sach-360");
    await page.goto(`/khach/${HA.id}`);
    await expect(page.getByText("Hay hoàn · theo quy tắc")).toBeVisible();
    await snap(page, "ho-so-360");
    await page.goto(`/khach/${HA.id}/gop?voi=${TUNG.id}`);
    await expect(page.getByRole("heading", { name: "Nguyễn Văn Tùng" })).toBeVisible();
    await snap(page, "gop-360");
  });
});
