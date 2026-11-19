// Luật thuần của màn Mã giảm giá: đổi đơn vị ô nhập, giờ Việt Nam, trạng thái hiện, kiểm điều khoản, tệp CSV.
import { describe, expect, it } from "vitest";
import type { Coupon } from "../../api";
import {
  EMPTY_TERMS,
  couponState,
  codesCsv,
  conditionText,
  discountText,
  isoToVnLocal,
  parsePercent,
  percentInput,
  periodText,
  readTerms,
  termsOf,
  usesText,
  vnLocalToIso,
} from "./couponRules";

const base: Coupon = {
  id: "c1",
  code: "THU10",
  name: "Giảm 10% mùa thu",
  discountKind: "percent",
  value: 1000,
  maxDiscount: 150_000,
  minSubtotal: 500_000,
  channels: [],
  maxUses: 200,
  usedCount: 87,
  remainingUses: 113,
  releaseOnCancel: true,
  validFrom: "2026-10-31T17:00:00Z",
  validTo: "2026-11-30T16:59:00Z",
  status: "active",
  bound: false,
  createdAt: "2026-10-30T00:00:00Z",
  version: 3,
};

describe("parsePercent: ô % → phần vạn", () => {
  it.each([
    ["10", 1000],
    ["12,5", 1250],
    ["12.5", 1250],
    ["0,01", 1],
    ["100", 10000],
    [" 7 ", 700],
  ])("%s → %d", (s, want) => {
    expect(parsePercent(s)).toBe(want);
  });
  it.each(["", "0", "100,01", "101", "-5", "1,234", "abc", "10%x"])("%s → null", (s) => {
    expect(parsePercent(s)).toBeNull();
  });
  it("percentInput là chiều ngược", () => {
    expect(percentInput(1000)).toBe("10");
    expect(percentInput(1250)).toBe("12,5");
    expect(percentInput(1)).toBe("0,01");
  });
});

describe("giờ Việt Nam cho ô datetime-local (không theo múi của máy)", () => {
  it("ô → ISO UTC", () => {
    expect(vnLocalToIso("2027-01-20T00:00")).toBe("2027-01-19T17:00:00.000Z");
    expect(vnLocalToIso("2027-02-10T23:59")).toBe("2027-02-10T16:59:00.000Z");
  });
  it("ISO → ô", () => {
    expect(isoToVnLocal("2027-01-19T17:00:00Z")).toBe("2027-01-20T00:00");
  });
  it("ô sai dạng hay trống → null", () => {
    expect(vnLocalToIso("")).toBeNull();
    expect(vnLocalToIso("20/01/2027")).toBeNull();
  });
});

describe("couponState: trạng thái hiện trên danh sách", () => {
  const now = Date.parse("2026-11-19T03:00:00Z");
  it.each<[string, Partial<Coupon>, string]>([
    ["đang chạy", {}, "active"],
    ["đã tắt thắng mọi thứ", { status: "disabled", usedCount: 200 }, "disabled"],
    ["hết hạn", { validTo: "2026-11-18T00:00:00Z" }, "expired"],
    ["chưa tới ngày", { validFrom: "2026-11-20T00:00:00Z" }, "upcoming"],
    ["hết lượt", { usedCount: 200, remainingUses: 0 }, "exhausted"],
    ["không giới hạn lượt", { maxUses: undefined, remainingUses: undefined, usedCount: 999 }, "active"],
  ])("%s", (_n, p, want) => {
    expect(couponState({ ...base, ...p }, now)).toBe(want);
  });
});

describe("chữ trên dòng mã", () => {
  it("giảm", () => {
    expect(discountText(base)).toBe("10%, tối đa 150.000 ₫");
    expect(discountText({ ...base, maxDiscount: undefined, value: 2000 })).toBe("20%");
    expect(discountText({ ...base, discountKind: "fixed", value: 50_000 })).toBe("50.000 ₫");
  });
  it("điều kiện", () => {
    expect(conditionText(base)).toBe("Đơn từ 500.000 ₫");
    expect(conditionText({ ...base, minSubtotal: 0, perCustomerLimit: 1 })).toBe("Mọi đơn · 1 lượt mỗi khách");
    expect(conditionText({ ...base, minSubtotal: 0, channels: ["web"] })).toBe("Chỉ Website");
    expect(conditionText({ ...base, minSubtotal: 0, bound: true })).toBe("Dành riêng một khách");
  });
  it("hạn và lượt", () => {
    expect(periodText(base)).toBe("01/11–30/11");
    expect(usesText(base)).toBe("87 / 200");
    expect(usesText({ ...base, maxUses: undefined })).toBe("87");
  });
});

describe("readTerms: kiểm ô điều khoản, dựng thân gửi", () => {
  const good = {
    ...EMPTY_TERMS,
    kind: "percent" as const,
    value: "10",
    maxDiscount: "200.000",
    minSubtotal: "300.000",
    from: "2027-01-20T00:00",
    to: "2027-02-10T23:59",
    maxUses: "300",
    perCustomer: "1",
    channels: ["manual", "facebook"],
  };
  it("đủ ô: tiền theo đồng, % theo phần vạn", () => {
    const r = readTerms(good);
    expect(r.errors).toEqual({});
    expect(r.terms).toEqual({
      discountKind: "percent",
      value: 1000,
      maxDiscount: 200_000,
      minSubtotal: 300_000,
      validFrom: "2027-01-19T17:00:00.000Z",
      validTo: "2027-02-10T16:59:00.000Z",
      maxUses: 300,
      perCustomerLimit: 1,
      channels: ["manual", "facebook"],
      releaseOnCancel: true,
    });
  });
  it("số tiền: value theo đồng, bỏ trần giảm", () => {
    const r = readTerms({ ...good, kind: "fixed", value: "50.000", maxDiscount: "999" });
    expect(r.terms?.value).toBe(50_000);
    expect(r.terms).not.toHaveProperty("maxDiscount");
  });
  it("ô trống tùy chọn không gửi; ô sai báo theo tên ô", () => {
    const r = readTerms({ ...good, maxDiscount: "", maxUses: "", perCustomer: "", minSubtotal: "" });
    expect(r.terms).toMatchObject({ minSubtotal: 0 });
    expect(r.terms).not.toHaveProperty("maxUses");
    const bad = readTerms({ ...good, value: "150", to: "2027-01-01T00:00", maxUses: "0" });
    expect(bad.terms).toBeNull();
    expect(Object.keys(bad.errors).sort()).toEqual(["maxUses", "to", "value"]);
  });
  it("termsOf dựng lại ô từ mã có sẵn", () => {
    expect(readTerms(termsOf(base)).terms).toMatchObject({ value: 1000, maxDiscount: 150_000, maxUses: 200 });
  });
});

it("codesCsv: có BOM cho Excel, một mã một dòng", () => {
  expect(codesCsv(["SN-A", "SN-B"])).toBe("﻿Mã\r\nSN-A\r\nSN-B\r\n");
});
