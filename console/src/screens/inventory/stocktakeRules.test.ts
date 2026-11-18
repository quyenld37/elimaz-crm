// Luật hiện của màn Kiểm kho (kế hoạch M1 Task 329; mockup docs/mockups/m1/kho-kiem-kho.html, trạng thái C, D, F).
import { describe, expect, it } from "vitest";
import type { Grant, StocktakeLine, VariantSearchHit } from "../../api";
import { canAt, confirmText, lineKind, matchHit, parseCount, preview, signedDelta } from "./stocktakeRules";

const WH = "0195f3b2-0000-7000-8000-00000000a001";
const OTHER = "0195f3b2-0000-7000-8000-00000000a002";

function line(n: number, s: Partial<StocktakeLine>): StocktakeLine {
  const l: StocktakeLine = {
    variantId: `v${String(n)}`,
    expectedOnShelf: 0,
    counted: 0,
    delta: 0,
    countedAt: "2026-11-18T09:00:00Z",
    version: 1,
    onHand: 0,
    held: 0,
    inTransit: 0,
    damaged: 0,
    available: 0,
    availableAfter: 0,
    ...s,
  };
  return l;
}

// Bốn mã của trạng thái C trong mockup cộng mã khớp thứ hai.
const KH05 = line(1, { onHand: 10, inTransit: 3, expectedOnShelf: 7, counted: 7, delta: 0, availableAfter: 7 });
const KH07 = line(2, {
  onHand: 5,
  held: 4,
  expectedOnShelf: 5,
  counted: 2,
  delta: -3,
  available: 1,
  availableAfter: -2,
});
const OVER = line(3, { onHand: 10, expectedOnShelf: 10, counted: 11, delta: 1, available: 10, availableAfter: 11 });
const MATCH1 = line(4, { onHand: 12, expectedOnShelf: 12, counted: 12, available: 12, availableAfter: 12 });
const MATCH2 = line(5, { onHand: 9, expectedOnShelf: 9, counted: 9, available: 9, availableAfter: 9 });
const UNDER = line(6, { onHand: 8, expectedOnShelf: 8, counted: 6, delta: -2, available: 8, availableAfter: 6 });

describe("lineKind", () => {
  it.each([
    ["KH05: sổ 10, đang giao 3, đếm 7 → không chỉnh", KH05, "match"],
    ["KH07: đếm thiếu, có thể bán sau âm → thiếu so với phần giữ", KH07, "short_held"],
    ["thừa", OVER, "over"],
    ["thiếu nhưng vẫn đủ phần giữ", UNDER, "under"],
    ["khớp sổ", MATCH1, "match"],
    ["khớp sổ nhưng có thể bán đã âm từ trước: không do phiên này", line(7, { availableAfter: -1 }), "match"],
  ])("%s", (_n, l, kind) => {
    expect(lineKind(l)).toBe(kind);
  });
});

describe("preview", () => {
  it("mã lệch và mã khớp có hàng đang giao hiện riêng; mã khớp còn lại gộp; danh sách thiếu so với phần giữ", () => {
    const p = preview([KH05, KH07, OVER, MATCH1, MATCH2]);
    expect(p.rows.map((l) => l.variantId)).toEqual(["v1", "v2", "v3"]);
    expect(p.matched.map((l) => l.variantId)).toEqual(["v4", "v5"]);
    expect(p.changed.map((l) => l.variantId)).toEqual(["v2", "v3"]);
    expect(p.shortHeld.map((l) => l.variantId)).toEqual(["v2"]);
  });

  it("phiên rỗng", () => {
    expect(preview([])).toEqual({ rows: [], matched: [], changed: [], shortHeld: [] });
  });
});

describe("confirmText (trạng thái D)", () => {
  it("câu của mockup: hai mã chỉnh, ba mã khớp, một mã thiếu so với phần giữ", () => {
    expect(confirmText([KH05, KH07, OVER, MATCH1, MATCH2])).toBe(
      "Sửa Tồn của 2 mã: −3 và +1. 3 mã khớp không đổi. 1 mã sẽ thiếu so với phần giữ.",
    );
  });

  it("một mã chỉnh, không mã khớp, không thiếu", () => {
    expect(confirmText([OVER])).toBe("Sửa Tồn của 1 mã: +1.");
  });

  it("không mã nào lệch", () => {
    expect(confirmText([MATCH1])).toBe("Không mã nào lệch sổ, Tồn không đổi. 1 mã khớp không đổi.");
  });

  it("nhiều hơn 5 mã chỉnh: kể 5 số đầu rồi …", () => {
    const many = [1, 2, 3, 4, 5, 6, 7].map((i) => line(i, { delta: i }));
    expect(confirmText(many)).toBe("Sửa Tồn của 7 mã: +1, +2, +3, +4, +5, …");
  });
});

describe("matchHit", () => {
  const hit = (sku: string, barcode: string): VariantSearchHit => ({
    variantId: sku,
    productId: "p",
    sku,
    barcode,
    productCode: sku.split("-")[0] ?? sku,
    productName: "Tên",
    color: "Đen",
    size: "S",
    price: 1,
    sellable: true,
  });

  it("barcode đúng, hoặc SKU không phân biệt hoa thường; tiền tố hay tên không tính là quét được", () => {
    const hits = [hit("DAM0123-DEN-M", "2000000000022"), hit("DAM0123-DEN-S", "2000000000015")];
    expect(matchHit("2000000000015", hits)?.sku).toBe("DAM0123-DEN-S");
    expect(matchHit(" dam0123-den-m ", hits)?.sku).toBe("DAM0123-DEN-M");
    expect(matchHit("DAM0123", hits)).toBeNull();
    expect(matchHit("2000000009999", [])).toBeNull();
  });
});

describe("parseCount", () => {
  it.each([
    ["7", 7],
    [" 12 ", 12],
    ["0", 0],
    ["1000000", 1_000_000],
    ["1000001", null],
    ["-1", null],
    ["2.5", null],
    ["", null],
    ["abc", null],
  ])("%s → %s", (text, n) => {
    expect(parseCount(text)).toBe(n);
  });
});

describe("canAt", () => {
  const g = (permission: string, type: Grant["scope"]["type"], id = ""): Grant => ({ permission, scope: { type, id } });

  it("toàn shop hoặc đúng kho; kho khác, page, quyền khác thì không", () => {
    expect(canAt([g("inventory.stocktake.apply", "all")], "inventory.stocktake.apply", WH)).toBe(true);
    expect(canAt([g("inventory.stocktake.apply", "warehouse", WH)], "inventory.stocktake.apply", WH)).toBe(true);
    expect(canAt([g("inventory.stocktake.apply", "warehouse", OTHER)], "inventory.stocktake.apply", WH)).toBe(false);
    expect(canAt([g("inventory.stocktake.apply", "page", WH)], "inventory.stocktake.apply", WH)).toBe(false);
    expect(canAt([g("inventory.stocktake", "all")], "inventory.stocktake.apply", WH)).toBe(false);
  });
});

describe("định dạng", () => {
  it("signedDelta dùng dấu trừ thật và dấu cộng", () => {
    expect([signedDelta(-3), signedDelta(1), signedDelta(0)]).toEqual(["−3", "+1", "0"]);
  });
});
