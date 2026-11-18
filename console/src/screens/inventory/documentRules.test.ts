// Luật của màn Phiếu kho (kế hoạch M1 Task 328): cộng dòng khi quét, bước số lượng, kiểm dòng, tổng, đổi sang API.
import { describe, expect, it } from "vitest";
import {
  addScanned,
  checkLines,
  fromApiLines,
  hasSupplier,
  sameLines,
  stepQty,
  toApiLines,
  totals,
  formatTime,
  type EditLine,
} from "./documentRules";

const A = "0195f3b2-0000-7000-8000-00000000b001";
const B = "0195f3b2-0000-7000-8000-00000000b002";
const line = (variantId: string, qty: string, cost = ""): EditLine => ({ variantId, qty, cost });

describe("addScanned", () => {
  it("mã mới thêm dòng 1 cái; quét lại cùng mã cộng 1", () => {
    let ls = addScanned([], A);
    expect(ls).toEqual([line(A, "1")]);
    ls = addScanned(ls, B);
    ls = addScanned(ls, A);
    expect(ls).toEqual([line(A, "2"), line(B, "1")]);
  });
  it("ô số lượng đang sai dạng: quét lại đặt về 1", () => {
    expect(addScanned([line(A, "abc")], A)).toEqual([line(A, "1")]);
  });
});

describe("stepQty", () => {
  it("phiếu thường không xuống dưới 1", () => {
    expect(stepQty([line(A, "1")], A, -1, "export")).toEqual([line(A, "1")]);
    expect(stepQty([line(A, "2")], A, -1, "import")).toEqual([line(A, "1")]);
  });
  it("phiếu điều chỉnh bỏ qua 0: 1 − 1 → −1, −1 + 1 → 1", () => {
    expect(stepQty([line(A, "1")], A, -1, "adjust")).toEqual([line(A, "-1")]);
    expect(stepQty([line(A, "-1")], A, 1, "adjust")).toEqual([line(A, "1")]);
  });
});

describe("checkLines", () => {
  it("số lượng phải là số nguyên dương; điều chỉnh khác 0; giá nhập sai dạng", () => {
    const errs = checkLines([line(A, "0"), line(B, "2", "18.0000")], "import");
    expect(errs[A]).toBe("Số lượng phải từ 1 trở lên");
    expect(errs[B]).toBe("Giá nhập không đúng dạng");
    expect(checkLines([line(A, "-3")], "adjust")).toEqual({});
    expect(checkLines([line(A, "0")], "adjust")[A]).toBe("Số lượng điều chỉnh phải khác 0");
    expect(checkLines([line(A, "1,5")], "export")[A]).toBe("Số lượng phải là số nguyên");
    expect(checkLines([line(A, "1000001")], "export")[A]).toBe("Số lượng tối đa 1.000.000");
  });
});

describe("totals, toApiLines, fromApiLines", () => {
  it("tổng mã, cái, giá nhập chỉ cộng dòng có giá", () => {
    const ls = [line(A, "10", "180.000"), line(B, "5")];
    expect(totals(ls)).toEqual({ skus: 2, units: 15, cost: 1_800_000 });
    expect(totals([line(A, "2")])).toEqual({ skus: 1, units: 2, cost: null });
  });
  it("giá nhập chỉ gửi ở phiếu nhập; đọc lại ra đúng chữ", () => {
    const ls = [line(A, "10", "180.000"), line(B, "5")];
    expect(toApiLines(ls, "import")).toEqual([
      { variantId: A, quantity: 10, unitCost: 180000 },
      { variantId: B, quantity: 5 },
    ]);
    expect(toApiLines(ls, "export")).toEqual([
      { variantId: A, quantity: 10 },
      { variantId: B, quantity: 5 },
    ]);
    const back = fromApiLines([
      { variantId: A, quantity: 10, unitCost: 180000 },
      { variantId: B, quantity: -1 },
    ]);
    expect(back).toEqual([line(A, "10", "180.000"), line(B, "-1")]);
    expect(sameLines(back, [line(A, "10", "180000"), line(B, "-1")])).toBe(true);
    expect(sameLines(back, [line(A, "11", "180.000"), line(B, "-1")])).toBe(false);
  });
});

describe("hasSupplier, formatTime", () => {
  it("chỉ phiếu nhập có nhà cung cấp", () => {
    expect(hasSupplier("import")).toBe(true);
    for (const k of ["export", "damage", "damage_export", "adjust"] as const) expect(hasSupplier(k)).toBe(false);
  });
  it("giờ Việt Nam kiểu 15:10 18/11", () => {
    expect(formatTime("2026-11-18T08:10:00Z")).toBe("15:10 18/11");
  });
});
