import { describe, expect, it } from "vitest";
import { lostAtText, purposeLabel } from "./lostFilesRules";

describe("purposeLabel", () => {
  it("tên tiếng Việt cho mục đích M1, khóa lạ hiện nguyên", () => {
    expect(purposeLabel("product_image")).toBe("Ảnh sản phẩm");
    expect(purposeLabel("catalog_import")).toBe("Tệp nhập Excel");
    expect(purposeLabel("test_export")).toBe("test_export");
  });
});

describe("lostAtText", () => {
  it('"HH:mm dd/MM" theo giờ Việt Nam, không theo múi giờ máy', () => {
    expect(lostAtText("2026-11-17T20:00:00Z")).toBe("03:00 18/11");
    expect(lostAtText("2026-01-05T16:59:00Z")).toBe("23:59 05/01");
    expect(lostAtText("2026-12-31T17:05:00Z")).toBe("00:05 01/01");
  });
  it("chuỗi không phải thời gian → —", () => {
    expect(lostAtText("không phải ngày")).toBe("—");
  });
});
