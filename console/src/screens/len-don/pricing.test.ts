// Tính tiền trước trên màn Lên đơn (kế hoạch M2 Task M2-27): cùng thuật toán với internal/modules/order/internal/pricing
// (Task M2-07), kiểm bằng đúng tệp vector Go dùng (testdata/vectors.json, 200 bộ).
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { MAX_MONEY, PricingError, allocate, compute, manualDiscountBP, type Input, type Totals } from "./pricing";

interface Vector {
  input: Input;
  totals: Totals;
}

function vectors(): Vector[] {
  const p = join(import.meta.dirname, "../../../../internal/modules/order/internal/pricing/testdata/vectors.json");
  const v: unknown = JSON.parse(readFileSync(p, "utf8"));
  if (!Array.isArray(v)) throw new Error("vectors.json không phải mảng");
  return v as Vector[]; // tệp test do Go sinh, không phải dữ liệu API
}

describe("vector chung với Go", () => {
  const vs = vectors();

  it("có đúng 200 bộ", () => {
    expect(vs).toHaveLength(200);
  });

  it.each(vs.map((v, i) => [i, v] as const))("bộ %i cho đúng từng con số", (_i, v) => {
    expect(compute(v.input)).toEqual(v.totals);
  });
});

describe("compute", () => {
  it("DH01: 2 × 250.000 giảm dòng 20.000, 1 × 180.000, ship 30.000 → 690.000", () => {
    const t = compute({
      lines: [
        { key: 1, qty: 2, unitPrice: 250000, lineDiscount: 20000 },
        { key: 2, qty: 1, unitPrice: 180000, lineDiscount: 0 },
      ],
      orderDiscount: 0,
      couponDiscount: 0,
      shippingFee: 30000,
    });
    expect(t.subtotal).toBe(680000);
    expect(t.grandTotal).toBe(690000);
    expect(t.lines.map((l) => l.base)).toEqual([480000, 180000]);
  });

  it("DH05: giảm đơn 100.001 chia ba dòng, phần lẻ vào dòng lớn nhất", () => {
    const t = compute({
      lines: [
        { key: 1, qty: 1, unitPrice: 500000, lineDiscount: 0 },
        { key: 2, qty: 1, unitPrice: 180000, lineDiscount: 0 },
        { key: 3, qty: 1, unitPrice: 320000, lineDiscount: 0 },
      ],
      orderDiscount: 100001,
      couponDiscount: 0,
      shippingFee: 25000,
    });
    expect(t.lines.map((l) => l.allocated)).toEqual([50001, 18000, 32000]);
    expect(t.grandTotal).toBe(1000000 - 100001 + 25000);
  });

  it.each([
    [
      "giảm dòng > tiền dòng",
      { lines: [{ key: 1, qty: 2, unitPrice: 1000, lineDiscount: 2001 }] },
      "line_discount_exceeds",
    ],
    [
      "giảm đơn + mã > tiền sau giảm dòng",
      { lines: [{ key: 1, qty: 1, unitPrice: 1000, lineDiscount: 100 }], orderDiscount: 800, couponDiscount: 101 },
      "order_discount_exceeds",
    ],
    ["số lượng âm", { lines: [{ key: 1, qty: -1, unitPrice: 1, lineDiscount: 0 }] }, "negative"],
    ["ship âm", { lines: [], shippingFee: -1 }, "negative"],
    ["tích tràn", { lines: [{ key: 1, qty: 3, unitPrice: 2 ** 52, lineDiscount: 0 }] }, "overflow"],
    [
      "tạm tính tràn",
      {
        lines: [
          { key: 1, qty: 1, unitPrice: MAX_MONEY, lineDiscount: 0 },
          { key: 2, qty: 1, unitPrice: 1, lineDiscount: 0 },
        ],
      },
      "overflow",
    ],
    [
      "tổng tràn vì ship",
      { lines: [{ key: 1, qty: 1, unitPrice: MAX_MONEY, lineDiscount: 0 }], shippingFee: 1 },
      "overflow",
    ],
    ["số lẻ", { lines: [{ key: 1, qty: 1.5, unitPrice: 1, lineDiscount: 0 }] }, "overflow"],
  ])("%s → lỗi %s", (_n, partial, code) => {
    const input: Input = { orderDiscount: 0, couponDiscount: 0, shippingFee: 0, ...partial };
    expect(() => compute(input)).toThrow(PricingError);
    try {
      compute(input);
    } catch (e) {
      expect(e instanceof PricingError && e.code).toBe(code);
    }
  });
});

describe("allocate", () => {
  it("bằng nhau thì dòng đầu nhận phần lẻ", () => {
    expect(allocate([300000, 300000], 1)).toEqual([1, 0]);
  });
  it("dòng lớn nhất không phải dòng đầu", () => {
    expect(allocate([100, 300, 300], 1)).toEqual([0, 1, 0]);
  });
  it("phần lẻ vượt chỗ còn của dòng lớn nhất sang dòng kế (Q-T64-1)", () => {
    expect(allocate([1, 1, 1], 2)).toEqual([1, 1, 0]);
    expect(allocate([1, 2, 1, 2], 5)).toEqual([1, 2, 0, 2]);
  });
  it("tích vượt 2^53 vẫn chia đúng", () => {
    const half = Math.floor(MAX_MONEY / 2);
    const got = allocate([half, half + 1], MAX_MONEY - 1);
    expect((got[0] ?? 0) + (got[1] ?? 0)).toBe(MAX_MONEY - 1);
  });
  it("Σ base = 0 → toàn 0", () => {
    expect(allocate([0, 0], 0)).toEqual([0, 0]);
  });
});

describe("manualDiscountBP (giống pricing.ManualDiscountBP)", () => {
  const t = (subtotal: number, lineDiscountTotal: number, orderDiscount: number) =>
    manualDiscountBP({ subtotal, lineDiscountTotal, orderDiscount });
  it.each([
    [1000000, 100000, 0, 1000],
    [1000000, 50000, 50001, 1001], // làm tròn lên
    [3, 1, 0, 3334],
    [0, 0, 0, 0],
    [1000, 0, 0, 0],
  ])("tạm tính %i, giảm dòng %i, giảm đơn %i → %i phần vạn", (s, l, o, want) => {
    expect(t(s, l, o)).toBe(want);
  });
  it("có giảm mà tạm tính 0 → vô cực (luôn vượt ngưỡng)", () => {
    expect(t(0, 1, 0)).toBe(Number.POSITIVE_INFINITY);
  });
});
