// Luật thuần của thẻ "Hãng trên website" ở cuối màn Kết nối vận chuyển (kế hoạch M5 Task 208, M5-25; mockup
// docs/mockups/m5/web-carriers.html, HV09–HV12). Một nơi lưu: cờ showOnWeb, webSort trên tài khoản hãng của M3 (QĐ28);
// hãng chọn sẵn và phí dự phòng là cài đặt web của Task 198. Máy chủ vẫn kiểm: ẩn hãng cuối → 422 last_web_carrier,
// chọn sẵn hãng không hiện → 422 carrier_not_on_web, phí ngoài 0–500.000 → 422 setting_out_of_range.
import type { CarrierAccount, SaveWebCarrierSettingsRequest, WebCarrierSettings } from "../../api";
import { moneyInput, parseMoney } from "../products/editRules";

/** Trần của phí dự phòng (CHECK fallback_fee BETWEEN 0 AND 500000 của Task 198). */
export const MAX_FALLBACK_FEE = 500_000;
/** Trần web_sort của máy chủ (fulfillment maxWebSort). */
export const MAX_WEB_SORT = 1000;

const byId = (a: CarrierAccount, b: CarrierAccount) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

/** Tài khoản trong thẻ: đang hiện (theo webSort rồi id, đúng thứ tự khách thấy) và chưa hiện; bỏ tài khoản đã thu hồi. */
export function webRows(accounts: readonly CarrierAccount[]): { shown: CarrierAccount[]; hidden: CarrierAccount[] } {
  const live = accounts.filter((a) => a.status !== "revoked");
  const shown = live.filter((a) => a.showOnWeb).sort((a, b) => a.webSort - b.webSort || byId(a, b));
  return { shown, hidden: live.filter((a) => !a.showOnWeb) };
}

/** Lý do không bật hiện trên website được (chỉ tài khoản đang hoạt động mới báo phí, gửi đơn); null = bật được. */
export function blockReason(a: Pick<CarrierAccount, "status">): string | null {
  switch (a.status) {
    case "active":
      return null;
    case "paused":
    case "disabled":
      return "Tạm ngừng: bật lại tài khoản ở Kết nối vận chuyển rồi mới hiện được trên website.";
    case "auth_failed":
      return "Cần cấp quyền lại: hãng từ chối khóa, chưa hiện được trên website.";
    case "draft":
    case "checking":
    case "revoked":
      return "Chưa hoạt động: kiểm tra tài khoản ở Kết nối vận chuyển rồi mới hiện được trên website.";
  }
}

/** Tài khoản a là hãng đang hoạt động cuối cùng còn hiện: ẩn nó thì khách không đặt được (luật Task 450). */
export function isLastActive(shown: readonly CarrierAccount[], a: CarrierAccount): boolean {
  return a.status === "active" && !shown.some((x) => x.id !== a.id && x.status === "active");
}

/** Thứ tự cho tài khoản vừa bật: đứng cuối danh sách đang hiện. */
export function nextWebSort(shown: readonly CarrierAccount[]): number {
  return Math.min(MAX_WEB_SORT, Math.max(0, ...shown.map((a) => a.webSort)) + 1);
}

/**
 * Thứ tự mới của cả danh sách đang hiện sau khi đổi chỗ dòng index với dòng kề (dir −1 lên, +1 xuống): mã tài khoản
 * theo thứ tự khách sẽ thấy, gửi một lần bằng reorderWebCarriers (Task 488; máy chủ đánh số lại 1, 2, 3…). Ra ngoài
 * danh sách → [].
 */
export function moveOrder(shown: readonly CarrierAccount[], index: number, dir: -1 | 1): string[] {
  const j = index + dir;
  if (index < 0 || index >= shown.length || j < 0 || j >= shown.length) return [];
  const next = shown.map((a) => a.id);
  const [moved] = next.splice(index, 1);
  if (moved === undefined) return [];
  next.splice(j, 0, moved);
  return next;
}

/** Số vận đơn 30 ngày theo mã tài khoản, cả hãng đang hiện và chưa hiện (Task 488). */
export function shipmentCounts(s: WebCarrierSettings | undefined): Map<string, number> {
  return new Map([...(s?.carriers ?? []), ...(s?.hiddenCarriers ?? [])].map((c) => [c.accountId, c.shipments30d]));
}

/**
 * Câu "ai lưu lúc nào" của hộp 409 (mockup trạng thái C): chỉ khi lỗi đến từ cài đặt hay thứ tự (stamp của cài đặt
 * mới có nghĩa) và bản đã tải lại mới hơn bản gửi đi; không có tên hay giờ → null.
 */
export function savedBy(
  s: WebCarrierSettings | undefined,
  sentVersion: number,
  time: (iso: string) => string,
): string | null {
  if (s === undefined || s.version <= sentVersion || s.updatedAt === undefined) return null;
  const at = time(s.updatedAt);
  return s.updatedByName === undefined ? `lần lưu cuối lúc ${at}` : `${s.updatedByName}, ${at}`;
}

// ---------------------------------------------------------------- Hãng chọn sẵn, phí dự phòng

export interface WebForm {
  /** Mã tài khoản chọn sẵn; "" = không chọn sẵn (website chọn hãng rẻ nhất). */
  preferred: string;
  useFallback: boolean;
  /** Ô phí dự phòng như người dùng gõ ("30.000"). */
  fee: string;
}

export function formOf(s: WebCarrierSettings): WebForm {
  return {
    preferred: s.preferredAccountId ?? "",
    useFallback: s.fallbackFee !== undefined,
    fee: moneyInput(s.fallbackFee),
  };
}

export function sameForm(a: WebForm, b: WebForm): boolean {
  return (
    a.preferred === b.preferred &&
    a.useFallback === b.useFallback &&
    (!a.useFallback || parseMoney(a.fee) === parseMoney(b.fee))
  );
}

export interface WebFormErrors {
  preferred?: string;
  fee?: string;
}

/** Kiểm form; choices = mã các tài khoản đang hoạt động và đang hiện trên website (chọn sẵn được). */
export function validateForm(f: WebForm, choices: readonly string[]): WebFormErrors {
  const e: WebFormErrors = {};
  if (f.preferred !== "" && !choices.includes(f.preferred)) {
    e.preferred = "Hãng chọn sẵn phải đang hiện trên website. Chọn hãng khác hoặc Không chọn sẵn.";
  }
  if (f.useFallback) {
    const v = parseMoney(f.fee);
    if (v === null) e.fee = "Nhập số tiền, ví dụ 30.000";
    else if (v > MAX_FALLBACK_FEE) e.fee = "Phí dự phòng từ 0 đến 500.000 ₫";
  }
  return e;
}

export function toRequest(f: WebForm, version: number): SaveWebCarrierSettingsRequest {
  const fee = f.useFallback ? parseMoney(f.fee) : null;
  return {
    version,
    ...(f.preferred === "" ? {} : { preferredAccountId: f.preferred }),
    ...(fee === null ? {} : { fallbackFee: fee }),
  };
}

/**
 * Hai việc shop phải xong trước khi website dùng báo phí của CRM (kế hoạch Task 198, M5-25): ít nhất một hãng đang
 * hoạt động hiện trên website, và phí dự phòng (để khách vẫn đặt được khi không hãng nào báo được phí).
 */
export function readiness(shown: readonly CarrierAccount[], s: WebCarrierSettings | undefined) {
  return {
    carrier: shown.some((a) => a.status === "active"),
    fallback: s?.fallbackFee !== undefined,
  };
}
