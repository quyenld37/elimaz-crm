// Màn Sự kiện lỗi (kế hoạch M1 Task 330; mockup docs/mockups/m1/su-kien-loi.html) ở 1512 × 767, 390 × 844 và 360,
// API giả (e2e/fixtures.ts). Kiểm: đủ 10 loại lỗi, nội dung dài 300 ký tự cắt 2 dòng và bấm thì mở đủ mà không tràn
// ngang, trạng thái trống, không quyền platform.health.read → không có mục menu, URL trực tiếp không mở màn.
import type { Page } from "@playwright/test";
import type { ErrorSummary, Grant, PlatformHealth } from "../src/api";
import { expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const PATH = "/he-thong/su-kien-loi";
const HEALTH_GRANTS: Grant[] = ["realtime.subscribe", "platform.health.read"].map((permission) => ({
  permission,
  scope: { type: "all", id: "" },
}));

// 300 ký tự (giới hạn maxLength của ErrorSummary.message), có một chuỗi dài không dấu cách.
const LONG_MESSAGE = (
  'ERROR: duplicate key value violates unique constraint "variants_tenant_id_seller_id_sku_key" (SQLSTATE 23505) ' +
  "while applying catalog import plan batch 3 of 5; request_id=01JD8Q2K7Y; detail: Key=" +
  "AO01-DEN-S-".repeat(20)
).slice(0, 300);

function e(fp: string, name: string, role: string, count: number, message: string): ErrorSummary {
  return {
    fingerprint: `${fp}0011223344556677`,
    name,
    role,
    message,
    count,
    firstSeenAt: "2026-11-17T23:12:00Z",
    lastSeenAt: "2026-11-18T07:28:00Z",
  };
}

// Mười dòng của trạng thái A trong mockup.
const ERRORS: ErrorSummary[] = [
  e(
    "a3f9c1",
    "pgconn.ConnectError",
    "worker",
    1204,
    "failed to connect to `host=postgres user=crm_app database=crm`: dial error",
  ),
  e("7be210", "imager: timeout", "worker", 37, "imager request timed out after 30s for file [đã che]"),
  e("0c4d88", "catalog import: unique violation", "worker", 9, LONG_MESSAGE),
  e("91aa07", "context deadline exceeded", "api", 5, "GET /api/v1/catalog/products: context deadline exceeded"),
  e("55e1b3", "realtime: client write failed", "api", 4, "write: broken pipe"),
  e("e02f6c", "commands: in_doubt", "worker", 3, "fakecarrier create shipment: response not received within 30s"),
  e("4d7a19", "files: head object not found", "worker", 2, "verify: object orig/[đã che] not found"),
  e("b81c3e", "panic: index out of range", "api", 2, "runtime error: index out of range [3] with length 3"),
  e("cc0918", "ingest: spool fsync", "ingest", 1, "fsync /var/lib/crm/spool/000123.ndjson: no space left on device"),
  e("3f6b72", "idempotency: request_in_progress", "api", 1, "key already being processed by another request"),
];

function health(errors: ErrorSummary[]): PlatformHealth {
  return {
    processes: [],
    queue: { oldestPendingAgeS: 0, pending: 0, dead: 0, inDoubt: 0 },
    ingest: { spoolFiles: 0 },
    backup: {},
    errors,
    alerts: [],
  };
}

async function healthApi(api: FakeApi, errors: ErrorSummary[]): Promise<{ calls: number }> {
  const log = { calls: 0 };
  await api.live({ grants: HEALTH_GRANTS });
  await api.on("**/api/v1/platform/health", (r) => {
    log.calls++;
    return json(r, 200, health(errors));
  });
  return log;
}

const errorRows = (page: Page) => page.locator("table.tbl tbody tr");

forEachViewport((vp) => {
  test("đủ 10 loại lỗi; nội dung 300 ký tự cắt 2 dòng, bấm thì mở đủ, không tràn ngang", async ({ page, api }) => {
    await healthApi(api, ERRORS);
    await page.goto(PATH);
    await expect(page.getByRole("heading", { level: 1, name: "Sự kiện lỗi" })).toBeVisible();
    await expect(errorRows(page)).toHaveCount(10);
    for (const [i, x] of ERRORS.entries()) {
      await expect(errorRows(page).nth(i)).toContainText(x.name);
      await expect(errorRows(page).nth(i)).toContainText(x.fingerprint.slice(0, 6));
    }
    const first = errorRows(page).first();
    await expect(first).toContainText("1.204");
    await expect(first).toContainText("06:12 18/11");
    await expect(first).toContainText("14:28 18/11");
    await expect(first).toContainText("worker");

    const row = errorRows(page).nth(2);
    const summary = row.locator("summary");
    const lineHeight = await summary
      .locator("span")
      .evaluate((el) => parseFloat(getComputedStyle(el).lineHeight) || 20);
    const clamped = await summary.locator("span").boundingBox();
    expect(clamped?.height ?? 0).toBeLessThanOrEqual(lineHeight * 2 + 1);
    await noHorizontalScroll(page);

    await summary.click();
    const full = row.locator("details p");
    await expect(full).toBeVisible();
    await expect(full).toHaveText(LONG_MESSAGE);
    const box = await full.boundingBox();
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(vp.width);
    await noHorizontalScroll(page);
  });

  test("không có lỗi → trạng thái trống có giờ kiểm", async ({ page, api }) => {
    await healthApi(api, []);
    await page.goto(PATH);
    await expect(page.getByRole("heading", { name: "Không có lỗi nào trong 24 giờ qua" })).toBeVisible();
    await expect(page.getByText(/^Lần kiểm \d{2}:\d{2}:\d{2}\.$/)).toBeVisible();
    await expect(page.locator("table")).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  test("không quyền platform.health.read → không có mục menu, URL trực tiếp không mở màn, không gọi API", async ({
    page,
    api,
  }) => {
    await api.live({ grants: [{ permission: "realtime.subscribe", scope: { type: "all", id: "" } }] });
    await page.goto(PATH);
    await expect(page.getByText("Không có trang này, hoặc bạn chưa được cấp quyền xem")).toBeVisible();
    await expect(page.getByRole("link", { name: "Sự kiện lỗi" })).toHaveCount(0);
    // apiGuard của fixtures làm test đỏ nếu màn gọi /api/v1/platform/health (đường chưa khai).
  });
});

test.describe("360 × 740", () => {
  test.use({ viewport: { width: 360, height: 740 } });
  test("điện thoại hẹp: thẻ lỗi không tràn ngang, kể cả khi mở nội dung dài", async ({ page, api }) => {
    await healthApi(api, ERRORS);
    await page.goto(PATH);
    await expect(errorRows(page)).toHaveCount(10);
    await noHorizontalScroll(page);
    await errorRows(page).nth(2).locator("summary").click();
    await expect(errorRows(page).nth(2).locator("details p")).toBeVisible();
    await noHorizontalScroll(page);
  });
});

test("có quyền → mục Sự kiện lỗi trong nhóm Hệ thống của menu", async ({ page, api }) => {
  await healthApi(api, []);
  await page.goto(PATH);
  await expect(page.getByRole("link", { name: "Sự kiện lỗi" })).toBeVisible();
  await expect(page.getByText("Hệ thống", { exact: true })).toBeVisible();
});

test("tự tải lại mỗi 20 giây", async ({ page, api }) => {
  await page.clock.install();
  const log = await healthApi(api, []);
  await page.goto(PATH);
  await expect(page.getByRole("heading", { name: "Không có lỗi nào trong 24 giờ qua" })).toBeVisible();
  const before = log.calls;
  await page.clock.runFor(20_000);
  await expect.poll(() => log.calls).toBeGreaterThan(before);
});
