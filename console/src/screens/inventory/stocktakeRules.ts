// Luật hiện của màn Kiểm kho (kế hoạch M1 Task 329; mockup docs/mockups/m1/kho-kiem-kho.html). Số liệu do máy chủ tính
// (Task 35): trên kệ theo sổ = Tồn − Đang giao lúc ghi số đếm; chênh = đếm − trên kệ; có thể bán sau = có thể bán +
// chênh. Màn chỉ phân loại và viết câu.
import type { Grant, StocktakeLine, VariantSearchHit } from "../../api";

/** Số đếm lớn nhất API nhận (RecordStocktakeCountRequest.counted). */
export const MAX_COUNT = 1_000_000;

/** "+1", "−3" (dấu trừ thật U+2212), "0". */
export function signedDelta(n: number): string {
  if (n > 0) return `+${String(n)}`;
  if (n < 0) return `−${String(-n)}`;
  return "0";
}

/** Có quyền ở kho: phạm vi toàn shop hoặc đúng kho (luật của API, docs/quyen.md "theo đích: warehouse"). */
export function canAt(grants: readonly Grant[], permission: string, warehouseId: string): boolean {
  return grants.some(
    (g) =>
      g.permission === permission &&
      (g.scope.type === "all" || (g.scope.type === "warehouse" && g.scope.id === warehouseId)),
  );
}

/** Kết quả tìm khớp đúng mã đã quét: barcode đúng, hoặc SKU không phân biệt hoa thường. Tiền tố, tên không tính. */
export function matchHit(code: string, hits: readonly VariantSearchHit[]): VariantSearchHit | null {
  const c = code.trim();
  const up = c.toUpperCase();
  return hits.find((h) => h.barcode === c) ?? hits.find((h) => h.sku.toUpperCase() === up) ?? null;
}

/** Số đếm gõ tay: số nguyên 0…1.000.000; khác → null. */
export function parseCount(text: string): number | null {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return n <= MAX_COUNT ? n : null;
}

/** match: không chỉnh; over: thừa; under: thiếu; short_held: thiếu và sau áp dụng có thể bán âm (KH07). */
export type LineKind = "match" | "over" | "under" | "short_held";

export function lineKind(l: StocktakeLine): LineKind {
  if (l.delta === 0) return "match";
  if (l.delta > 0) return "over";
  return l.availableAfter < 0 ? "short_held" : "under";
}

export interface Preview {
  /** Dòng hiện riêng: lệch sổ, hoặc khớp nhưng có hàng đang giao (KH05: ghi rõ "Tồn 10, đang giao 3"). */
  rows: StocktakeLine[];
  /** Mã khớp sổ không có hàng đang giao: gộp một dòng "N mã khớp sổ". */
  matched: StocktakeLine[];
  /** Mã sẽ được chỉnh Tồn (chênh khác 0). */
  changed: StocktakeLine[];
  /** Mã sẽ thiếu so với phần giữ. */
  shortHeld: StocktakeLine[];
}

export function preview(lines: readonly StocktakeLine[]): Preview {
  return {
    rows: lines.filter((l) => l.delta !== 0 || l.inTransit > 0),
    matched: lines.filter((l) => l.delta === 0 && l.inTransit <= 0),
    changed: lines.filter((l) => l.delta !== 0),
    shortHeld: lines.filter((l) => lineKind(l) === "short_held"),
  };
}

const MAX_LISTED = 5;

/** Câu của hộp xác nhận áp dụng (trạng thái D). */
export function confirmText(lines: readonly StocktakeLine[]): string {
  const p = preview(lines);
  const matched = lines.length - p.changed.length;
  const parts: string[] = [];
  if (p.changed.length === 0) {
    parts.push("Không mã nào lệch sổ, Tồn không đổi.");
  } else {
    const ds = p.changed.map((l) => signedDelta(l.delta));
    let list: string;
    if (ds.length > MAX_LISTED) list = `${ds.slice(0, MAX_LISTED).join(", ")}, …`;
    else if (ds.length === 1) list = ds[0] ?? "";
    else list = `${ds.slice(0, -1).join(", ")} và ${ds.at(-1) ?? ""}`;
    parts.push(`Sửa Tồn của ${String(p.changed.length)} mã: ${list}${list.endsWith("…") ? "" : "."}`);
  }
  if (matched > 0) parts.push(`${String(matched)} mã khớp không đổi.`);
  if (p.shortHeld.length > 0) parts.push(`${String(p.shortHeld.length)} mã sẽ thiếu so với phần giữ.`);
  return parts.join(" ");
}
