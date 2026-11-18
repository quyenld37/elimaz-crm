// Màn Sức khỏe hệ thống (kế hoạch M1 Task 53; mockup docs/mockups/m1/suc-khoe.html, trạng thái A, B, C) ở 1512 × 767 và
// 390 × 844, API giả (e2e/fixtures.ts). Kiểm: worker nhịp sống 200 s → ô đỏ "Ngừng 3 phút"; cảnh báo gấp và thường
// phân biệt bằng chữ; tự tải lại mỗi 20 s và khi bấm "Tải lại ngay"; không quyền platform.health.read → không có mục
// menu, mở thẳng đường dẫn thấy "Không có quyền".
import type { Page, Route } from "@playwright/test";
import type { ErrorSummary, Grant, PlatformHealth } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const NOW = new Date("2026-11-18T07:32:05Z"); // 14:32:05 giờ Việt Nam
const HEALTH_GRANTS: Grant[] = [
  ...OWNER_GRANTS,
  { permission: "platform.health.read", scope: { type: "all", id: "" } },
];
const LONG_ERROR = "pgconn.ConnectError: failed to connect to host=crm-postgres-primary-very-long-hostname-for-test";

const err = (name: string, role: string, count: number, last: string): ErrorSummary => ({
  fingerprint: name,
  name,
  role,
  message: name,
  count,
  firstSeenAt: "2026-11-17T08:00:00Z",
  lastSeenAt: last,
});

/** Trạng thái A của mockup: worker ngừng (200 s), 2 việc hỏng, 1 việc không chắc đã gửi, 10 loại lỗi, 3 cảnh báo. */
const STATE_A: PlatformHealth = {
  processes: [
    { role: "api", lastHeartbeatAgeS: 4 },
    { role: "ingest", lastHeartbeatAgeS: 6 },
    { role: "worker", lastHeartbeatAgeS: 200 },
  ],
  queue: { oldestPendingAgeS: 45, pending: 12, dead: 2, inDoubt: 1 },
  ingest: { spoolFiles: 0 },
  backup: { lastBaseAgeH: 6, lastDrillAgeH: 5 * 24 + 2, lastDrillOk: true },
  errors: [
    err(LONG_ERROR, "worker", 1204, "2026-11-18T07:28:00Z"),
    err("imager: timeout", "worker", 37, "2026-11-18T07:27:00Z"),
    err("context deadline exceeded", "api", 5, "2026-11-18T06:02:00Z"),
    ...Array.from({ length: 7 }, (_, i) => err(`lỗi ${String(i)}`, "api", 1, "2026-11-18T05:00:00Z")),
  ],
  alerts: [
    { key: "worker-heartbeat", severity: "urgent", sentAt: "2026-11-18T07:29:00Z" },
    { key: "new-error:imager-timeout", severity: "normal", sentAt: "2026-11-18T04:40:00Z" },
    { key: "stock-shortage:KK-0007", severity: "normal", sentAt: "2026-11-17T09:40:00Z" },
  ],
};

/** Trạng thái B: mọi thứ bình thường, chưa có sao lưu, chưa diễn tập. */
const STATE_B: PlatformHealth = {
  ...STATE_A,
  processes: STATE_A.processes.map((p) => ({ ...p, lastHeartbeatAgeS: 5 })),
  queue: { oldestPendingAgeS: 0, pending: 0, dead: 0, inDoubt: 0 },
  backup: {},
  errors: [],
  alerts: [],
};

interface Fake {
  calls: number;
  body: PlatformHealth;
  status: number;
}

async function healthApi(api: FakeApi, body: PlatformHealth, grants: Grant[] = HEALTH_GRANTS): Promise<Fake> {
  const fake: Fake = { calls: 0, body, status: 200 };
  await api.live({ grants });
  await api.on("**/api/v1/platform/health", (r: Route) => {
    fake.calls++;
    // 403 không được thử lại (queryClient.shouldRetry): lỗi hiện ngay.
    if (fake.status !== 200) return json(r, fake.status, { error: "forbidden", request_id: "e2e" });
    return json(r, 200, fake.body);
  });
  return fake;
}

/** 1512, đồng hồ giả bắt đầu ở NOW, mở màn với trạng thái A và chờ ô worker đỏ. */
async function openStateA(page: Page, api: FakeApi): Promise<Fake> {
  await page.setViewportSize({ width: 1512, height: 767 });
  await page.clock.install({ time: NOW });
  const fake = await healthApi(api, STATE_A);
  await page.goto("/suc-khoe");
  await expect(page.getByRole("group", { name: "worker", exact: true })).toContainText("Ngừng 3 phút");
  return fake;
}

test.use({ timezoneId: "Asia/Ho_Chi_Minh", locale: "vi-VN" });

forEachViewport(() => {
  test("trạng thái A: worker ngừng 3 phút, việc hỏng, cảnh báo gấp và thường phân biệt bằng chữ", async ({
    page,
    api,
  }) => {
    // Giờ đứng yên ở NOW (bộ hẹn giờ vẫn chạy): "lần cuối" đúng từng giây.
    await page.clock.setFixedTime(NOW);
    await healthApi(api, STATE_A);
    await page.goto("/suc-khoe");

    await expect(page.getByRole("heading", { name: "Sức khỏe hệ thống", level: 1 })).toBeVisible();
    await expect(page.getByText("Tự cập nhật mỗi 20 giây · lần cuối 14:32:05")).toBeVisible();

    const banner = page.getByRole("alert");
    await expect(banner).toContainText("1 tiến trình ngừng: worker.");
    await expect(banner).toContainText("Đã gửi cảnh báo gấp lúc 14:29.");

    const worker = page.getByRole("group", { name: "worker", exact: true });
    await expect(worker).toHaveClass(/is-bad/);
    await expect(worker).toContainText("Ngừng 3 phút");
    await expect(worker).toContainText("Nhịp sống cuối 200 giây trước");
    const apiBox = page.getByRole("group", { name: "api", exact: true });
    await expect(apiBox).not.toHaveClass(/is-bad/);
    await expect(apiBox).toContainText("Đang chạy");
    await expect(apiBox).toContainText("Nhịp sống 4 giây trước");
    await expect(page.getByRole("group", { name: "ingest", exact: true })).toContainText("Đang chạy");
    // Thứ tự ô như mockup: api, worker, ingest.
    await expect(page.locator(".kpi[aria-label]")).toHaveText([/^api/, /^worker/, /^ingest/]);

    await expect(page.getByRole("group", { name: "Việc chờ lâu nhất" })).toContainText("45 giây");
    await expect(page.getByRole("group", { name: "Đang chờ" })).toContainText("12");
    const dead = page.getByRole("group", { name: "Hỏng (đã thử hết lần)" });
    await expect(dead).toHaveClass(/is-bad/);
    await expect(dead).toContainText("Cần người xem");
    const doubt = page.getByRole("group", { name: "Không chắc đã gửi" });
    await expect(doubt).toHaveClass(/is-bad/);
    await expect(doubt).toContainText("Không tự gửi lại; cần tra ở bên nhận");

    await expect(page.getByRole("group", { name: "Tin chờ ghi (ingest)" })).toContainText("0");
    await expect(page.getByRole("group", { name: "Sao lưu đầy đủ gần nhất" })).toContainText("6 giờ");
    const drill = page.getByRole("group", { name: "Diễn tập khôi phục gần nhất" });
    await expect(drill).toContainText("5 ngày");
    await expect(drill).toContainText("Đạt");

    const errors = page.getByRole("region", { name: "Lỗi nhiều nhất 24 giờ" });
    await expect(errors.getByRole("listitem")).toHaveCount(3);
    await expect(errors.getByRole("listitem").first()).toContainText("1.204 lần");
    await expect(errors.getByRole("listitem").first()).toContainText("worker · lần cuối 14:28");
    await expect(errors.getByRole("link", { name: "Xem đủ 10 loại" })).toHaveAttribute("href", "/su-kien-loi");

    const alerts = page.getByRole("region", { name: "Cảnh báo đã gửi 24 giờ" });
    const items = alerts.getByRole("listitem");
    await expect(items).toHaveCount(3);
    await expect(items.nth(0)).toContainText("worker-heartbeat");
    await expect(items.nth(0)).toContainText("14:29 18/11");
    await expect(items.nth(0)).toContainText("Gấp");
    await expect(items.nth(1)).toContainText("Thường");
    await expect(items.nth(2)).toContainText("16:40 17/11");
    await expect(items.nth(2)).toContainText("Thường");
    await expect(alerts).toContainText("Gấp: cần xử lý ngay. Thường: xem trong ngày.");

    await noHorizontalScroll(page);
  });

  test("trạng thái B: mọi thứ bình thường, chưa có sao lưu", async ({ page, api }) => {
    await healthApi(api, STATE_B);
    await page.goto("/suc-khoe");
    await expect(page.getByText("Mọi tiến trình đang chạy.")).toBeVisible();
    await expect(page.getByText("Không có việc hỏng, không có cảnh báo trong 24 giờ.")).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
    for (const role of ["api", "worker", "ingest"]) {
      const box = page.getByRole("group", { name: role, exact: true });
      await expect(box).toContainText("Đang chạy");
      await expect(box).not.toHaveClass(/is-bad/);
    }
    await expect(page.getByRole("group", { name: "Hỏng / Không chắc đã gửi" })).toContainText("0 / 0");
    const backup = page.getByRole("group", { name: "Sao lưu đầy đủ gần nhất" });
    await expect(backup).toContainText("—");
    await expect(backup).toContainText("Chưa có bản nào");
    await expect(page.getByRole("group", { name: "Diễn tập khôi phục gần nhất" })).toContainText("Chưa diễn tập");
    await expect(page.getByRole("region", { name: "Cảnh báo đã gửi 24 giờ" })).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  test("không có quyền: không có mục menu, mở thẳng đường dẫn thấy Không có quyền", async ({ page, api }) => {
    await api.live({ grants: OWNER_GRANTS });
    await page.goto("/suc-khoe");
    await expect(page.getByRole("heading", { name: "Không có quyền" })).toBeVisible();
    await expect(
      page.getByText("Tài khoản của bạn không được xem Sức khỏe hệ thống. Cần thì nhờ Chủ shop cấp quyền."),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Về trang Danh sách sản phẩm" })).toHaveAttribute("href", "/san-pham");
    await expect(page.getByRole("link", { name: "Sức khỏe hệ thống" })).toHaveCount(0);
    await noHorizontalScroll(page);
  });
});

test("có quyền: mục Sức khỏe hệ thống trong nhóm Hệ thống của menu", async ({ page, api }) => {
  await page.setViewportSize({ width: 1512, height: 767 });
  await healthApi(api, STATE_B);
  await page.goto("/suc-khoe");
  const nav = page.getByRole("navigation", { name: "Menu chính" });
  await expect(nav.getByText("Hệ thống", { exact: true })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Sức khỏe hệ thống" })).toHaveAttribute("aria-current", "page");
});

test("tự tải lại mỗi 20 giây và khi bấm Tải lại ngay; worker chạy lại thì ô hết đỏ", async ({ page, api }) => {
  const fake = await openStateA(page, api);
  const first = fake.calls;

  fake.body = STATE_B;
  await page.clock.fastForward(20_000);
  await expect(page.getByRole("group", { name: "worker", exact: true })).toContainText("Đang chạy");
  await expect(page.getByText("Mọi tiến trình đang chạy.")).toBeVisible();
  await expect(page.getByText(/lần cuối 14:32:2\d$/)).toBeVisible();
  // Lưới an toàn resync của lớp realtime cũng có thể tải lại cùng lúc: ít nhất một lần.
  expect(fake.calls).toBeGreaterThan(first);
  const second = fake.calls;

  fake.body = STATE_A;
  await page.getByRole("button", { name: "Tải lại ngay" }).click();
  await expect(page.getByRole("group", { name: "worker", exact: true })).toContainText("Ngừng 3 phút");
  expect(fake.calls).toBeGreaterThan(second);
});

test("API lỗi: hiện hộp lỗi, giữ số đã có", async ({ page, api }) => {
  const fake = await openStateA(page, api);
  fake.status = 403;
  await page.getByRole("button", { name: "Tải lại ngay" }).click();
  await expect(page.locator(".box-err", { hasText: "Không tải lại được" })).toBeVisible();
  await expect(page.getByRole("group", { name: "worker", exact: true })).toContainText("Ngừng 3 phút");
});

test("API lỗi ngay lần đầu: hộp lỗi, không có số", async ({ page, api }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fake = await healthApi(api, STATE_A);
  fake.status = 403;
  await page.goto("/suc-khoe");
  await expect(page.locator(".box-err")).toBeVisible();
  await expect(page.getByRole("group", { name: "worker", exact: true })).toHaveCount(0);
});
