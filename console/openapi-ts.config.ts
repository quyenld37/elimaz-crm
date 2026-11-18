// Sinh client TypeScript từ hợp đồng API (nguồn duy nhất: ../openapi/openapi.yaml). Chạy bằng `make gen`
// (hoặc `npm run gen`); không sửa tay console/src/api/**. Lựa chọn công cụ: docs/chon-cong-cu-console.md.
import { defineConfig } from "@hey-api/openapi-ts";

export default defineConfig({
  input: "../openapi/openapi.yaml",
  output: { path: "src/api", postProcess: ["prettier"] },
  plugins: ["@hey-api/client-fetch", "@hey-api/typescript", "@hey-api/sdk"],
});
