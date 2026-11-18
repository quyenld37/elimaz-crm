// Bảng câu lỗi tiếng Việt (kế hoạch M1 Task 48): mọi mã trong x-error-codes của openapi.yaml có câu; đổi kết quả
// của client sinh thành dữ liệu hoặc ApiError, NetworkError.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  ApiError,
  ERROR_MESSAGES,
  NetworkError,
  TEXT_NETWORK,
  TEXT_UNKNOWN_ERROR,
  errorMessage,
  unwrap,
} from "./errors";

function contractCodes(): string[] {
  const text = readFileSync(join(import.meta.dirname, "../../../openapi/openapi.yaml"), "utf8");
  const lines = text.split("\n");
  const start = lines.findIndex((l) => /^\s+x-error-codes:\s*$/.test(l));
  expect(start, "openapi.yaml thiếu x-error-codes").toBeGreaterThan(0);
  const codes: string[] = [];
  for (const l of lines.slice(start + 1)) {
    if (/^\s+#/.test(l)) continue; // dòng chú thích nhóm
    const m = /^\s+- ([a-z_]+)\s*(#.*)?$/.exec(l);
    if (m?.[1] === undefined) break;
    codes.push(m[1]);
  }
  return codes;
}

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", ...headers } });

describe("ERROR_MESSAGES", () => {
  it("có câu cho mọi mã lỗi trong openapi.yaml, không thừa mã", () => {
    const codes = contractCodes();
    expect(codes.length).toBeGreaterThan(40);
    const missing = codes.filter((c) => !(c in ERROR_MESSAGES));
    expect(missing).toEqual([]);
    const extra = Object.keys(ERROR_MESSAGES).filter((c) => !codes.includes(c));
    expect(extra).toEqual([]);
  });

  it("câu không rỗng, không lộ mã máy, kết thúc không có dấu chấm thừa", () => {
    for (const [code, text] of Object.entries(ERROR_MESSAGES)) {
      expect(text.trim().length, code).toBeGreaterThan(5);
      expect(text, code).not.toContain(code);
      expect(text.endsWith(".."), code).toBe(false);
    }
  });

  it("M1: các mã của kế hoạch có trong hợp đồng", () => {
    const codes = contractCodes();
    for (const c of [
      "out_of_stock",
      "insufficient_stock",
      "sku_taken",
      "version_conflict",
      "retry_later",
      "file_too_large",
    ])
      expect(codes).toContain(c);
  });
});

describe("errorMessage", () => {
  it("out_of_stock kèm available → Không đủ hàng: còn 2", () => {
    const e = new ApiError({ status: 409, code: "out_of_stock", details: { available: 2, variantId: "v" } });
    expect(errorMessage(e)).toBe("Không đủ hàng: còn 2");
  });

  it("thiếu số available → bỏ phần số, không in {available}", () => {
    const e = new ApiError({ status: 409, code: "insufficient_stock", details: {} });
    expect(errorMessage(e)).not.toContain("{");
    expect(errorMessage(e)).toMatch(/^Không đủ hàng/);
  });

  it("available không phải số (chữ từ máy chủ) → không chèn", () => {
    const e = new ApiError({ status: 409, code: "out_of_stock", details: { available: "<b>x</b>" } });
    expect(errorMessage(e)).not.toContain("<b>");
  });

  it("mã lạ → câu chung kèm mã yêu cầu để tra log", () => {
    const e = new ApiError({ status: 418, code: "teapot", requestId: "req-1", details: {} });
    expect(errorMessage(e)).toBe(`${TEXT_UNKNOWN_ERROR} (mã yêu cầu req-1)`);
  });

  it("lỗi mạng → câu mất kết nối", () => {
    expect(errorMessage(new NetworkError())).toBe(TEXT_NETWORK);
  });

  it("lỗi không phải của API → câu chung", () => {
    expect(errorMessage(new Error("boom"))).toBe(TEXT_UNKNOWN_ERROR);
  });
});

describe("unwrap", () => {
  it("2xx → dữ liệu", async () => {
    const data = await unwrap(
      Promise.resolve({ data: { id: "1" }, error: undefined, response: json(200, { id: "1" }) }),
    );
    expect(data).toEqual({ id: "1" });
  });

  it("4xx → ApiError có status, code, request_id, trường thêm", async () => {
    const body = { error: "out_of_stock", request_id: "r9", available: 1, variantId: "v1" };
    const p = unwrap(Promise.resolve({ data: undefined, error: body, response: json(409, body) }));
    await expect(p).rejects.toBeInstanceOf(ApiError);
    const e: unknown = await p.catch((x: unknown) => x);
    expect(e).toMatchObject({ status: 409, code: "out_of_stock", requestId: "r9", details: { available: 1 } });
  });

  it("503 Retry-After: 2 → retryAfterMs 2000", async () => {
    const body = { error: "retry_later", request_id: "r" };
    const e: unknown = await unwrap(
      Promise.resolve({ data: undefined, error: body, response: json(503, body, { "retry-after": "2" }) }),
    ).catch((x: unknown) => x);
    expect(e).toMatchObject({ status: 503, code: "retry_later", retryAfterMs: 2000 });
  });

  it("thân lỗi không phải JSON chuẩn (cổng vào 502) → ApiError mã rỗng, status đúng", async () => {
    const e: unknown = await unwrap(
      Promise.resolve({
        data: undefined,
        error: "Bad gateway",
        response: new Response("Bad gateway", { status: 502 }),
      }),
    ).catch((x: unknown) => x);
    expect(e).toMatchObject({ status: 502, code: "" });
  });

  it("2xx nhưng đọc thân lỗi (mạng rớt giữa chừng, thân không phải JSON) → NetworkError, không phải lỗi dứt khoát", async () => {
    const e: unknown = await unwrap(
      Promise.resolve({ data: undefined, error: new SyntaxError("Unexpected end"), response: json(201, {}) }),
    ).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(NetworkError);
  });

  it("mã trùng tên thuộc tính của Object (constructor) → câu chung, không ném", () => {
    const e = new ApiError({ status: 400, code: "constructor", details: {} });
    expect(errorMessage(e)).toBe(TEXT_UNKNOWN_ERROR);
  });

  it("không có response (fetch ném lỗi) → NetworkError", async () => {
    const e: unknown = await unwrap(
      Promise.resolve({ data: undefined, error: new TypeError("Failed to fetch"), response: undefined }),
    ).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(NetworkError);
  });

  it("lời hứa bị từ chối bởi fetch → NetworkError", async () => {
    const e: unknown = await unwrap(Promise.reject(new TypeError("Failed to fetch"))).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(NetworkError);
  });
});
