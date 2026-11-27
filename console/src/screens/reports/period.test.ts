// Luật chọn kỳ của báo cáo (Task 239, M6-16; mockup tong-quan.html trạng thái A–C): kỳ mẫu tính theo ngày Việt Nam,
// khoảng tối đa 366 ngày tính cả hai đầu, không chọn ngày tương lai.
import { describe, expect, it } from "vitest";
import { daysBetween, parsePeriod, periodError, presetPeriod, vnToday, writePeriod } from "./period";

const TODAY = "2026-11-27";

describe("vnToday", () => {
  it("lấy ngày theo giờ Việt Nam, không theo UTC", () => {
    expect(vnToday(new Date("2026-11-26T18:30:00Z"))).toBe("2026-11-27");
    expect(vnToday(new Date("2026-11-27T16:59:00Z"))).toBe("2026-11-27");
  });
});

describe("presetPeriod", () => {
  it("Hôm nay, Hôm qua, 7 ngày (gồm hôm nay), Tháng này", () => {
    expect(presetPeriod("today", TODAY)).toEqual({ preset: "today", from: TODAY, to: TODAY });
    expect(presetPeriod("yesterday", TODAY)).toEqual({ preset: "yesterday", from: "2026-11-26", to: "2026-11-26" });
    expect(presetPeriod("7d", TODAY)).toEqual({ preset: "7d", from: "2026-11-21", to: TODAY });
    expect(presetPeriod("month", TODAY)).toEqual({ preset: "month", from: "2026-11-01", to: TODAY });
  });
  it("qua ranh tháng và năm", () => {
    expect(presetPeriod("yesterday", "2026-01-01").from).toBe("2025-12-31");
    expect(presetPeriod("7d", "2026-01-03").from).toBe("2025-12-28");
  });
});

describe("periodError", () => {
  it("hợp lệ → null", () => {
    expect(periodError({ from: "2026-11-01", to: TODAY }, TODAY)).toBeNull();
    expect(periodError({ from: "2025-11-27", to: TODAY }, TODAY)).toBeNull(); // đúng 366 ngày
  });
  it("quá 366 ngày → nêu số ngày đang chọn", () => {
    expect(periodError({ from: "2025-11-26", to: TODAY }, TODAY)).toBe(
      "Khoảng tối đa 366 ngày; đang chọn 367 ngày. Thu hẹp khoảng ngày để xem.",
    );
    expect(periodError({ from: "2026-01-01", to: "2027-01-03" }, TODAY)).toMatch(/đang chọn 368 ngày/);
  });
  it("ngày tương lai, ngày đầu sau ngày cuối, ngày thiếu hay sai dạng", () => {
    expect(periodError({ from: "2026-11-20", to: "2026-11-28" }, TODAY)).toBe(
      "Không chọn được ngày sau hôm nay (27/11/2026).",
    );
    expect(periodError({ from: "2026-11-20", to: "2026-11-19" }, TODAY)).toBe(
      "Ngày đầu phải trước hoặc bằng ngày cuối.",
    );
    expect(periodError({ from: "", to: TODAY }, TODAY)).toBe("Chọn đủ ngày đầu và ngày cuối.");
    expect(periodError({ from: "2026-13-40", to: TODAY }, TODAY)).toBe("Chọn đủ ngày đầu và ngày cuối.");
  });
});

describe("daysBetween", () => {
  it("tính cả hai đầu", () => {
    expect(daysBetween("2026-11-27", "2026-11-27")).toBe(1);
    expect(daysBetween("2026-02-27", "2026-03-01")).toBe(3);
  });
});

describe("parsePeriod / writePeriod (kỳ nằm trên địa chỉ để nút quay lại của trình duyệt đúng)", () => {
  it("không có tham số → Hôm nay", () => {
    expect(parsePeriod(new URLSearchParams(), TODAY)).toEqual(presetPeriod("today", TODAY));
  });
  it("kỳ mẫu ghi một tham số; tính lại theo hôm nay", () => {
    const sp = new URLSearchParams();
    writePeriod(sp, presetPeriod("7d", TODAY));
    expect(sp.toString()).toBe("ky=7d");
    expect(parsePeriod(sp, "2026-11-28")).toEqual(presetPeriod("7d", "2026-11-28"));
  });
  it("tùy chọn ghi cả hai ngày; sai dạng → về Hôm nay", () => {
    const sp = new URLSearchParams();
    writePeriod(sp, { preset: "custom", from: "2026-11-01", to: "2026-11-10" });
    expect(parsePeriod(sp, TODAY)).toEqual({ preset: "custom", from: "2026-11-01", to: "2026-11-10" });
    expect(parsePeriod(new URLSearchParams("ky=custom&tu=abc&den=2026-11-10"), TODAY)).toEqual(
      presetPeriod("today", TODAY),
    );
    expect(parsePeriod(new URLSearchParams("ky=lạ"), TODAY)).toEqual(presetPeriod("today", TODAY));
  });
});
