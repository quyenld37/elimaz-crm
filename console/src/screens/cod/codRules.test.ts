import { describe, expect, it } from "vitest";
import { TOTALS_C } from "./codTestkit";
import {
  checkLocalFile,
  columnText,
  confirmSummary,
  contentTypeOf,
  defaultGroup,
  feeMoney,
  parseErrorText,
  signedMoney,
  sizeText,
  timeDay,
  visibleGroups,
} from "./codRules";

describe("luật hiển thị đối soát COD", () => {
  it("tiền có dấu (dấu trừ U+2212), phí là số trừ, 0", () => {
    expect(signedMoney(100_000)).toBe("+100.000 ₫");
    expect(signedMoney(-90_000)).toBe("−90.000 ₫");
    expect(signedMoney(0)).toBe("0 ₫");
    expect(feeMoney(32_000)).toBe("−32.000 ₫");
    expect(signedMoney(-123_456_789)).toBe("−123.456.789 ₫");
  });

  it("lỗi đọc tệp: tên cột của mẫu ELIMAZ bằng tiếng Việt, cột của hãng giữ nguyên, có số dòng", () => {
    expect(parseErrorText({ code: "missing_column", column: "tien_cod" })).toBe('Thiếu cột "Tiền thu hộ"');
    expect(parseErrorText({ code: "missing_column", column: "Phí dịch vụ" })).toBe('Thiếu cột "Phí dịch vụ"');
    expect(parseErrorText({ code: "bad_amount", column: "phi_van_chuyen", line: 57 })).toBe(
      'Số tiền không đọc được ở dòng 57, cột "Phí vận chuyển"',
    );
    expect(parseErrorText({ code: "formula_not_allowed", column: "thuc_nhan", line: 3 })).toBe(
      "Tệp có công thức ở dòng 3",
    );
    expect(parseErrorText({ code: "la" })).toBe("Không đọc được tệp");
    expect(columnText("ma_van_don")).toBe("Mã vận đơn");
  });

  it("tệp: đuôi, cỡ, kiểu nội dung, chữ cỡ tệp", () => {
    expect(checkLocalFile({ name: "a.XLSX", size: 10 })).toBe(true);
    expect(checkLocalFile({ name: "a.csv", size: 10 * 1024 * 1024 })).toBe(true);
    expect(checkLocalFile({ name: "a.csv", size: 10 * 1024 * 1024 + 1 })).toBe(false);
    expect(checkLocalFile({ name: "a.xls", size: 10 })).toBe(false);
    expect(checkLocalFile({ name: "a.csv", size: 0 })).toBe(false);
    expect(contentTypeOf("a.csv")).toBe("text/csv");
    expect(contentTypeOf("a.xlsx")).toBe("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet");
    expect(sizeText(86_016)).toBe("84 KB");
    expect(sizeText(3 * 1024 * 1024 + 200_000)).toBe("3,2 MB");
  });

  it("giờ Việt Nam", () => {
    expect(timeDay("2026-11-21T09:05:00Z")).toBe("16:05 · 21/11");
    expect(timeDay("2026-11-21T17:30:00Z")).toBe("00:30 · 22/11");
  });

  it("nhóm: năm nhóm luôn hiện, Chưa khớp chỉ khi có; mở sẵn Lệch tiền nếu có", () => {
    expect(visibleGroups(TOTALS_C).map((g) => g.label)).toEqual([
      "Khớp",
      "Lệch tiền",
      "Ngoài hệ thống",
      "Trùng",
      "Chỉ phí",
    ]);
    expect(visibleGroups([{ matchStatus: "pending", count: 2, codAmount: 1 }]).at(-1)?.label).toBe("Chưa khớp");
    expect(defaultGroup(TOTALS_C)).toBe("amount_mismatch");
    expect(defaultGroup([{ matchStatus: "external", count: 1, codAmount: 1 }])).toBe("external");
    expect(defaultGroup([])).toBe("matched");
  });

  it("câu Chốt: phí của dòng khớp, lệch, chỉ phí; thiếu của dòng lệch; thiếu trường (máy chủ cũ) → 0", () => {
    expect(confirmSummary(TOTALS_C)).toEqual({ matched: 196, mismatched: 3, short: 100_000, fee: 6_240_000 });
    expect(confirmSummary([{ matchStatus: "matched", count: 1, codAmount: 5 }])).toEqual({
      matched: 1,
      mismatched: 0,
      short: 0,
      fee: 0,
    });
  });
});
