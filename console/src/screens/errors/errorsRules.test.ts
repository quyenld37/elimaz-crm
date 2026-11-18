// Luật hiện của màn Sự kiện lỗi (kế hoạch M1 Task 330): giờ theo múi Việt Nam, số lần có dấu chấm nghìn, mã nhận
// diện 6 ký tự đầu.
import { describe, expect, it } from "vitest";
import { clockTime, countText, shortId } from "./errorsRules";

describe("clockTime", () => {
  it("ra HH:mm:ss theo giờ Việt Nam", () => {
    expect(clockTime(Date.parse("2026-11-18T07:32:05Z"))).toBe("14:32:05");
  });
});

describe("countText", () => {
  it("dấu chấm ngăn nghìn như mockup", () => {
    expect(countText(1204)).toBe("1.204");
    expect(countText(9)).toBe("9");
    expect(countText(1234567)).toBe("1.234.567");
  });
});

describe("shortId", () => {
  it("6 ký tự đầu của dấu vân tay", () => {
    expect(shortId("a3f9c1d2e4b5")).toBe("a3f9c1");
    expect(shortId("abc")).toBe("abc");
  });
});
