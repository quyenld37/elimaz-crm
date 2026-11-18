// Luật của màn Phiếu kho (kế hoạch M1 Task 328; mockup docs/mockups/m1/kho-phieu.html). Luật thật (đủ hàng, quyền
// theo kho, phiếu nháp mới sửa được) ở API chứng từ kho (Task 29); ở đây chỉ soạn dòng, kiểm dạng số và dựng chữ.
import type { StockDocumentKind, StockDocumentLine, StockDocumentStatus } from "../../api";
import { moneyInput, parseMoney } from "../products/editRules";

export const KINDS: readonly StockDocumentKind[] = ["import", "export", "damage", "damage_export", "adjust"];

/** Tên loại ở ô chọn và cột "Loại" (mockup). */
export const KIND_LABEL: Readonly<Record<StockDocumentKind, string>> = {
  import: "Nhập hàng",
  export: "Xuất khác",
  damage: "Chuyển hàng lỗi",
  damage_export: "Xuất hàng lỗi",
  adjust: "Điều chỉnh",
};

/** Đầu đề của phiếu ("Phiếu nhập PN-000124"). */
export const KIND_TITLE: Readonly<Record<StockDocumentKind, string>> = {
  import: "Phiếu nhập",
  export: "Phiếu xuất",
  damage: "Phiếu chuyển hàng lỗi",
  damage_export: "Phiếu xuất hàng lỗi",
  adjust: "Phiếu điều chỉnh",
};

export const STATUSES: readonly StockDocumentStatus[] = ["draft", "posted", "cancelled"];

export const STATUS_LABEL: Readonly<Record<StockDocumentStatus, string>> = {
  draft: "Nháp",
  posted: "Đã ghi sổ",
  cancelled: "Đã hủy",
};

export const STATUS_CHIP: Readonly<Record<StockDocumentStatus, string>> = {
  draft: "chip",
  posted: "chip chip-ok",
  cancelled: "chip chip-line",
};

/** Giới hạn số lượng một dòng (openapi.yaml: StockDocumentLine.quantity). */
export const MAX_QTY = 1_000_000;
/** Số dòng tối đa của một phiếu (ReplaceStockDocumentLinesRequest.lines maxItems). */
export const MAX_LINES = 500;

/** Chỉ phiếu nhập có nhà cung cấp (máy chủ trả supplier_not_for_kind với loại khác). */
export const hasSupplier = (kind: StockDocumentKind): boolean => kind === "import";

/** Một dòng đang soạn: chữ trong ô số lượng và ô giá nhập. */
export interface EditLine {
  variantId: string;
  qty: string;
  cost: string;
}

const QTY_RE = /^-?\d+$/;
const intOf = (s: string): number | null => (QTY_RE.test(s.trim()) ? Number(s.trim()) : null);

/** Quét (hay chọn) một mã: đã có dòng thì cộng 1, chưa có thì thêm dòng 1 cái ở cuối. */
export function addScanned(lines: readonly EditLine[], variantId: string): EditLine[] {
  if (!lines.some((l) => l.variantId === variantId)) return [...lines, { variantId, qty: "1", cost: "" }];
  return lines.map((l) => {
    if (l.variantId !== variantId) return l;
    const n = intOf(l.qty);
    return { ...l, qty: n === null ? "1" : String(n + 1) };
  });
}

/** Nút −/+ của ô số lượng: phiếu thường không dưới 1; phiếu điều chỉnh bỏ qua 0 (1 − 1 → −1). */
export function stepQty(
  lines: readonly EditLine[],
  variantId: string,
  delta: 1 | -1,
  kind: StockDocumentKind,
): EditLine[] {
  return lines.map((l) => {
    if (l.variantId !== variantId) return l;
    let n = (intOf(l.qty) ?? 0) + delta;
    if (kind === "adjust") {
      if (n === 0) n += delta;
    } else if (n < 1) n = 1;
    return { ...l, qty: String(Math.max(-MAX_QTY, Math.min(MAX_QTY, n))) };
  });
}

/** Lỗi dạng số của từng dòng (khóa variantId); rỗng là hợp lệ. */
export function checkLines(lines: readonly EditLine[], kind: StockDocumentKind): Record<string, string> {
  const errs: Record<string, string> = {};
  for (const l of lines) {
    const n = intOf(l.qty);
    if (n === null) errs[l.variantId] = "Số lượng phải là số nguyên";
    else if (Math.abs(n) > MAX_QTY) errs[l.variantId] = "Số lượng tối đa 1.000.000";
    else if (kind === "adjust" && n === 0) errs[l.variantId] = "Số lượng điều chỉnh phải khác 0";
    else if (kind !== "adjust" && n < 1) errs[l.variantId] = "Số lượng phải từ 1 trở lên";
    else if (kind === "import" && l.cost.trim() !== "" && parseMoney(l.cost) === null)
      errs[l.variantId] = "Giá nhập không đúng dạng";
  }
  return errs;
}

/** Dòng gửi API (gọi sau checkLines rỗng). Giá nhập chỉ gửi ở phiếu nhập, ô trống thì không gửi. */
export function toApiLines(lines: readonly EditLine[], kind: StockDocumentKind): StockDocumentLine[] {
  return lines.map((l) => {
    const cost = kind === "import" && l.cost.trim() !== "" ? parseMoney(l.cost) : null;
    return { variantId: l.variantId, quantity: intOf(l.qty) ?? 0, ...(cost !== null && { unitCost: cost }) };
  });
}

/** Dòng của API thành dòng đang soạn. */
export function fromApiLines(lines: readonly StockDocumentLine[]): EditLine[] {
  return lines.map((l) => ({ variantId: l.variantId, qty: String(l.quantity), cost: moneyInput(l.unitCost) }));
}

/** Hai bản dòng cùng nội dung (so theo số, không theo chữ: "180.000" = "180000"). */
export function sameLines(a: readonly EditLine[], b: readonly EditLine[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((l, i) => {
    const r = b[i];
    if (r?.variantId !== l.variantId) return false;
    const qa = intOf(l.qty);
    const qb = intOf(r.qty);
    const sameQty = qa === null || qb === null ? l.qty.trim() === r.qty.trim() : qa === qb;
    const ca = l.cost.trim() === "" ? null : parseMoney(l.cost);
    const cb = r.cost.trim() === "" ? null : parseMoney(r.cost);
    const sameCost = ca === null || cb === null ? l.cost.trim() === r.cost.trim() : ca === cb;
    return sameQty && sameCost;
  });
}

/** Thành tiền một dòng (số lượng × giá nhập); null khi thiếu giá hay số sai dạng. */
export function lineCost(l: EditLine): number | null {
  const n = intOf(l.qty);
  const c = l.cost.trim() === "" ? null : parseMoney(l.cost);
  return n === null || c === null ? null : n * c;
}

/** Tổng số mã, số cái (cộng đại số, dòng sai dạng tính 0) và tổng giá nhập (null khi không dòng nào có giá). */
export function totals(lines: readonly EditLine[]): { skus: number; units: number; cost: number | null } {
  let units = 0;
  let cost: number | null = null;
  for (const l of lines) {
    units += intOf(l.qty) ?? 0;
    const c = lineCost(l);
    if (c !== null) cost = (cost ?? 0) + c;
  }
  return { skus: lines.length, units, cost };
}

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

/** Số kiểu "1.000", số âm có dấu trừ thật (U+2212) như mockup. */
export function formatUnits(n: number): string {
  return n < 0 ? `−${NUM.format(-n)}` : NUM.format(n);
}

const TIME = new Intl.DateTimeFormat("vi-VN", {
  timeZone: "Asia/Ho_Chi_Minh",
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  hourCycle: "h23",
});

/** Giờ Việt Nam kiểu "15:10 18/11" (mockup). */
export function formatTime(iso: string): string {
  const parts = Object.fromEntries(TIME.formatToParts(new Date(iso)).map((p) => [p.type, p.value]));
  return `${parts.hour ?? ""}:${parts.minute ?? ""} ${parts.day ?? ""}/${parts.month ?? ""}`;
}
