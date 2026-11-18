// Test trình duyệt của màn hình (kế hoạch M1 Task 48; lựa chọn: docs/chon-cong-cu-console.md).
// - Chromium chạy mọi spec; WebKit chạy spec có đuôi .phone.spec.ts (Safari trên điện thoại).
// - Kích thước: 1512 × 767 và 390 × 844 (e2e/fixtures.ts: DESKTOP, PHONE), spec tự chọn.
// - Vite dev ở CRM_E2E_PORT (mặc định 5148). Không dùng 4175, 4176, 4180, 4181. Mặc định API giả (e2e/fixtures.ts);
//   proxy /api của Vite trỏ CRM_API_URL (API dev 4190) cho spec chạy với API thật.
import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.CRM_E2E_PORT ?? "5148");
const forbidden = [4175, 4176, 4180, 4181];
if (forbidden.includes(port)) throw new Error(`cổng ${String(port)} dành cho website, chọn cổng khác`);

export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: process.env.CI !== undefined,
  retries: 0,
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${String(port)}`, trace: "retain-on-failure" },
  projects: [
    { name: "chromium", testMatch: /\.spec\.ts$/, use: { ...devices["Desktop Chrome"] } },
    { name: "webkit", testMatch: /\.phone\.spec\.ts$/, use: { ...devices["iPhone 13"] } },
  ],
  webServer: {
    command: `npx vite --port ${String(port)} --strictPort`,
    url: `http://127.0.0.1:${String(port)}`,
    reuseExistingServer: false,
    env: { CRM_VITE_PORT: String(port) },
  },
});
