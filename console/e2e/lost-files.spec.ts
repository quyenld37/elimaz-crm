// Màn Tệp bị mất (kế hoạch M1 Task 331, ca TP12 phần màn hình; mockup docs/mockups/m1/tep-bi-mat.html, trạng thái A,
// B, C) ở 1512 × 767 và 390 × 844, API giả (e2e/fixtures.ts). Luật thật ở API (Task 46: lệnh file.verify, đánh dấu
// lost); ở đây kiểm màn hiện đúng các dòng có mã mẫu và nút mở mẫu, nút "Kiểm lại toàn bộ tệp" gọi verifyFiles một lần
// có khóa chống trùng rồi chuyển sang "Đang kiểm", trạng thái trống, lỗi API, và mục menu theo quyền.
import type { Grant, LostFile, LostFilePage } from "../src/api";
import {
  OWNER_GRANTS,
  expect,
  forEachViewport,
  json,
  noHorizontalScroll,
  test,
  writeRecorder,
  type FakeApi,
  type WriteLog,
} from "./fixtures";

const PATH = "/he-thong/tep-bi-mat";
const MANAGE: Grant[] = [...OWNER_GRANTS, { permission: "platform.files.manage", scope: { type: "all", id: "" } }];
const SET0012 = "0195f3b2-0000-7000-8000-0000000c0012";
const DAM0123 = "0195f3b2-0000-7000-8000-0000000c0123";

const lost = (n: number, s: Partial<LostFile>): LostFile => ({
  fileId: `0195f3b2-0000-7000-8000-0000000f${String(n).padStart(4, "0")}`,
  purpose: "product_image",
  // 03:00 18/11 giờ Việt Nam.
  lostAt: "2026-11-17T20:00:00Z",
  ...s,
});

// Hai dòng của trạng thái A trong mockup.
const TWO: LostFile[] = [
  lost(1, { productId: SET0012, productCode: "SET0012" }),
  lost(2, { productId: DAM0123, productCode: "DAM0123" }),
];

interface Fake extends WriteLog {
  /** Số lần gọi listLostFiles. */
  lists: number;
}

/** API giả: listLostFiles trả `items` (đổi được giữa chừng); verifyFiles ghi log và trả 202 {jobId}. */
async function lostApi(api: FakeApi, opts: { grants?: Grant[]; items?: LostFile[]; listStatus?: number } = {}) {
  const fake: Fake & { items: LostFile[] } = { writes: [], failNext: null, lists: 0, items: opts.items ?? TWO };
  const rec = writeRecorder(fake);
  await api.live({ grants: opts.grants ?? MANAGE });
  await api.on(/\/api\/v1\/platform\/files\/lost(\?|$)/, (r) => {
    fake.lists++;
    if (opts.listStatus !== undefined) return json(r, opts.listStatus, { error: "forbidden", request_id: "e2e" });
    const body: LostFilePage = { items: fake.items };
    return json(r, 200, body);
  });
  await api.on("**/api/v1/platform/files/verify", async (r) => {
    rec.record(r);
    if (await rec.fail(r)) return;
    await json(r, 202, { jobId: "0195f3b2-0000-7000-8000-0000000j0001" });
  });
  return fake;
}

forEachViewport((vp) => {
  test("TP12: 2 tệp mất → 2 dòng có mã mẫu, nút mở mẫu để tải lại; Kiểm lại gọi verifyFiles một lần", async ({
    page,
    api,
  }) => {
    const fake = await lostApi(api);
    await page.goto(PATH);
    await expect(page.getByRole("heading", { name: "Tệp bị mất", exact: true })).toBeVisible();
    if (vp.width >= 1000) {
      const link = page.locator("aside.side").getByRole("link", { name: "Tệp bị mất" });
      await expect(link).toHaveAttribute("aria-current", "page");
    }
    const rows = page.locator("tbody tr");
    await expect(rows).toHaveCount(2);
    for (const [i, code, id] of [
      [0, "SET0012", SET0012],
      [1, "DAM0123", DAM0123],
    ] as const) {
      const row = rows.nth(i);
      await expect(row.getByText("Ảnh sản phẩm")).toBeVisible();
      await expect(row.locator('td[data-label="Thuộc về"]')).toHaveText(code);
      await expect(row.getByRole("link", { name: code })).toHaveAttribute("href", `/san-pham/${id}`);
      await expect(row.locator('td[data-label="Phát hiện"]')).toHaveText("03:00 18/11");
      await expect(row.getByRole("link", { name: "Mở mẫu để tải lại" })).toHaveAttribute("href", `/san-pham/${id}`);
    }
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/tep-bi-mat-${String(vp.width)}.png`, fullPage: true });

    // Bấm hai lần liền: một yêu cầu, có khóa chống trùng, không thân.
    const btn = page.getByRole("button", { name: "Kiểm lại toàn bộ tệp" }).first();
    await btn.dblclick();
    await expect(page.getByText("Đang kiểm toàn bộ tệp…")).toBeVisible();
    await expect(page.getByText("Chạy nền, vài phút với vài nghìn tệp.", { exact: false })).toBeVisible();
    for (const b of await page.getByRole("button", { name: "Kiểm lại toàn bộ tệp" }).all()) {
      await expect(b).toBeDisabled();
    }
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]?.method).toBe("POST");
    expect(fake.writes[0]?.path).toBe("/api/v1/platform/files/verify");
    expect(fake.writes[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
    // Danh sách cũ vẫn hiện trong lúc kiểm.
    await expect(rows).toHaveCount(2);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/tep-bi-mat-dang-kiem-${String(vp.width)}.png`, fullPage: true });
  });

  test("không có tệp mất → trạng thái trống; Kiểm lại lỗi → câu báo, nút bấm lại được", async ({ page, api }) => {
    const fake = await lostApi(api, { items: [] });
    await page.goto(PATH);
    await expect(page.getByRole("heading", { name: "Không có tệp nào bị mất" })).toBeVisible();
    await expect(page.getByText('Bấm "Kiểm lại toàn bộ tệp" để kiểm ngay', { exact: false })).toBeVisible();
    await expect(page.locator("table")).toHaveCount(0);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/tep-bi-mat-trong-${String(vp.width)}.png`, fullPage: true });

    fake.failNext = { status: 403, code: "forbidden" };
    const btn = page.getByRole("button", { name: "Kiểm lại toàn bộ tệp" });
    await btn.click();
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(page.getByText("Đang kiểm toàn bộ tệp…")).toHaveCount(0);
    await expect(btn).toBeEnabled();
    await btn.click();
    await expect(page.getByText("Đang kiểm toàn bộ tệp…")).toBeVisible();
    expect(fake.writes).toHaveLength(2);
    // Lỗi dứt khoát (403) → lần bấm sau dùng khóa mới.
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  });
});

test("tệp không gắn mẫu: Thuộc về là —, không có nút; tên mục đích lạ hiện nguyên khóa", async ({ page, api }) => {
  await lostApi(api, {
    items: [lost(3, { purpose: "catalog_import" }), lost(4, { purpose: "test_export" }), ...TWO],
  });
  await page.goto(PATH);
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(4);
  await expect(rows.nth(0).getByText("Tệp nhập Excel")).toBeVisible();
  await expect(rows.nth(0).locator('td[data-label="Thuộc về"]')).toHaveText("—");
  await expect(rows.nth(0).getByRole("link")).toHaveCount(0);
  await expect(rows.nth(1).getByText("test_export")).toBeVisible();
});

test("danh sách lỗi (403) → câu lỗi, không có bảng", async ({ page, api }) => {
  await lostApi(api, { listStatus: 403 });
  await page.goto(PATH);
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.locator("table")).toHaveCount(0);
});

test("không có quyền platform.files.manage → không có mục Tệp bị mất trên menu", async ({ page, api }) => {
  await api.live();
  await api.catalog();
  await page.goto("/");
  await expect(page.locator("aside.side").getByRole("link").first()).toBeVisible();
  await expect(page.locator("aside.side").getByRole("link", { name: "Tệp bị mất" })).toHaveCount(0);
});
