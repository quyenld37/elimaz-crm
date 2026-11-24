// Giờ hiện trên màn hình (src/app/time.ts; chuyển từ test của màn Sự kiện lỗi, Task 330).
import { describe, expect, it } from "vitest";
import { fullTime, shortTime } from "./time";

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

describe("fullTime", () => {
  it("ra HH:mm · dd/MM/yyyy theo giờ Việt Nam, qua nửa đêm đổi ngày, sang năm", () => {
    expect(fullTime("2026-11-21T06:05:00Z")).toBe("13:05 · 21/11/2026");
    expect(fullTime("2026-12-31T17:30:00Z")).toBe("00:30 · 01/01/2027");
  });
  it("chuỗi không phải giờ thì trả rỗng", () => {
    expect(fullTime("x")).toBe("");
  });
});
