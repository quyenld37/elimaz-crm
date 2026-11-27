// Luật hiển thị của màn Đối soát COD (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html). Khớp dòng,
// tính chênh, tổng theo nhóm đều ở máy chủ (cash); ở đây chỉ dựng chữ và chọn nhãn.
import type {
  CodDiscrepancy,
  CodMatchStatus,
  CodMatchTotal,
  CodStatement,
  CodStatementParseError,
  CodStatementStatus,
} from "../../api";
import { VN_TIME_ZONE } from "../../app/time";
import { formatMoney } from "../products/productRules";

export { formatMoney };

/** Chủ đề realtime của bảng kê (máy chủ: cash.SignalStatementChanged); báo hiệu khi nhập, đọc tệp, khớp, chốt. */
export const COD_TOPIC = "cod_statement.changed";

/** Mục đích tệp bảng kê (cash.StatementPurposeKey). */
export const STATEMENT_PURPOSE = "cod_statement";
export const XLSX_TYPE = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
export const CSV_TYPE = "text/csv";
export const ACCEPT = `.xlsx,.csv,${XLSX_TYPE},${CSV_TYPE}`;
/** Bảng kê lớn nhất máy chủ nhận (cash.MaxStatementBytes). */
export const MAX_BYTES = 10 * 1024 * 1024;
/** Định dạng chung của CRM (formats.ElimazV1Key); rỗng là mặc định của hãng (máy chủ chọn). */
export const ELIMAZ_FORMAT = "elimaz_v1";
export const PAGE = 50;

export const TEXT_CHANGED = "Bảng kê vừa được người khác chốt hoặc đổi — tải lại.";
export const TEXT_BAD_FILE = "Chỉ nhận tệp Excel (.xlsx) hoặc CSV, tối đa 10 MB.";
export const TEXT_UPLOAD_INCOMPLETE = "Tải lên chưa trọn. Chọn lại tệp để tải lên lần nữa.";
export const TEXT_NO_ACCOUNTS =
  "Vai trò của bạn chưa có quyền xem tài khoản hãng (kết nối vận chuyển) nên chưa chọn được tài khoản để nhập. Nhờ Chủ shop nhập, hoặc cấp quyền xem kết nối vận chuyển cho vai trò này.";

const CARRIER_NAMES: Readonly<Record<string, string>> = {
  vtp: "Viettel Post",
  spx: "SPX",
  best: "Best Express",
  ghn: "GHN",
  ghtk: "GHTK",
  self: "Tự giao",
};

/** Tên hãng theo khóa (khóa lạ: hiện nguyên khóa). */
export function carrierName(key: string): string {
  return CARRIER_NAMES[key] ?? key;
}

/** Tên tài khoản hãng của bảng kê; máy chủ không đọc được tên thì tên hãng. */
export function accountTitle(st: CodStatement): string {
  return st.carrierAccountName ?? carrierName(st.carrierKey);
}

/** "2026-11-08" → "08/11". */
export function dayMonth(date: string): string {
  const [, m, d] = date.split("-");
  return `${d ?? ""}/${m ?? ""}`;
}

/** Kỳ bảng kê "08/11–14/11"; chưa đọc xong tệp → "—". */
export function periodText(st: CodStatement): string {
  if (st.periodFrom === undefined || st.periodTo === undefined) return "—";
  return `${dayMonth(st.periodFrom)}–${dayMonth(st.periodTo)}`;
}

const PARTS = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  hourCycle: "h23",
});

/** Giờ Việt Nam "16:05 · 21/11". */
export function timeDay(iso: string): string {
  const p = Object.fromEntries(PARTS.formatToParts(new Date(iso)).map((x) => [x.type, x.value]));
  return `${p.hour ?? ""}:${p.minute ?? ""} · ${p.day ?? ""}/${p.month ?? ""}`;
}

/** Ngày Việt Nam "21/11" của một mốc giờ. */
export function vnDay(iso: string): string {
  return timeDay(iso).split(" · ")[1] ?? "";
}

/** "Nhập 16:05 · 21/11 · Mai Thị Kế". */
export function importedText(st: CodStatement): string {
  const who = st.importedBy?.name ?? "";
  return `Nhập ${timeDay(st.createdAt)}${who === "" ? "" : ` · ${who}`}`;
}

/** Tiền có dấu: "+100.000 ₫", "−90.000 ₫" (dấu trừ U+2212), "0 ₫". */
export function signedMoney(v: number): string {
  if (v === 0) return formatMoney(0);
  return `${v > 0 ? "+" : "−"}${formatMoney(Math.abs(v))}`;
}

/** Phí hãng hiện là số trừ vào tiền shop: 32.000 → "−32.000 ₫". */
export function feeMoney(fee: number): string {
  return signedMoney(-fee);
}

export type Tone = "busy" | "err" | "dark" | "ok" | "line";

export interface StatusView {
  text: string;
  tone: Tone;
}

/** Nhãn trạng thái bảng kê. */
export function statusView(st: CodStatement): StatusView {
  const by: Record<CodStatementStatus, StatusView> = {
    imported: { text: "Đang phân tích", tone: "busy" },
    parsing: { text: "Đang phân tích", tone: "busy" },
    parse_failed: { text: "Lỗi tệp", tone: "err" },
    parsed: { text: "Đang khớp", tone: "busy" },
    matched: { text: "Đã khớp · chờ chốt", tone: "dark" },
    confirmed: {
      text: st.confirmedAt === undefined ? "Đã chốt" : `Đã chốt ${vnDay(st.confirmedAt)}`,
      tone: "ok",
    },
  };
  return by[st.status];
}

/** Máy chủ còn đọc tệp hoặc đang khớp (màn chờ báo hiệu). */
export function isWorking(st: CodStatement): boolean {
  return st.status === "imported" || st.status === "parsing" || st.status === "parsed";
}

const COLUMN_TEXT: Readonly<Record<string, string>> = {
  ma_van_don: "Mã vận đơn",
  tien_cod: "Tiền thu hộ",
  phi_van_chuyen: "Phí vận chuyển",
  phi_khac: "Phí khác",
  thuc_nhan: "Thực nhận",
  ngay_chuyen: "Ngày chuyển",
};

/** Tên cột bằng tiếng Việt (cột của mẫu chung ELIMAZ); cột theo mẫu của hãng đã là chữ trên tệp nên giữ nguyên. */
export function columnText(column: string): string {
  return COLUMN_TEXT[column] ?? column;
}

/** Câu ngắn cho lỗi đọc tệp (cột trạng thái của danh sách). */
export function parseErrorText(pe: CodStatementParseError | undefined): string {
  if (pe === undefined) return "Không đọc được tệp";
  const col = `"${columnText(pe.column ?? "")}"`;
  const line = pe.line === undefined ? "" : String(pe.line);
  switch (pe.code) {
    case "missing_column":
      return `Thiếu cột ${col}`;
    case "duplicate_column":
      return `Hai cột cùng là ${col}`;
    case "missing_value":
      return `Ô ${col} trống ở dòng ${line}`;
    case "bad_amount":
      return `Số tiền không đọc được ở dòng ${line}, cột ${col}`;
    case "bad_date":
      return `Ngày không đọc được ở dòng ${line}, cột ${col}`;
    case "too_many_rows":
      return "Tệp quá 20.000 dòng";
    case "too_many_columns":
      return `Dòng ${line} có quá nhiều cột`;
    case "cell_too_long":
      return `Ô quá dài ở dòng ${line}`;
    case "too_large":
      return "Tệp quá 10 MB";
    case "formula_not_allowed":
      return `Tệp có công thức ở dòng ${line}`;
    case "bad_file":
      return "Không đọc được tệp (Excel hỏng hoặc CSV sai dấu nháy)";
    case "bad_encoding":
      return "Tệp CSV không phải UTF-8";
    case "no_lines":
      return "Tệp không có dòng nào";
    case "unknown_format":
      return "Định dạng bảng kê không còn dùng được";
    case "file_unavailable":
      return "Tệp không còn trên kho tệp";
    default:
      return "Không đọc được tệp";
  }
}

/** Câu đủ cho hộp lỗi tệp: câu ngắn kèm cách sửa. */
export function parseErrorHelp(pe: CodStatementParseError | undefined): string {
  switch (pe?.code) {
    case "missing_column":
    case "duplicate_column":
      return "Kiểm lại định dạng đã chọn hoặc dùng mẫu chung ELIMAZ.";
    case "formula_not_allowed":
      return "Lưu lại thành giá trị rồi nhập.";
    case "read_failed":
      return "Máy chủ đang bận. Nhập lại sau ít phút.";
    default:
      return "Sửa tệp rồi nhập lại.";
  }
}

/** Kiểm trước ở máy: đuôi .xlsx/.csv, không quá 10 MB (máy chủ vẫn kiểm lại). */
export function checkLocalFile(f: { name: string; size: number }): boolean {
  return /\.(xlsx|csv)$/i.test(f.name) && f.size > 0 && f.size <= MAX_BYTES;
}

/** Kiểu nội dung gửi kho tệp theo đuôi tệp (trình duyệt hay để trống với .csv). */
export function contentTypeOf(name: string): string {
  return /\.csv$/i.test(name) ? CSV_TYPE : XLSX_TYPE;
}

/** "84 KB", "1,2 MB". */
export function sizeText(bytes: number): string {
  if (bytes < 1024 * 1024) return `${String(Math.max(1, Math.round(bytes / 1024)))} KB`;
  return `${(bytes / 1024 / 1024).toLocaleString("vi-VN", { maximumFractionDigits: 1 })} MB`;
}

export interface GroupView {
  status: CodMatchStatus;
  label: string;
  note?: string;
  tone?: "bad" | "off";
}

/** Năm nhóm của mockup (HV31), theo thứ tự hiện; "Chưa khớp" chỉ có khi máy chủ chưa khớp xong. */
export const GROUPS: readonly GroupView[] = [
  { status: "matched", label: "Khớp" },
  { status: "amount_mismatch", label: "Lệch tiền", tone: "bad" },
  { status: "external", label: "Ngoài hệ thống", note: "Không tính vào số liệu", tone: "off" },
  { status: "duplicate", label: "Trùng" },
  { status: "fee_only", label: "Chỉ phí", note: "Phí hãng, không có thu hộ" },
  { status: "pending", label: "Chưa khớp" },
];

export function groupOf(status: CodMatchStatus): GroupView {
  return GROUPS.find((g) => g.status === status) ?? { status, label: status };
}

const zero = (status: CodMatchStatus): CodMatchTotal => ({ matchStatus: status, count: 0, codAmount: 0 });

/** Tổng của một nhóm (nhóm không có dòng → 0). */
export function totalOf(totals: readonly CodMatchTotal[], status: CodMatchStatus): CodMatchTotal {
  return totals.find((t) => t.matchStatus === status) ?? zero(status);
}

/** Nhóm hiện trên màn: năm nhóm của mockup luôn có; "Chưa khớp" chỉ khi có dòng. */
export function visibleGroups(totals: readonly CodMatchTotal[]): GroupView[] {
  return GROUPS.filter((g) => g.status !== "pending" || totalOf(totals, "pending").count > 0);
}

/** Nhóm mở sẵn: Lệch tiền nếu có (việc cần xem trước), không thì nhóm đầu có dòng, không thì Khớp. */
export function defaultGroup(totals: readonly CodMatchTotal[]): CodMatchStatus {
  if (totalOf(totals, "amount_mismatch").count > 0) return "amount_mismatch";
  return GROUPS.find((g) => totalOf(totals, g.status).count > 0)?.status ?? "matched";
}

export interface ConfirmSummary {
  matched: number;
  mismatched: number;
  /** Phần hãng trả thiếu sẽ còn mở. */
  short: number;
  /** Phí hãng tách ra khi chốt (dòng khớp, lệch, chỉ phí). */
  fee: number;
}

/** Số liệu cho câu của thanh Chốt (tổng của mọi dòng, máy chủ tính). */
export function confirmSummary(totals: readonly CodMatchTotal[]): ConfirmSummary {
  const t = (s: CodMatchStatus) => totalOf(totals, s);
  const posted: CodMatchStatus[] = ["matched", "amount_mismatch", "fee_only"];
  return {
    matched: t("matched").count,
    mismatched: t("amount_mismatch").count,
    short: t("amount_mismatch").shortAmount ?? 0,
    fee: posted.reduce((sum, s) => sum + (t(s).feeAmount ?? 0), 0),
  };
}

/** Cột "Loại" của Lệch đối soát. */
export function discrepancyKind(d: CodDiscrepancy): string {
  if (d.kind === "remitted_then_returned") return "Hãng chuyển tiền rồi báo hoàn";
  if (d.kind === "open_remainder") return "Phần thiếu còn mở";
  switch (d.matchStatus) {
    case "external":
      return "Ngoài hệ thống · không tính vào số liệu";
    case "duplicate":
      return "Dòng trùng trong bảng kê";
    default:
      return (d.diff ?? 0) > 0 ? "Hãng chuyển dư" : "Phần thiếu còn mở";
  }
}

/** Dòng phụ "Viettel Post · 1234567890001". */
export function carrierLine(carrierKey: string | undefined, tracking: string | undefined): string {
  return [carrierKey === undefined ? "" : carrierName(carrierKey), tracking ?? ""].filter((x) => x !== "").join(" · ");
}
