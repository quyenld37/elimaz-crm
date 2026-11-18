import { describe, expect, it } from "vitest";
import type { Supplier } from "../../api";
import { canQuickAdd, formatPhone, noteCount, normalizePhone, phoneValid } from "./supplierRules";

const s = (name: string): Supplier => ({ id: name, name, phone: "", note: "", status: "active", version: 1 });

describe("supplierRules", () => {
  it.each([
    ["", true],
    ["0912 345 678", true],
    ["0912.345-678", true],
    ["+84 24 3936 1234", true],
    ["09123", false],
    ["12ab5678", false],
    ["+1234567890123456", false],
    ["++12345678", false],
  ])("số điện thoại %j hợp lệ: %s (cùng luật máy chủ)", (p, ok) => {
    expect(phoneValid(p)).toBe(ok);
  });

  it("chuẩn hóa và hiện số", () => {
    expect(normalizePhone("0912 345.678-")).toBe("0912345678");
    expect(formatPhone("0912345678")).toBe("0912 345 678");
    expect(formatPhone("+842439361234")).toBe("+842439361234");
    expect(formatPhone("")).toBe("");
  });

  it("đếm ghi chú theo ký tự", () => {
    expect(noteCount("")).toBe("0 / 1.000 ký tự");
    expect(noteCount("Lụa 😀")).toBe("5 / 1.000 ký tự");
  });

  it("thêm nhanh chỉ khi tên gõ chưa trùng hẳn tên có trong danh sách", () => {
    const list = [s("Xưởng may Hoa Sen")];
    expect(canQuickAdd("Xưởng Hoa", list)).toBe(true);
    expect(canQuickAdd("  Xưởng may Hoa Sen ", list)).toBe(false);
    expect(canQuickAdd("   ", list)).toBe(false);
    expect(canQuickAdd("x".repeat(201), [])).toBe(false);
  });
});
