// Luật thuần của màn Khách (kế hoạch M2 Task M2-29): chỉ số và đơn vị của quy tắc nhãn (máy chủ lưu tỷ lệ hoàn theo
// phần vạn, màn nhập %), màu nhãn → lớp chip có sẵn, câu cho mã lỗi nhập khách, đánh dấu đuôi SĐT khớp, các lần gộp
// đọc từ dòng thời gian và hạn hoàn tác 30 ngày (Q14).
import type { CustomerTagLink, TagCondition, TimelineItem } from "../../api";

export type TagField = TagCondition["field"];
export type TagOp = TagCondition["op"];

type Unit = "count" | "money" | "percent" | "days";

/** Chỉ số khách dùng được trong quy tắc nhãn, theo thứ tự trong ô chọn (mockup trạng thái E). */
export const TAG_FIELDS: readonly { field: TagField; label: string; unit: Unit }[] = [
  { field: "return_rate_bp", label: "Tỷ lệ hoàn (%)", unit: "percent" },
  { field: "order_count", label: "Số đơn", unit: "count" },
  { field: "delivered_count", label: "Số đơn thành công", unit: "count" },
  { field: "returned_count", label: "Số đơn hoàn", unit: "count" },
  { field: "total_spent", label: "Đã chi (đồng)", unit: "money" },
  { field: "days_since_last_order", label: "Số ngày từ lần mua cuối", unit: "days" },
];

export const OP_TEXT: Readonly<Record<TagOp, string>> = { gte: "từ", lte: "tới" };

const unitOf = (f: TagField): Unit => TAG_FIELDS.find((x) => x.field === f)?.unit ?? "count";

/** Giá trị lớn nhất máy chủ nhận (2^53 − 1, TagConditionInput.value). */
const MAX_VALUE = Number.MAX_SAFE_INTEGER;
const BP_PER_PERCENT = 100;
const MAX_PERCENT_BP = 10_000;

/** Câu nhắc khi giá trị không đổi được (theo đơn vị của chỉ số). */
export function valueHint(f: TagField): string {
  return unitOf(f) === "percent" ? "nhập % từ 0 tới 100, tối đa hai số lẻ" : "nhập số nguyên từ 0";
}

/**
 * Chữ người dùng gõ → giá trị gửi máy chủ; null khi sai. Tỷ lệ hoàn: % (dấu phẩy hay chấm thập phân, tối đa hai số lẻ)
 * → phần vạn. Chỉ số khác: số nguyên không âm, cho phép dấu chấm ngăn hàng nghìn ("5.000.000").
 */
export function toServerValue(f: TagField, text: string): number | null {
  const t = text.trim();
  if (unitOf(f) === "percent") {
    const m = /^(\d{1,3})(?:[.,](\d{1,2}))?$/.exec(t);
    if (m === null) return null;
    const bp = Number(m[1]) * BP_PER_PERCENT + Number((m[2] ?? "").padEnd(2, "0"));
    return bp <= MAX_PERCENT_BP ? bp : null;
  }
  if (!/^\d{1,3}(\.\d{3})*$|^\d+$/.test(t)) return null;
  const n = Number(t.replace(/\./g, ""));
  return Number.isSafeInteger(n) && n <= MAX_VALUE ? n : null;
}

/** Giá trị máy chủ → chữ hiện trong ô (ngược của toServerValue). */
export function toDisplayValue(f: TagField, value: number): string {
  if (unitOf(f) === "percent") return (value / BP_PER_PERCENT).toLocaleString("vi-VN", { maximumFractionDigits: 2 });
  if (unitOf(f) === "money") return value.toLocaleString("vi-VN");
  return String(value);
}

/** Tỷ lệ hoàn phần vạn → "18,18%". */
export function percentText(bp: number): string {
  return `${toDisplayValue("return_rate_bp", bp)}%`;
}

/** 10 khóa màu nhãn (customer.TagColors của máy chủ) và tên hiện trong ô chọn. */
export const TAG_COLORS: readonly { key: string; name: string }[] = [
  { key: "gray", name: "Xám" },
  { key: "red", name: "Đỏ" },
  { key: "orange", name: "Cam" },
  { key: "yellow", name: "Vàng" },
  { key: "green", name: "Xanh lá" },
  { key: "teal", name: "Xanh ngọc" },
  { key: "blue", name: "Xanh dương" },
  { key: "purple", name: "Tím" },
  { key: "pink", name: "Hồng" },
  { key: "brown", name: "Nâu" },
];

// Hệ thiết kế chỉ có bốn kiểu chip (không thêm màu mới): đỏ/cam/hồng → chip lỗi, xanh lá/ngọc → chip tốt, nâu → chip
// đậm, còn lại → chip viền.
const CHIP_OF: Readonly<Record<string, string>> = {
  red: "chip chip-err",
  orange: "chip chip-err",
  pink: "chip chip-err",
  green: "chip chip-ok",
  teal: "chip chip-ok",
  brown: "chip chip-dark",
};

export function tagChip(color: string): string {
  return Object.hasOwn(CHIP_OF, color) ? (CHIP_OF[color] ?? "chip chip-line") : "chip chip-line";
}

export const TAG_SOURCE_TEXT: Readonly<Record<CustomerTagLink["source"], string>> = {
  staff: "gắn tay",
  rule: "theo quy tắc",
};

/** Câu cho mã lỗi một dòng (ImportError.code, internal/modules/customer/importer_rows.go) và lỗi cả tệp (dòng 0). */
export const IMPORT_ERROR_TEXT: Readonly<Record<string, string>> = {
  required: "Thiếu ô bắt buộc",
  invalid_name: "Họ tên không hợp lệ",
  invalid_phone: "SĐT không hợp lệ",
  invalid_date: "Ngày sinh không đúng dạng dd/mm/yyyy",
  unknown_province: "Không có tỉnh/thành này",
  unknown_ward: "Không có xã/phường này",
  ward_not_in_province: "Xã/phường không thuộc tỉnh đã chọn",
  invalid_text: "Ô có ký tự không dùng được hoặc quá dài",
  tag_too_long: "Tên nhãn quá 50 ký tự",
  too_many_tags: "Quá nhiều nhãn trong một ô",
  import_too_large: "Tệp quá 20.000 dòng hoặc quá 5 MB",
  import_file_invalid: "Tệp không đọc được. Lưu lại bằng Excel dạng .xlsx từ tệp mẫu",
  import_failed: "Lần nhập dừng giữa chừng. Nhập lại cùng tệp, dòng đã thêm không bị trùng",
};

export function importErrorText(code: string): string {
  return Object.hasOwn(IMPORT_ERROR_TEXT, code) ? (IMPORT_ERROR_TEXT[code] ?? code) : `Dòng không hợp lệ (${code})`;
}

/** Tách chuỗi SĐT đã định dạng thành [phần đầu, phần chứa n chữ số cuối] để đánh dấu đuôi số khớp. */
export function markTail(display: string, n: number): [string, string] {
  if (n <= 0) return [display, ""];
  let seen = 0;
  for (let i = display.length - 1; i >= 0; i--) {
    if (/\d/.test(display.charAt(i))) {
      seen += 1;
      if (seen === n) return [display.slice(0, i), display.slice(i)];
    }
  }
  return ["", display];
}

/** Một lần gộp đọc từ dòng thời gian (mục kind merge, lúc hoàn tác từ mục merge_undone cùng refId). */
export interface MergeEntry {
  id: string;
  at: string;
  survivorId: string;
  mergedId: string;
  actor: string;
  undoneAt: string | undefined;
}

/** Các lần gộp có trong các mục dòng thời gian đã tải, mới nhất trước. */
export function mergeEntries(items: readonly TimelineItem[]): MergeEntry[] {
  const undone = new Map(items.filter((i) => i.kind === "merge_undone").map((i) => [i.refId, i.at]));
  return items
    .filter((i) => i.kind === "merge")
    .map((i) => ({
      id: i.refId,
      at: i.at,
      survivorId: i.summary?.survivor_id ?? "",
      mergedId: i.summary?.merged_id ?? "",
      actor: i.actor?.name ?? "",
      undoneAt: undone.get(i.refId),
    }))
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

const UNDO_WINDOW_MS = 30 * 86_400_000;

/** Hạn cuối hoàn tác một lần gộp: 30 ngày sau lúc gộp (Q14). */
export function undoDeadline(at: string): Date {
  return new Date(Date.parse(at) + UNDO_WINDOW_MS);
}

/** Trạng thái hoàn tác hiện trên dòng; máy chủ vẫn là nơi quyết (merge_not_latest, merge_window_expired). */
export function undoState(e: MergeEntry, now: number): "undone" | "expired" | "open" {
  if (e.undoneAt !== undefined) return "undone";
  return now > undoDeadline(e.at).getTime() ? "expired" : "open";
}
