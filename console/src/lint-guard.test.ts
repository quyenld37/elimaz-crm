// Cổng lint phải đỏ với `any`, ép kiểu `as` và fetch thẳng (kể cả globalThis.fetch, window.fetch) trong src/**
// (kế hoạch M0 Task 17, Step 4; M1 Task 48).
// Ghi một tệp mẫu tạm trong src/ (để projectService thấy nó qua tsconfig), lint, rồi xóa.
import { randomUUID } from "node:crypto";
import { rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";

const root = join(import.meta.dirname, "..");

async function lintSample(code: string): Promise<string[]> {
  const file = join(import.meta.dirname, `lint-guard-sample-${randomUUID()}.ts`);
  await writeFile(file, code);
  try {
    const [res] = await new ESLint({ cwd: root }).lintFiles([file]);
    return (res?.messages ?? []).map((m) => m.ruleId ?? m.message);
  } finally {
    await rm(file, { force: true });
  }
}

describe("lint của console", () => {
  it("any và ép kiểu as → đỏ", { timeout: 60_000 }, async () => {
    const rules = await lintSample("export function f(x: any): number {\n  return (x as number) + 1;\n}\n");
    expect(rules).toContain("@typescript-eslint/no-explicit-any");
    expect(rules).toContain("@typescript-eslint/consistent-type-assertions");
  });

  it("fetch thẳng ngoài src/realtime → đỏ", { timeout: 60_000 }, async () => {
    const rules = await lintSample('export const p: Promise<Response> = fetch("/api/v1/me");\n');
    expect(rules).toContain("no-restricted-globals");
  });

  it(
    "globalThis.fetch, window.fetch, self.fetch ngoài src/realtime → đỏ (ghi chú reviewer Task 17)",
    { timeout: 60_000 },
    async () => {
      for (const obj of ["globalThis", "window", "self"]) {
        const rules = await lintSample(`export const p: Promise<Response> = ${obj}.fetch("/api/v1/me");\n`);
        expect(rules, obj).toContain("no-restricted-properties");
      }
    },
  );

  it("mã sạch → không lỗi", { timeout: 60_000 }, async () => {
    const rules = await lintSample("export function f(x: number): number {\n  return x + 1;\n}\n");
    expect(rules).toEqual([]);
  });
});
