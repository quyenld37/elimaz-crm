// Quy tắc thuần của màn Kho: Đóng gói, in nhãn theo lô, phiếu nhặt (mockup docs/mockups/m3/kho.html; HV23, HV24, HV25).
import type { LabelBatchSkipped, OrderListItem, PackingResult } from "../../api";
import { shortTime } from "../../app/time";

export type PackingStatus = NonNullable<OrderListItem["packingStatus"]>;
export type PackTo = "picking" | "packed";

export const PACK_TEXT: Readonly<Record<PackingStatus, string>> = {
  none: "Chưa đóng gói",
  picking: "Đang đóng gói",
  packed: "Đã đóng gói",
};
export const PACK_CHIP: Readonly<Record<PackingStatus, string>> = {
  none: "chip",
  picking: "chip chip-dark",
  packed: "chip chip-ok",
};

/** Mã hãng tự giao (fulfillment.SelfCarrierKey). */
export const SELF_CARRIER = "self";

/** Tên hiển thị của hãng theo mã (adapter trong internal/adapters/carrier); mã lạ hiện nguyên mã. */
const CARRIER_NAME: Readonly<Record<string, string>> = {
  vtp: "Viettel Post",
  best: "Best Express",
  spx: "SPX",
  ghn: "GHN",
  [SELF_CARRIER]: "Shop tự giao",
};
export const carrierName = (key: string) => CARRIER_NAME[key] ?? key;

/** Bộ lọc của thanh trên bảng (mọi bộ lọc đều trên vận đơn "Chờ thu gom"). */
export type PackFilter = "all" | "unprinted" | "none" | "picking" | "packed";
export const PACK_FILTERS: readonly PackFilter[] = ["all", "unprinted", "none", "picking", "packed"];
export const PACK_FILTER_TEXT: Readonly<Record<PackFilter, string>> = {
  all: "Chờ thu gom",
  unprinted: "Chưa in nhãn",
  none: "Chưa đóng gói",
  picking: "Đang đóng gói",
  packed: "Đã đóng gói",
};
export function packFilterQuery(f: PackFilter): { labelPrinted?: boolean; packingStatus?: PackingStatus } {
  switch (f) {
    case "all":
      return {};
    case "unprinted":
      return { labelPrinted: false };
    default:
      return { packingStatus: f };
  }
}

/** Vận đơn không có nhãn của hãng: tự giao hay tài khoản nhập tay (CRM không lấy được nhãn). */
export const noCarrierLabel = (o: OrderListItem) =>
  o.shipment !== undefined && (o.shipment.carrierKey === SELF_CARRIER || o.shipment.mode === "manual");

/** Cột "Nhãn": Không có nhãn hãng / Đã in <giờ> / Chưa in. */
export function labelCell(o: OrderListItem): { text: string; muted: boolean } {
  if (noCarrierLabel(o)) return { text: "Không có nhãn hãng", muted: true };
  const at = o.shipment?.labelPrintedAt;
  return at === undefined ? { text: "Chưa in", muted: true } : { text: `Đã in ${shortTime(at)}`, muted: false };
}

/** "Đã chọn 5 vận đơn · Viettel Post 3, Best Express 1, tự giao 1" (hãng theo thứ tự xuất hiện). */
export function selectionText(rows: readonly OrderListItem[]): string {
  const counts = new Map<string, number>();
  for (const r of rows) {
    const key = r.shipment?.carrierKey ?? "";
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const parts = [...counts].map(([k, n]) => `${k === SELF_CARRIER ? "tự giao" : carrierName(k)} ${String(n)}`);
  return `Đã chọn ${String(rows.length)} vận đơn · ${parts.join(", ")}`;
}

/** Câu thông báo sau khi đánh dấu đóng gói (HV24: chỉ đi tới; dòng lùi bước không đổi và được kể ra). */
export function packingMessage(
  to: PackTo,
  results: readonly PackingResult[],
  byId: ReadonlyMap<string, OrderListItem>,
): string {
  const code = (id: string) => byId.get(id)?.code ?? id;
  const updated = results.filter((r) => r.result === "updated");
  const further = results.filter((r) => r.result === "already_further");
  const denied = results.filter((r) => r.result === "not_allowed");
  const parts = [`Đã đánh dấu ${String(updated.length)} đơn "${PACK_TEXT[to]}".`];
  if (further.length > 0) {
    const list = further
      .map((r) => {
        const st = byId.get(r.orderId)?.packingStatus;
        return st === undefined ? code(r.orderId) : `${code(r.orderId)} · ${PACK_TEXT[st]}`;
      })
      .join(", ");
    parts.push(`${String(further.length)} đơn đã ở bước sau (${list}), không đổi.`);
  }
  if (denied.length > 0) {
    parts.push(
      `${String(denied.length)} đơn không đổi được (${denied.map((r) => code(r.orderId)).join(", ")}): không còn chờ thu gom hoặc ngoài kho bạn được giao.`,
    );
  }
  return parts.join(" ");
}

/** Các câu của vận đơn bị bỏ qua khi in nhãn: gom theo lý do; codeOf trả mã đơn của vận đơn. */
export function skippedMessages(
  skipped: readonly LabelBatchSkipped[],
  carrierOf: (shipmentId: string) => string,
  codeOf: (shipmentId: string) => string,
): string[] {
  const groups = new Map<string, string[]>();
  for (const s of skipped) {
    const why =
      s.reason === "no_carrier_label"
        ? carrierOf(s.shipmentId) === SELF_CARRIER
          ? "tự giao không có nhãn hãng"
          : "nhập tay không có nhãn hãng"
        : s.reason === "no_tracking_code"
          ? "chưa có mã của hãng, chưa in được"
          : s.reason === "cancelled"
            ? "đã hủy, không in"
            : "không in được";
    groups.set(why, [...(groups.get(why) ?? []), codeOf(s.shipmentId)]);
  }
  return [...groups].map(([why, codes]) => `${String(codes.length)} vận đơn ${why} (${codes.join(", ")}).`);
}

/** Đường dẫn tệp nhãn ký sẵn: chỉ nhận http(s) (không mở chuỗi lạ từ API như javascript:). */
export function safeFileUrl(u: string | undefined): string | null {
  if (u === undefined) return null;
  try {
    const p = new URL(u, window.location.origin);
    return p.protocol === "https:" || p.protocol === "http:" ? p.href : null;
  } catch {
    return null;
  }
}

/** Tối đa số dòng chọn một lần (khớp maxItems của createLabelBatch, getPickingList, setPackingStatus). */
export const MAX_SELECT = 200;
