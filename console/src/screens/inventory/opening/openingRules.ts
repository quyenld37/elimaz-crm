// Luật thuần của màn Kiểm kho mở sổ (Task 273, M7-28; mockup docs/mockups/m7/kiem-kho-mo-so.html, HV03–HV09). Số liệu
// do máy chủ tính (view inventory.opening_status_v); màn chỉ phân loại và viết câu.
import type { CountImportRowError, OpeningStocktake } from "../../../api";

/** Số mã còn phải đếm (không âm: mã bật bán rồi tắt trong lúc đếm có thể làm số đã đếm vượt số phải đếm). */
export function remaining(o: Pick<OpeningStocktake, "linesExpected" | "linesCounted">): number {
  return Math.max(0, o.linesExpected - o.linesCounted);
}

/** Lý do nút "Đăng mở sổ" khóa; null khi đăng được (HV06: phải có số cho mọi mã đang bán). */
export function postBlockedReason(o: OpeningStocktake): string | null {
  if (o.status === "applied") return "Phiên này đã đăng.";
  if (o.status === "cancelled") return "Phiên này đã hủy.";
  if (o.linesExpected === 0) return "Kho này chưa có mã hàng nào đang bán để đếm.";
  const n = remaining(o);
  return n > 0 ? `Còn ${String(n)} mã chưa đếm. Đếm hoặc nhập 0 cho cả ${String(n)} mã thì nút mở.` : null;
}

export function canPost(o: OpeningStocktake): boolean {
  return postBlockedReason(o) === null;
}

const ROW_ERRORS: Readonly<Record<CountImportRowError["code"], string>> = {
  unknown_code: "Không thấy mã trong danh mục",
  ambiguous_code: "Mã có ở 2 người bán: ghi SKU thay barcode",
  not_integer: "Số đếm không phải số nguyên",
  negative: "Số đếm âm",
  formula: "Ô có công thức: dán giá trị",
};

/** Lỗi một dòng của tệp số đếm: chỉ số dòng và câu theo mã, không bao giờ nội dung ô (HV09). */
export function importRowText(e: CountImportRowError): string {
  return `Dòng ${String(e.row)}: ${ROW_ERRORS[e.code]}`;
}

/** Cột "Lỗi" của bảng lỗi từng dòng. */
export function importRowReason(e: CountImportRowError): string {
  return ROW_ERRORS[e.code];
}

/** "+1", "−1" (dấu trừ thật U+2212), "+3". */
export function scanLabel(by: number): string {
  return by < 0 ? `−${String(-by)}` : `+${String(by)}`;
}
