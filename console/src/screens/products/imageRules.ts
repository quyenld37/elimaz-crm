// Luật thuần của khối Ảnh trên màn Sửa mẫu (kế hoạch M1 Task 325; mockup docs/mockups/m1/san-pham-sua.html, trạng
// thái E): câu theo lý do ảnh bị từ chối, thứ tự gửi reorderProductImages, lựa chọn màu của ảnh.
import { ApiError, errorMessage } from "../../app/errors";

/** Mục đích tệp của ảnh sản phẩm (platform/files: product_image). */
export const IMAGE_PURPOSE = "product_image";
/** Loại ảnh nhận (khớp mục đích product_image). */
export const IMAGE_ACCEPT = "image/jpeg,image/png,image/webp";

const TEXT_PROCESSING_FAILED = "Xử lý ảnh không xong, chọn lại ảnh";
const TEXT_TYPE = "Loại ảnh không nhận, chọn JPEG, PNG hoặc WebP";
const TEXT_TOO_LARGE = "Ảnh quá 20 MB";
/** PUT lên kho không xong (mạng, kho trả lỗi). */
export const TEXT_UPLOAD_INCOMPLETE = "Tải lên chưa trọn, chọn lại ảnh";

const REJECT: Readonly<Record<string, string>> = {
  not_image: "Tệp không phải ảnh",
  type_not_allowed: TEXT_TYPE,
  too_large: TEXT_TOO_LARGE,
  too_many_pixels: "Ảnh quá lớn (trên 40 triệu điểm ảnh)",
  decode_failed: "Ảnh hỏng, không mở được",
  size_mismatch: TEXT_UPLOAD_INCOMPLETE,
};

/** Câu hiện trên dòng tải lên khi máy chủ từ chối ảnh (rejectReason); lý do khác hay thiếu → câu xử lý không xong. */
export function rejectText(reason: string | undefined): string {
  return (reason === undefined ? undefined : REJECT[reason]) ?? TEXT_PROCESSING_FAILED;
}

/** Câu khi ảnh xử lý quá thời gian chờ. */
export const TEXT_PROCESSING_TIMEOUT = TEXT_PROCESSING_FAILED;

/** Câu khi createUpload, completeUpload lỗi: loại, cỡ tệp dùng câu của ảnh; lỗi khác câu chung. */
export function uploadErrorText(err: unknown): string {
  if (err instanceof ApiError && err.code === "file_type_not_allowed") return TEXT_TYPE;
  if (err instanceof ApiError && err.code === "file_too_large") return TEXT_TOO_LARGE;
  return errorMessage(err);
}

/** Dời phần tử ở vị trí from sang vị trí to (to quá cuối → cuối). */
export function moveTo<T>(ids: readonly T[], from: number, to: number): T[] {
  const out = [...ids];
  const [x] = out.splice(from, 1);
  if (x === undefined) return [...ids];
  out.splice(Math.min(Math.max(to, 0), out.length), 0, x);
  return out;
}

/** Thứ tự sau khi gắn ảnh thay ảnh mất: ảnh mới đứng đúng chỗ ảnh mất, ảnh mất ngay sau (rồi bị xóa). */
export function replaceOrder(ids: readonly string[], lostId: string, newId: string): string[] {
  return ids.flatMap((x) => (x === newId ? [] : x === lostId ? [newId, lostId] : [x]));
}

export interface ColorChoice {
  value: string;
  label: string;
}

/**
 * Lựa chọn của ô "Màu của ảnh": màu đang ghi trên ảnh nếu không còn biến thể nào mang màu đó (đổi tên màu biến thể
 * không kéo theo ảnh), các màu của biến thể (không trùng), rồi "Chưa gắn" (ảnh chung, value rỗng).
 */
export function colorChoices(current: string | undefined, colors: readonly string[]): ColorChoice[] {
  const uniq = [...new Set(colors)];
  const out: ColorChoice[] = [];
  if (current !== undefined && current !== "" && !uniq.includes(current))
    out.push({ value: current, label: `Màu: ${current} (không còn biến thể)` });
  for (const c of uniq) out.push({ value: c, label: `Màu: ${c}` });
  out.push({ value: "", label: "Chưa gắn" });
  return out;
}
