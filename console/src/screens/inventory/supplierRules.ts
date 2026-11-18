// Luật nhỏ của màn Nhà cung cấp (kế hoạch M1 Task 327). Số điện thoại kiểm cùng luật máy chủ
// (internal/modules/inventory/suppliers.go: bỏ dấu cách, chấm, gạch; rỗng hoặc 8–15 chữ số, có thể có + đầu) để báo
// ngay dưới ô; máy chủ vẫn là nơi quyết (invalid_input).
import type { Supplier } from "../../api";
import { queryKey } from "../../app/queryClient";

export const TEXT_NAME_TAKEN = "Tên này đã có. Chọn nhà cung cấp có sẵn hoặc đặt tên khác.";
export const TEXT_BAD_PHONE = "Số điện thoại cần 8–15 chữ số, có thể bắt đầu bằng +.";
export const TEXT_CONFLICT_TITLE = "Nhà cung cấp đã thay đổi, tải lại.";
export const TEXT_CONFLICT_DETAIL = "Có người vừa sửa nhà cung cấp này. Thay đổi của bạn chưa được lưu.";
export const TEXT_EMPTY = "Chưa có nhà cung cấp nào";
export const TEXT_NO_MATCH = "Không có nhà cung cấp nào khớp";

export const NAME_MAX = 200;
export const PHONE_MAX = 30;
export const NOTE_MAX = 1000;

const PHONE_RE = /^\+?[0-9]{8,15}$/;

/** Bỏ dấu cách, chấm, gạch như máy chủ. */
export function normalizePhone(s: string): string {
  return s.replace(/[ .-]/g, "");
}

/** Rỗng hoặc đúng dạng (sau khi chuẩn hóa). */
export function phoneValid(s: string): boolean {
  const p = normalizePhone(s);
  return p === "" || PHONE_RE.test(p);
}

/** Số di động 10 chữ số bắt đầu bằng 0 hiện theo nhóm 4-3-3 như mockup ("0912 345 678"); dạng khác giữ nguyên. */
export function formatPhone(p: string): string {
  const m = /^(0\d{3})(\d{3})(\d{3})$/.exec(p);
  return m === null ? p : `${m[1] ?? ""} ${m[2] ?? ""} ${m[3] ?? ""}`;
}

/** Nhãn "N / 1.000 ký tự" dưới ô ghi chú (đếm theo ký tự như máy chủ, không theo đơn vị UTF-16). */
export function noteCount(note: string): string {
  return `${Array.from(note).length.toLocaleString("vi-VN")} / ${NOTE_MAX.toLocaleString("vi-VN")} ký tự`;
}

/**
 * Có hiện dòng "+ Thêm nhà cung cấp" trong ô chọn không: tên gõ (đã cắt khoảng trắng) không rỗng và chưa trùng hẳn tên
 * nhà cung cấp nào trong danh sách (máy chủ chặn trùng theo đúng tên, kể cả nhà cung cấp đã lưu trữ).
 */
export function canQuickAdd(typed: string, list: readonly Supplier[]): boolean {
  const name = typed.trim();
  return name !== "" && Array.from(name).length <= NAME_MAX && !list.some((s) => s.name === name);
}

/** Khóa truy vấn danh sách nhà cung cấp (không có chủ đề realtime); ghi xong làm mới mọi truy vấn bắt đầu bằng khóa này. */
export const SUPPLIERS_KEY = queryKey("inventory.suppliers");
