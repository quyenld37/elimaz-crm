// Luật thuần của màn Kiểm kho mở sổ (Task 273; mockup docs/mockups/m7/kiem-kho-mo-so.html).
import { describe, expect, it } from "vitest";
import type { OpeningStocktake } from "../../../api";
import { canPost, importRowText, postBlockedReason, remaining, scanLabel } from "./openingRules";

const base: OpeningStocktake = {
  stocktakeId: "s",
  warehouseId: "w",
  status: "open",
  linesExpected: 412,
  linesCounted: 405,
  unitsCounted: 8214,
};

describe("remaining, canPost, postBlockedReason", () => {
  it("còn 7 mã → còn 7, khóa nút Đăng kèm lý do nêu số 7", () => {
    expect(remaining(base)).toBe(7);
    expect(canPost(base)).toBe(false);
    expect(postBlockedReason(base)).toBe("Còn 7 mã chưa đếm. Đếm hoặc nhập 0 cho cả 7 mã thì nút mở.");
  });
  it("đếm đủ → mở nút, không có lý do", () => {
    const done = { ...base, linesCounted: 412 };
    expect(remaining(done)).toBe(0);
    expect(canPost(done)).toBe(true);
    expect(postBlockedReason(done)).toBeNull();
  });
  it("số đếm vượt số phải đếm (mã mới bật bán rồi tắt) không âm", () => {
    expect(remaining({ ...base, linesCounted: 420 })).toBe(0);
  });
  it("kho không có mã nào phải đếm → không đăng được (không có gì để mở sổ)", () => {
    const none = { ...base, linesExpected: 0, linesCounted: 0 };
    expect(canPost(none)).toBe(false);
    expect(postBlockedReason(none)).toBe("Kho này chưa có mã hàng nào đang bán để đếm.");
  });
  it("phiên đã đăng hoặc đã hủy → không đăng, lý do nêu trạng thái", () => {
    expect(canPost({ ...base, linesCounted: 412, status: "applied" })).toBe(false);
    expect(postBlockedReason({ ...base, linesCounted: 412, status: "applied" })).toBe("Phiên này đã đăng.");
    expect(postBlockedReason({ ...base, status: "cancelled" })).toBe("Phiên này đã hủy.");
  });
});

describe("importRowText: lỗi từng dòng của tệp Excel theo mã", () => {
  it.each([
    ["unknown_code", "Dòng 12: Không thấy mã trong danh mục"],
    ["ambiguous_code", "Dòng 12: Mã có ở 2 người bán: ghi SKU thay barcode"],
    ["not_integer", "Dòng 12: Số đếm không phải số nguyên"],
    ["negative", "Dòng 12: Số đếm âm"],
    ["formula", "Dòng 12: Ô có công thức: dán giá trị"],
  ] as const)("%s", (code, text) => {
    expect(importRowText({ row: 12, code })).toBe(text);
  });
});

describe("scanLabel", () => {
  it("+1, −1 (dấu trừ thật), số khác", () => {
    expect(scanLabel(1)).toBe("+1");
    expect(scanLabel(-1)).toBe("−1");
    expect(scanLabel(3)).toBe("+3");
  });
});
