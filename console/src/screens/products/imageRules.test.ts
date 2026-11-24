// Luật thuần của khối Ảnh (Task 325; mockup docs/mockups/m1/san-pham-sua.html, trạng thái E).
import { describe, expect, it } from "vitest";
import { ApiError, NetworkError, TEXT_NETWORK } from "../../app/errors";
import { colorChoices, moveTo, rejectText, replaceOrder, uploadErrorText } from "./imageRules";

describe("rejectText — câu theo lý do máy chủ từ chối ảnh", () => {
  it.each([
    ["not_image", "Tệp không phải ảnh"],
    ["type_not_allowed", "Loại ảnh không nhận, chọn JPEG, PNG hoặc WebP"],
    ["too_large", "Ảnh quá 20 MB"],
    ["too_many_pixels", "Ảnh quá lớn (trên 40 triệu điểm ảnh)"],
    ["decode_failed", "Ảnh hỏng, không mở được"],
    ["size_mismatch", "Tải lên chưa trọn, chọn lại ảnh"],
    ["too_small", "Ảnh quá nhỏ: cần rộng ít nhất 600 px. Xuất lại ảnh rồi tải lên"],
    ["processing_failed", "Xử lý ảnh không xong, chọn lại ảnh"],
    ["timeout", "Xử lý ảnh không xong, chọn lại ảnh"],
    ["upload_failed", "Xử lý ảnh không xong, chọn lại ảnh"],
    ["lý do lạ", "Xử lý ảnh không xong, chọn lại ảnh"],
    [undefined, "Xử lý ảnh không xong, chọn lại ảnh"],
  ])("%s → %s", (reason, text) => {
    expect(rejectText(reason)).toBe(text);
  });
});

describe("uploadErrorText — lỗi lúc xin đường tải lên", () => {
  it("loại tệp, cỡ tệp dùng câu của ảnh; lỗi khác dùng câu chung", () => {
    const api = (code: string) => new ApiError({ status: 400, code, details: {} });
    expect(uploadErrorText(api("file_type_not_allowed"))).toBe("Loại ảnh không nhận, chọn JPEG, PNG hoặc WebP");
    expect(uploadErrorText(api("file_too_large"))).toBe("Ảnh quá 20 MB");
    expect(uploadErrorText(new NetworkError())).toBe(TEXT_NETWORK);
  });
});

describe("moveTo, replaceOrder — thứ tự gửi reorderProductImages", () => {
  it("dời một ảnh sang vị trí khác, giữ đủ mã", () => {
    expect(moveTo(["a", "b", "c"], 0, 1)).toEqual(["b", "a", "c"]);
    expect(moveTo(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(moveTo(["a", "b", "c"], 1, 1)).toEqual(["a", "b", "c"]);
    expect(moveTo(["a", "b"], 0, 5)).toEqual(["b", "a"]);
  });

  it("ảnh mới vào đúng chỗ ảnh mất, ảnh mất đứng ngay sau (sẽ xóa)", () => {
    expect(replaceOrder(["a", "lost", "c", "new"], "lost", "new")).toEqual(["a", "new", "lost", "c"]);
  });
});

describe("colorChoices — ô chọn màu của ảnh", () => {
  it("màu của biến thể (không trùng), rồi Chưa gắn", () => {
    expect(colorChoices(undefined, ["Đen", "Be", "Đen"])).toEqual([
      { value: "Đen", label: "Màu: Đen" },
      { value: "Be", label: "Màu: Be" },
      { value: "", label: "Chưa gắn" },
    ]);
  });

  it("màu trên ảnh không còn biến thể nào (đổi tên màu) → vẫn hiện, ghi rõ", () => {
    expect(colorChoices("Kem", ["Be"])).toEqual([
      { value: "Kem", label: "Màu: Kem (không còn biến thể)" },
      { value: "Be", label: "Màu: Be" },
      { value: "", label: "Chưa gắn" },
    ]);
  });
});
