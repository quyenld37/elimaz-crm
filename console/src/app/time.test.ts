// Giờ hiện trên màn hình (src/app/time.ts; chuyển từ test của màn Sự kiện lỗi, Task 330).
import { describe, expect, it } from "vitest";
import { shortTime } from "./time";

describe("shortTime", () => {
  it("ra HH:mm dd/MM theo giờ Việt Nam (UTC+7)", () => {
    expect(shortTime("2026-11-17T23:12:00Z")).toBe("06:12 18/11");
    expect(shortTime("2026-11-18T07:28:59Z")).toBe("14:28 18/11");
  });
  it("qua nửa đêm giờ Việt Nam đổi ngày", () => {
    expect(shortTime("2026-11-18T17:00:00Z")).toBe("00:00 19/11");
  });
  it("chuỗi không phải giờ thì trả rỗng, không ném", () => {
    expect(shortTime("không phải giờ")).toBe("");
  });
});
