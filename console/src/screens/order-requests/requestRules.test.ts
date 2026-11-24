import { describe, expect, it } from "vitest";
import { messageError, nextStatuses, runeCount, shipBackErrors, vnToday } from "./requestRules";

describe("đếm ký tự như máy chủ (rune)", () => {
  it("emoji, chữ có dấu là 1 ký tự", () => {
    expect(runeCount("ạ")).toBe(1);
    expect(runeCount("😀😀")).toBe(2);
  });

  it("lời nhắn 5 emoji hợp lệ; 1000 emoji hợp lệ, 1001 thì quá", () => {
    expect(messageError("😀".repeat(5), true)).toBeNull();
    expect(messageError("😀".repeat(1000), true)).toBeNull();
    expect(messageError("😀".repeat(1001), true)).toBe("Lời nhắn tối đa 1000 ký tự.");
    expect(messageError("   ", true)).toBe("Cần nhập lời nhắn cho khách.");
    expect(messageError("   ", false)).toBeNull();
  });

  it("mã gói 3 emoji hợp lệ; địa chỉ 300 ký tự có dấu hợp lệ, 301 thì quá", () => {
    const ok = { address: "ạ".repeat(300), packageCode: "😀😀😀", deadline: "2026-11-21" };
    expect(shipBackErrors(ok, "2026-11-21")).toEqual({ address: null, packageCode: null, deadline: null });
    expect(shipBackErrors({ ...ok, address: "ạ".repeat(301) }, "2026-11-21").address).not.toBeNull();
  });
});

describe("trạng thái kế tiếp", () => {
  it("đã đóng thì không còn gì; Đã xác nhận đổi chỉ cho đổi hàng", () => {
    expect(nextStatuses("exchange", "resolved")).toEqual([]);
    expect(nextStatuses("return", "rejected")).toEqual([]);
    expect(nextStatuses("question", "cancelled")).toEqual([]);
    expect(nextStatuses("exchange", "in_review")).toContain("confirmed");
    expect(nextStatuses("cancel", "received")).not.toContain("confirmed");
  });
});

describe("ngày hôm nay giờ Việt Nam", () => {
  it("17:30 UTC đã là ngày hôm sau ở Việt Nam", () => {
    expect(vnToday(Date.parse("2026-11-20T17:30:00Z"))).toBe("2026-11-21");
    expect(vnToday(Date.parse("2026-11-20T16:59:00Z"))).toBe("2026-11-20");
  });
});
