// Fixture chung của test trình duyệt (kế hoạch M1 Task 48).
//
// `apiGuard` (tự bật cho mọi test): mọi yêu cầu /api/** và /health/** chưa được khai bị trả 501 và làm test đỏ, kể cả
// spec quên dùng `api` (không lặng lẽ đi qua proxy của Vite tới API dev). Chỉ tắt khi CRM_E2E_REAL_API=1: spec chạy với
// API dev thật (4190, dữ liệu seed-dev); đường đó còn cần thẻ Cloudflare Access cho máy dev (docs/tasks/task-48.md,
// "Việc phát hiện thêm").
// `api`: API giả bằng page.route, đúng kiểu của client sinh (src/api/types.gen.ts).
import { test as base, expect, type Route } from "@playwright/test";
import type { Grant, Me } from "../src/api";

/** Laptop của chủ shop và điện thoại (mockup M1). */
export const DESKTOP = { width: 1512, height: 767 } as const;
export const PHONE = { width: 390, height: 844 } as const;

export interface FakeApi {
  /** API sống: /health/live 200, /api/v1/me trả `me`, luồng realtime mở và im (không băng báo). */
  live(me?: Partial<Me>): Promise<void>;
  /** API tắt: mọi yêu cầu tới /health và /api lỗi mạng. */
  down(): Promise<void>;
  /** Khai thêm một đường (ghi đè đường cùng mẫu đã khai). */
  on(pattern: string, handler: (route: Route) => Promise<void> | void): Promise<void>;
  /** Yêu cầu tới API chưa được khai. */
  readonly unexpected: string[];
}

export const OWNER_GRANTS: Grant[] = ["realtime.subscribe", "catalog.read", "inventory.read"].map((permission) => ({
  permission,
  scope: { type: "all", id: "" },
}));

// Chỉ đường của API (/api/…, /health/…), không bắt tệp mã do Vite phục vụ như /src/api/client.gen.ts.
const isApi = (url: URL) => /^\/(api|health)\//.test(url.pathname);

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

const realApi = process.env.CRM_E2E_REAL_API === "1";

export const test = base.extend<{ apiGuard: string[]; api: FakeApi }>({
  apiGuard: [
    async ({ page }, provide) => {
      const unexpected: string[] = [];
      if (!realApi) {
        // Đăng ký trước nên được gọi sau cùng (Playwright gọi route khai sau trước): chỉ bắt đường chưa khai.
        await page.route(isApi, async (route) => {
          unexpected.push(`${route.request().method()} ${new URL(route.request().url()).pathname}`);
          await json(route, 501, { error: "not_implemented", request_id: "e2e" });
        });
      }
      await provide(unexpected);
      expect(unexpected, "yêu cầu tới API chưa khai trong test").toEqual([]);
    },
    { auto: true },
  ],
  api: async ({ page, apiGuard }, provide) => {
    if (realApi) throw new Error("CRM_E2E_REAL_API=1: spec này dùng API giả, chạy không đặt biến đó");
    const unexpected = apiGuard;
    const api: FakeApi = {
      unexpected,
      async live(me) {
        const body: Me = {
          userId: "0195f3b2-0000-7000-8000-000000000001",
          email: "lan@elimaz.vn",
          grants: OWNER_GRANTS,
          ...me,
        };
        await page.route("**/health/live", (r) => json(r, 200, { status: "ok" }));
        await page.route("**/api/v1/me", (r) => json(r, 200, body));
        // Luồng mở và im: không trả lời (watchdog của lớp realtime là 60 s, dài hơn mọi test).
        await page.route("**/api/v1/stream", () => undefined);
      },
      async down() {
        await page.route(isApi, (r) => r.abort("connectionrefused"));
      },
      async on(pattern, handler) {
        await page.route(pattern, handler);
      },
    };
    await provide(api);
  },
});

export { expect };
