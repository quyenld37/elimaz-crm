// Lint của màn hình (kế hoạch M0 Task 17): typescript-eslint strict-type-checked + stylistic-type-checked qua
// projectService, react-hooks; cấm any và ép kiểu trong src/** (trừ test); chỉ lớp realtime được gọi fetch thẳng
// (cả fetch, globalThis.fetch, window.fetch, self.fetch).
// typescript-eslint đọc kiểu qua API TypeScript 6 (gói `typescript` là bí danh của @typescript/typescript6);
// cổng kiểu `tsc --noEmit` chạy TypeScript 7.0.2. Lý do: docs/chon-cong-cu-console.md.
import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/**", "src/api/**", "node_modules/**"] },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  reactHooks.configs.flat.recommended,
  {
    languageOptions: {
      globals: { ...globals.browser },
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/**/*.test.{ts,tsx}"],
    rules: {
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/consistent-type-assertions": ["error", { assertionStyle: "never" }],
      "@typescript-eslint/no-non-null-assertion": "error",
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/realtime/**", "src/**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-globals": [
        "error",
        {
          name: "fetch",
          message: "Gọi API qua client sinh trong src/api (luật console 2); chỉ src/realtime được fetch thẳng.",
        },
        {
          name: "process",
          message: "Mã chạy trên trình duyệt không có process; cấu hình dựng đọc bằng import.meta.env.",
        },
      ],
      // Ghi chú reviewer Task 17: fetch qua đối tượng toàn cục cũng bị chặn (M1 Task 48).
      "no-restricted-properties": [
        "error",
        ...["globalThis", "window", "self"].map((object) => ({
          object,
          property: "fetch",
          message: "Gọi API qua client sinh trong src/api (luật console 2); chỉ src/realtime được fetch thẳng.",
        })),
      ],
    },
  },
  {
    files: ["**/*.js"],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: { globals: { ...globals.node } },
  },
);
