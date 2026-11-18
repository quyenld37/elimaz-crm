// Bản dựng: tệp tĩnh trong dist/ (container crm-console chỉ phục vụ tệp tĩnh, ADR-34; CSP ở docs/RUNBOOK.md).
// Dev: cổng 4192 theo kế hoạch (CRM_VITE_PORT để đổi khi nhiều phiên chạy song song), chuyển /api và /health
// sang api ở 4190 (CRM_API_URL để đổi). Không dùng 4175, 4176, 4180, 4181.
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const apiURL = process.env.CRM_API_URL ?? "http://127.0.0.1:4190";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: Number(process.env.CRM_VITE_PORT ?? "4192"),
    strictPort: true,
    proxy: { "/api": apiURL, "/health": apiURL },
  },
  build: { outDir: "dist", sourcemap: false },
  test: {
    environment: "node",
    restoreMocks: true,
    setupFiles: ["src/test-setup.ts"],
  },
});
