// Luật thuần của màn Nhập Excel (kế hoạch M1 Task 52; mockup docs/mockups/m1/nhap-excel.html).
import { describe, expect, it } from "vitest";
import type { CatalogImport, CatalogImportError } from "../../api";
import {
  MAX_BYTES,
  TEMPLATE_NAME,
  checkLocalFile,
  columnText,
  fileErrors,
  fileNameFrom,
  fmtInt,
  fmtSize,
  fmtTime,
  percent,
  rowText,
  stepOf,
  variantTotal,
} from "./importRules";

const imp = (s: Partial<CatalogImport>): CatalogImport => ({
  id: "i1",
  fileId: "f1",
  status: "ready",
  summary: { models: 0, create: 0, update: 0, unchanged: 0 },
  errors: [],
  version: 1,
  createdAt: "2026-11-18T03:02:00Z",
  ...s,
});

const err = (row: number, column: string): CatalogImportError => ({ row, column, code: "x", message: "m" });

describe("checkLocalFile", () => {
  it("nhận .xlsx (mọi cỡ chữ) tới đúng 5 MB", () => {
    expect(checkLocalFile({ name: "a.xlsx", size: 1 })).toBeNull();
    expect(checkLocalFile({ name: "A.XLSX", size: MAX_BYTES })).toBeNull();
  });
  it("từ chối tệp không phải .xlsx, tệp rỗng, tệp quá 5 MB", () => {
    expect(checkLocalFile({ name: "a.xls", size: 10 })).toBe("not_xlsx");
    expect(checkLocalFile({ name: "a.csv", size: 10 })).toBe("not_xlsx");
    expect(checkLocalFile({ name: "xlsx", size: 10 })).toBe("not_xlsx");
    expect(checkLocalFile({ name: "a.xlsx", size: 0 })).toBe("not_xlsx");
    expect(checkLocalFile({ name: "a.xlsx", size: MAX_BYTES + 1 })).toBe("too_large");
  });
});

describe("fmtInt, fmtTime, percent", () => {
  it("số có dấu chấm ngăn nghìn", () => {
    expect(fmtInt(0)).toBe("0");
    expect(fmtInt(997)).toBe("997");
    expect(fmtInt(1000)).toBe("1.000");
    expect(fmtInt(20000)).toBe("20.000");
  });
  it("giờ:phút ngày/tháng theo giờ máy", () => {
    expect(fmtTime(new Date(2026, 10, 18, 10, 2))).toBe("10:02 18/11");
    expect(fmtTime(new Date(2026, 0, 5, 7, 9))).toBe("07:09 05/01");
  });
  it("phần trăm nguyên, chặn 0–100, tổng 0 là 0", () => {
    expect(percent(0, 0)).toBe(0);
    expect(percent(68, 100)).toBe(68);
    expect(percent(1, 3)).toBe(33);
    expect(percent(5, 4)).toBe(100);
  });
});

describe("fileNameFrom", () => {
  it("lấy tên trong Content-Disposition, thiếu thì tên mặc định", () => {
    expect(fileNameFrom('attachment; filename="mau-nhap-san-pham.xlsx"')).toBe("mau-nhap-san-pham.xlsx");
    expect(fileNameFrom("attachment; filename=mau.xlsx")).toBe("mau.xlsx");
    expect(fileNameFrom(null)).toBe(TEMPLATE_NAME);
    expect(fileNameFrom("attachment")).toBe(TEMPLATE_NAME);
  });
  it("bỏ phần đường dẫn trong tên (không ghi ra ngoài thư mục tải về)", () => {
    expect(fileNameFrom('attachment; filename="../x/mau.xlsx"')).toBe("mau.xlsx");
    expect(fileNameFrom('attachment; filename="..\\mau.xlsx"')).toBe("mau.xlsx");
  });
});

describe("fileErrors, rowText, columnText", () => {
  it("chỉ lỗi dòng 0 là lỗi của cả tệp", () => {
    expect(fileErrors(imp({ status: "invalid", errors: [err(0, "")] }))).toBe(true);
    expect(fileErrors(imp({ status: "invalid", errors: [err(0, ""), err(5, "SKU")] }))).toBe(false);
    expect(fileErrors(imp({ status: "invalid", errors: [] }))).toBe(false);
  });
  it("dòng 0 là cả tệp, cột rỗng là cả dòng", () => {
    expect(rowText(0)).toBe("Cả tệp");
    expect(rowText(1200)).toBe("1.200");
    expect(columnText("")).toBe("Cả dòng");
    expect(columnText("Giá bán")).toBe("Giá bán");
  });
});

describe("variantTotal", () => {
  it("cộng tạo, cập nhật, giữ nguyên", () => {
    expect(variantTotal({ models: 200, create: 997, update: 3, unchanged: 0 })).toBe(1000);
  });
});

describe("stepOf", () => {
  it("bước đang làm theo trạng thái màn", () => {
    expect(stepOf({ kind: "idle", downloaded: false })).toBe(1);
    expect(stepOf({ kind: "idle", downloaded: true })).toBe(2);
    expect(stepOf({ kind: "uploading" })).toBe(3);
    for (const status of ["validating", "invalid", "ready", "duplicate"] as const)
      expect(stepOf({ kind: "import", status })).toBe(3);
    expect(stepOf({ kind: "import", status: "applying" })).toBe(4);
    expect(stepOf({ kind: "import", status: "failed" })).toBe(4);
    expect(stepOf({ kind: "import", status: "applied" })).toBe(5);
  });
});

describe("fmtSize", () => {
  it("KB dưới 1 MB, MB một chữ số thập phân dấu phẩy", () => {
    expect(fmtSize(1)).toBe("1 KB");
    expect(fmtSize(2048)).toBe("2 KB");
    expect(fmtSize(1024 * 1024)).toBe("1,0 MB");
    expect(fmtSize(2.4 * 1024 * 1024)).toBe("2,4 MB");
  });
});
