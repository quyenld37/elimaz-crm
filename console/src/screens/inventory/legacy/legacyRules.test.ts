// Luật thuần của màn Hàng hoàn đơn cũ (Task 273; mockup docs/mockups/m7/hang-hoan-don-cu.html).
import { describe, expect, it } from "vitest";
import type { LegacyReturn } from "../../../api";
import {
  addScan,
  blockedReason,
  dayKey,
  dayTitle,
  formatVnDate,
  groupByDay,
  normalizeReference,
  parseVnDate,
  qtyError,
  referenceError,
  returnCounts,
  summary,
  type FormLine,
} from "./legacyRules";

const line = (over: Partial<FormLine> = {}): FormLine => ({
  id: "l1",
  variantId: "v1",
  sku: "A-1",
  name: "Áo",
  qty: 1,
  condition: "sellable",
  ...over,
});

describe("normalizeReference, referenceError", () => {
  it("bỏ khoảng trắng và viết hoa: 'vtp 1234 5678' → VTP12345678", () => {
    expect(normalizeReference(" vtp 1234\t5678 ")).toBe("VTP12345678");
  });
  it("3–64 ký tự sau chuẩn hóa; rỗng chưa báo lỗi (chưa gõ)", () => {
    expect(referenceError("")).toBeNull();
    expect(referenceError("V1")).toBe("Mã tham chiếu từ 3 đến 64 ký tự.");
    expect(referenceError("ABC")).toBeNull();
    expect(referenceError("A".repeat(64))).toBeNull();
    expect(referenceError("A".repeat(65))).toBe("Mã tham chiếu từ 3 đến 64 ký tự.");
  });
});

describe("qtyError", () => {
  it.each([
    [0, true],
    [1, false],
    [1000, false],
    [1001, true],
    [1.5, true],
    [-1, true],
  ])("%s", (n, bad) => {
    expect(qtyError(n) !== null).toBe(bad);
  });
});

describe("addScan: quét cộng vào dòng 'Bán lại được' của biến thể, chưa có thì tạo", () => {
  const hit = { variantId: "v1", sku: "A-1", productName: "Áo", color: "Trắng", size: "S" };
  it("lần đầu tạo dòng 1 cái bán lại được", () => {
    const r = addScan([], hit, () => "n1");
    expect(r).toEqual([
      { id: "n1", variantId: "v1", sku: "A-1", name: "Áo · Trắng · S", qty: 1, condition: "sellable" },
    ]);
  });
  it("quét lại cộng 1 vào dòng bán lại được; dòng Hỏng của cùng mã giữ nguyên", () => {
    const start = [line({ id: "a", qty: 2 }), line({ id: "b", condition: "damaged", qty: 1 })];
    const r = addScan(start, hit, () => "n2");
    expect(r.map((l) => [l.id, l.qty, l.condition])).toEqual([
      ["a", 3, "sellable"],
      ["b", 1, "damaged"],
    ]);
  });
  it("chỉ còn dòng Hỏng thì tạo dòng Bán lại được mới", () => {
    const r = addScan([line({ id: "b", condition: "damaged" })], hit, () => "n3");
    expect(r.map((l) => [l.id, l.condition])).toEqual([
      ["b", "damaged"],
      ["n3", "sellable"],
    ]);
  });
  it("không vượt 1000 mỗi dòng", () => {
    const r = addScan([line({ id: "a", qty: 1000 })], hit, () => "n4");
    expect(r[0]?.qty).toBe(1000);
  });
});

describe("summary, blockedReason", () => {
  it("3 cái: 2 bán lại được, 1 hỏng", () => {
    expect(summary([line({ qty: 2 }), line({ id: "b", condition: "damaged" })])).toBe("3 cái: 2 bán lại được, 1 hỏng");
    expect(summary([])).toBe("Chưa có món nào");
  });
  it("khóa khi thiếu mã, thiếu món, số lượng sai; mở khi đủ", () => {
    const msg = "Thêm ít nhất một món và mã tham chiếu thì nút mở.";
    expect(blockedReason("", [line()])).toBe(msg);
    expect(blockedReason("ABC", [])).toBe(msg);
    expect(blockedReason("AB", [line()])).toBe("Mã tham chiếu từ 3 đến 64 ký tự.");
    expect(blockedReason("ABC", [line({ qty: 0 })])).toBe("Số lượng từ 1 đến 1.000. Không còn cái nào thì bỏ dòng.");
    expect(blockedReason("ABC", [line()])).toBeNull();
  });
});

describe("ngày Việt Nam", () => {
  it("23:30 UTC ngày 2/12 đã là 3/12 ở Việt Nam", () => {
    expect(dayKey("2026-12-02T23:30:00Z")).toBe("2026-12-03");
    expect(dayKey("2026-12-02T16:59:00Z")).toBe("2026-12-02");
  });
  it("tiêu đề ngày kèm thứ", () => {
    expect(dayTitle("2026-12-03", 2)).toBe("Thứ Năm 03/12 · 2 kiện");
    expect(dayTitle("2026-12-02", 4)).toBe("Thứ Tư 02/12 · 4 kiện");
    expect(dayTitle("2026-12-06", 1)).toBe("Chủ nhật 06/12 · 1 kiện");
  });
  it("gõ dd/mm/yyyy ⇄ yyyy-mm-dd; sai thì null", () => {
    expect(parseVnDate("01/12/2026")).toBe("2026-12-01");
    expect(parseVnDate("1/2/2026")).toBe("2026-02-01");
    expect(parseVnDate("31/02/2026")).toBeNull();
    expect(parseVnDate("abc")).toBeNull();
    expect(formatVnDate("2026-12-01")).toBe("01/12/2026");
  });
});

const ret = (id: string, at: string, lines: LegacyReturn["lines"]): LegacyReturn => ({
  id,
  warehouseId: "w",
  reference: id.toUpperCase(),
  stockDocumentId: "d",
  lines,
  createdBy: { id: "u", name: "Hùng" },
  createdAt: at,
});

describe("groupByDay, returnCounts", () => {
  const items = [
    ret("a", "2026-12-03T02:00:00Z", [{ variantId: "v", qty: 3, condition: "sellable" }]),
    ret("b", "2026-12-02T03:00:00Z", [
      { variantId: "v", qty: 2, condition: "sellable" },
      { variantId: "w", qty: 1, condition: "damaged" },
    ]),
    ret("c", "2026-12-02T01:00:00Z", [{ variantId: "v", qty: 4, condition: "sellable" }]),
  ];
  it("nhóm theo ngày, ngày mới trước, giữ thứ tự trong ngày", () => {
    const g = groupByDay(items);
    expect(g.map((d) => [d.day, d.items.map((i) => i.id)])).toEqual([
      ["2026-12-03", ["a"]],
      ["2026-12-02", ["b", "c"]],
    ]);
  });
  it("đếm cái bán lại được và hỏng của một kiện, và của cả danh sách", () => {
    const [, second] = items;
    if (second === undefined) throw new Error("thiếu kiện thứ hai");
    expect(returnCounts(second)).toEqual({ sellable: 2, damaged: 1, total: 3 });
    expect(returnCounts(items)).toEqual({ sellable: 9, damaged: 1, total: 10 });
  });
});
