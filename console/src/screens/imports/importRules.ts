// Luật thuần của màn Nhập Excel (kế hoạch M1 Task 52; mockup docs/mockups/m1/nhap-excel.html): kiểm tệp trước khi
// tải lên, định dạng số và giờ, tên tệp mẫu tải về, bước đang làm.
import type { CatalogImport, CatalogImportSummary } from "../../api";

/** Mục đích tệp của bản nhập (platform/files: catalog_import). */
export const IMPORT_PURPOSE = "catalog_import";
/** Loại tệp duy nhất máy chủ nhận cho catalog_import. */
export const XLSX_TYPE = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
/** Cỡ tối đa của catalog_import (máy chủ: 5 MB). */
export const MAX_BYTES = 5 * 1024 * 1024;
/** Tên tệp mẫu khi máy chủ không gửi Content-Disposition. */
export const TEMPLATE_NAME = "mau-nhap-san-pham.xlsx";
/** Hỏi lại bản nhập đang kiểm hay đang áp (ms). */
export const POLL_MS = 2000;

/** null khi tệp gửi được; not_xlsx khi không phải .xlsx hay rỗng; too_large khi quá 5 MB. */
export function checkLocalFile(f: { name: string; size: number }): "not_xlsx" | "too_large" | null {
  if (!/.\.xlsx$/i.test(f.name) || f.size <= 0) return "not_xlsx";
  if (f.size > MAX_BYTES) return "too_large";
  return null;
}

/** Số nguyên có dấu chấm ngăn nghìn: 1.000. */
export function fmtInt(n: number): string {
  return String(Math.trunc(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

const two = (n: number) => String(n).padStart(2, "0");

/** "10:02 18/11" theo giờ máy. */
export function fmtTime(d: Date): string {
  return `${two(d.getHours())}:${two(d.getMinutes())} ${two(d.getDate())}/${two(d.getMonth() + 1)}`;
}

/** Phần trăm nguyên trong 0–100; total 0 → 0. */
export function percent(loaded: number, total: number): number {
  if (total <= 0) return 0;
  return Math.min(100, Math.max(0, Math.floor((loaded * 100) / total)));
}

/** Tên tệp trong Content-Disposition (chỉ phần tên, bỏ đường dẫn); thiếu → TEMPLATE_NAME. */
export function fileNameFrom(cd: string | null): string {
  const m = cd === null ? null : /filename="?([^";]+)"?/i.exec(cd);
  const raw = m?.[1] ?? "";
  const base = raw.split(/[/\\]/).pop()?.trim() ?? "";
  return base === "" ? TEMPLATE_NAME : base;
}

/** Bản nhập chỉ có lỗi của cả tệp (dòng 0): tệp không đọc được, không có bảng lỗi theo dòng. */
export function fileErrors(imp: CatalogImport): boolean {
  return imp.errors.length > 0 && imp.errors.every((e) => e.row === 0);
}

/** Ô "Dòng" của bảng lỗi: dòng 0 là lỗi của cả tệp. */
export function rowText(row: number): string {
  return row === 0 ? "Cả tệp" : fmtInt(row);
}

/**
 * Ô "Cột" của bảng lỗi: cột rỗng là lỗi của cả dòng. Máy chủ trả tiêu đề cột của tệp mẫu, cột bắt buộc có "*" ở cuối
 * ("Tên mẫu*"); màn bỏ dấu đó như mockup nhap-excel.html.
 */
export function columnText(column: string): string {
  return column === "" ? "Cả dòng" : column.replace(/\*$/, "");
}

/** Tổng số biến thể của tệp hợp lệ. */
export function variantTotal(s: CatalogImportSummary): number {
  return s.create + s.update + s.unchanged;
}

/** Trạng thái màn: chưa chọn tệp, đang tải lên, hay đang xem một bản nhập. */
export type View =
  { kind: "idle"; downloaded: boolean } | { kind: "uploading" } | { kind: "import"; status: CatalogImport["status"] };

/** Bước đang làm của dải bốn bước (1–4); 5 là đã xong cả bốn. */
export function stepOf(v: View): number {
  if (v.kind === "idle") return v.downloaded ? 2 : 1;
  if (v.kind === "uploading") return 3;
  switch (v.status) {
    case "applying":
    case "failed":
      return 4;
    case "applied":
      return 5;
    default:
      return 3;
  }
}

/** Cỡ tệp: "2,4 MB" từ 1 MB, dưới đó "12 KB" (làm tròn lên, tối thiểu 1 KB). */
export function fmtSize(bytes: number): string {
  const mb = 1024 * 1024;
  if (bytes >= mb) return `${(Math.round((bytes * 10) / mb) / 10).toFixed(1).replace(".", ",")} MB`;
  return `${fmtInt(Math.max(1, Math.ceil(bytes / 1024)))} KB`;
}
