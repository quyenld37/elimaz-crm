// Định dạng số của báo cáo (Task 239, M6-16): tiền, tỷ lệ phần vạn, số đếm, ngày. Một nguồn cho mọi màn báo cáo.
import { describe, expect, it } from "vitest";
import { count, dayLabel, money, rate, vnd } from "./format";

describe("format", () => {
  it("money: dấu chấm hàng nghìn, số nguyên đồng", () => {
    expect(money(1910000)).toBe("1.910.000");
    expect(money(0)).toBe("0");
    expect(money(999)).toBe("999");
    expect(money(123456789)).toBe("123.456.789");
    expect(money(9007199254740991)).toBe("9.007.199.254.740.991");
  });

  it("vnd: thêm đơn vị ₫", () => {
    expect(vnd(272857)).toBe("272.857 ₫");
  });

  it("rate: phần vạn → phần trăm hai số lẻ, dấu phẩy; null → —", () => {
    expect(rate(6667)).toBe("66,67%");
    expect(rate(null)).toBe("—");
    expect(rate(0)).toBe("0,00%");
    expect(rate(10000)).toBe("100,00%");
    expect(rate(5)).toBe("0,05%");
  });

  it("count: số đếm có dấu chấm hàng nghìn", () => {
    expect(count(8)).toBe("8");
    expect(count(1200)).toBe("1.200");
  });

  it("dayLabel: YYYY-MM-DD → DD/MM/YYYY; chuỗi lạ giữ nguyên", () => {
    expect(dayLabel("2026-11-18")).toBe("18/11/2026");
    expect(dayLabel("không phải ngày")).toBe("không phải ngày");
  });
});
