// Luật thuần của màn Kết nối vận chuyển và Cài đặt vận chuyển (kế hoạch M3 Task 132, M3-41; mockup
// docs/mockups/m3/ket-noi-van-chuyen.html). Nhãn trạng thái, câu lần kiểm, ô khóa theo hãng, đổi ô cài đặt ↔ API.
// Máy chủ vẫn là nơi kiểm (fulfillment.Settings.Validate, 422 setting_out_of_range nêu tên trường).
import type { CarrierAccount, FulfillmentSettings, UpdateFulfillmentSettingsRequest } from "../../api";
import { shortTime } from "../../app/time";
import { percentInput, percentOrZero } from "../ma-giam-gia/couponRules";
import { SELF_CARRIER, carrierName } from "../warehouse/warehouseRules";

export { SELF_CARRIER, carrierName };

/** Chủ đề báo hiệu khi tài khoản hãng đổi (máy chủ: fulfillment accountSignalTopic). */
export const CARRIER_ACCOUNT_TOPIC = "fulfillment.carrier_account";
/** Chủ đề riêng của cài đặt vận chuyển (không có báo hiệu: đọc lại sau khi lưu). */
export const FULFILLMENT_SETTINGS_KEY = "fulfillment.settings";

// ---------------------------------------------------------------- Trạng thái tài khoản (sáu trạng thái của mockup A)

/** Nhãn và chip của trạng thái; "disabled" là giá trị cũ của M1 (migration đổi thành paused). */
export function statusChip(a: Pick<CarrierAccount, "status" | "hasCredentials" | "mode">): {
  text: string;
  cls: string;
} {
  switch (a.status) {
    case "draft":
      return { text: a.hasCredentials ? "Nháp" : "Nháp · chưa có khóa", cls: "chip chip-line" };
    case "checking":
      return { text: "Đang kiểm tra…", cls: "chip" };
    case "active":
      return { text: "Đang hoạt động", cls: "chip chip-ok" };
    case "auth_failed":
      return { text: "Cần cấp quyền lại", cls: "chip chip-err" };
    case "paused":
    case "disabled":
      return { text: "Tạm ngừng", cls: "chip" };
    case "revoked":
      return { text: "Đã thu hồi", cls: "chip chip-dark" };
  }
}

/** Đang chờ kết quả kiểm: máy chủ đặt checking (sau Thay khóa) hoặc màn vừa gửi lệnh kiểm mà lần kiểm chưa đổi. */
export function isChecking(a: CarrierAccount, pendingSince: string | undefined): boolean {
  return a.status === "checking" || (pendingSince !== undefined && (a.lastCheckAt ?? "") === pendingSince);
}

/** Câu kết quả của lần kiểm (mã của máy chủ, accounts_check.go; không bao giờ thông điệp thô của hãng). */
export function checkResultText(a: Pick<CarrierAccount, "status" | "lastCheckResult">): string {
  const r = a.lastCheckResult ?? "";
  if (r === "ok") return "Đạt";
  if (r === "check_not_supported") return "Hãng chưa nối API trong CRM";
  if (a.status === "auth_failed") return "Hãng từ chối khóa";
  return "Không đạt";
}

/** Ô "Lần kiểm gần nhất": "09:12 · 21/11 · Đạt"; nhập tay không kiểm; err khi lần kiểm không đạt. */
export function lastCheckCell(a: CarrierAccount, checking: boolean): { text: string; err: boolean; muted: boolean } {
  if (a.mode === "manual") return { text: "Không kiểm (nhập tay)", err: false, muted: true };
  if (checking) return { text: "Đang hỏi hãng", err: false, muted: true };
  if (a.lastCheckAt === undefined) return { text: "Chưa kiểm", err: false, muted: true };
  const at = shortTime(a.lastCheckAt).replace(" ", " · ");
  return { text: `${at} · ${checkResultText(a)}`, err: a.lastCheckResult !== "ok", muted: false };
}

/** Dòng phụ dưới tên: hãng · cách nối (· môi trường cho tài khoản API). */
export function accountKind(a: Pick<CarrierAccount, "carrierKey" | "mode">): string {
  if (a.carrierKey === SELF_CARRIER) return "Tự giao · Nhập tay";
  if (a.mode === "manual") return `${carrierName(a.carrierKey)} · Nhập mã tay · không gọi API`;
  return `${carrierName(a.carrierKey)} · API`;
}

export const ENV_TEXT = { production: "Thật", sandbox: "Thử (sandbox)" } as const;

export const webText = (a: Pick<CarrierAccount, "showOnWeb" | "webSort">) =>
  a.showOnWeb ? `Có · thứ tự ${String(a.webSort)}` : "Không";

// ---------------------------------------------------------------- Thêm tài khoản (mockup B)

/** Lựa chọn hãng của hộp Thêm tài khoản; "other" là hãng không có trong CRM (chỉ nhập mã tay, khóa hãng nhập riêng). */
export const CARRIER_CHOICES = [
  { key: "vtp", label: "Viettel Post" },
  { key: "best", label: "Best Express" },
  { key: "spx", label: "SPX" },
  { key: SELF_CARRIER, label: "Shop tự giao" },
] as const;

export interface CredentialField {
  key: string;
  label: string;
}

/**
 * Ô khóa API theo hãng. Hợp đồng để CarrierCredentials là map tự do (Task 131 để lại): Viettel Post dùng username,
 * password (internal/adapters/carrier/vtp/auth.go); hãng khác chưa có adapter thật nên một ô token (hãng giả ở dev).
 */
export function credentialFields(carrierKey: string): readonly CredentialField[] {
  if (carrierKey === "vtp") {
    return [
      { key: "username", label: "Tên đăng nhập API" },
      { key: "password", label: "Mật khẩu API" },
    ];
  }
  return [{ key: "token", label: "Khóa API (token)" }];
}

/** Ô khóa → bộ khóa gửi đi; bỏ ô trống. Rỗng hết → null (không gửi credentials). */
export function credentialsBody(values: Readonly<Record<string, string>>): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(values)) if (v !== "") out[k] = v;
  return Object.keys(out).length === 0 ? null : out;
}

const MAX_WEB_SORT = 1000;

/** Thứ tự trên web: số nguyên 0–1000 (khớp CreateCarrierAccountRequest.webSort); sai → null. */
export function parseWebSort(s: string): number | null {
  const m = /^\s*(\d{1,4})\s*$/.exec(s);
  const n = m === null ? Number.NaN : Number(m[1]);
  return n <= MAX_WEB_SORT ? n : null;
}

// ---------------------------------------------------------------- Cài đặt vận chuyển (mockup F)

type Unit = "giờ" | "ngày" | "đơn" | "%" | "gam" | "phút";

/** Đường tới một số của FulfillmentSettings (trừ pollMinutes: chu kỳ tra không hiện, HV06). */
export type SettingPath =
  | `stuckHours.${string}`
  | "callbackDeadlineHours"
  | "codOverdueDays"
  | "agedDays.held"
  | "agedDays.inTransit"
  | "returnRisk.watchMinReturned"
  | "returnRisk.highMinReturned"
  | "returnRisk.highRateBp"
  | "defaultWeightGramsPerItem"
  | "quoteValidMinutes";

export interface SettingRow {
  path: SettingPath;
  label: string;
  unit: Unit;
}

/** Năm trạng thái có ngưỡng kẹt, theo thứ tự hiện (fulfillment.StuckStatuses). */
export const STUCK_ROWS: readonly SettingRow[] = [
  { path: "stuckHours.awaiting_pickup", label: "Chờ thu gom", unit: "giờ" },
  { path: "stuckHours.shipping", label: "Đang chuyển", unit: "giờ" },
  { path: "stuckHours.delivery_failed", label: "Thất bại", unit: "giờ" },
  { path: "stuckHours.returning", label: "Đang chuyển hoàn", unit: "giờ" },
  { path: "stuckHours.cancel_requested", label: "Đang yêu cầu hủy", unit: "giờ" },
];
export const DEADLINE_ROWS: readonly SettingRow[] = [
  { path: "callbackDeadlineHours", label: "Hạn gọi lại sau khi giao thất bại", unit: "giờ" },
  { path: "codOverdueDays", label: "Quá hạn chưa có tiền sau khi giao", unit: "ngày" },
  { path: "agedDays.held", label: "Giữ hàng quá hạn (chưa gửi)", unit: "ngày" },
  { path: "agedDays.inTransit", label: "Đang giao quá hạn", unit: "ngày" },
];
export const RISK_ROWS: readonly SettingRow[] = [
  { path: "returnRisk.watchMinReturned", label: '"Có hoàn" khi số đơn hoàn từ', unit: "đơn" },
  { path: "returnRisk.highMinReturned", label: '"Hay hoàn" khi số đơn hoàn từ', unit: "đơn" },
  { path: "returnRisk.highRateBp", label: "và tỷ lệ hoàn từ", unit: "%" },
];
export const OTHER_ROWS: readonly SettingRow[] = [
  { path: "defaultWeightGramsPerItem", label: "Cân nặng mặc định mỗi món", unit: "gam" },
  { path: "quoteValidMinutes", label: "Báo phí còn hiệu lực", unit: "phút" },
];
export const ALL_ROWS: readonly SettingRow[] = [...STUCK_ROWS, ...DEADLINE_ROWS, ...RISK_ROWS, ...OTHER_ROWS];

export type SettingInputs = Readonly<Record<string, string>>;

const MAX_INT = 1_000_000;
export const TEXT_POSITIVE = "Nhập số nguyên lớn hơn 0";
export const TEXT_RATE = "Nhập từ 1% đến 100%";

function numberAt(s: FulfillmentSettings, path: SettingPath): number {
  const [head, tail] = path.split(".");
  switch (head) {
    case "stuckHours":
      return s.stuckHours[tail ?? ""] ?? 0;
    case "agedDays":
      return tail === "held" ? s.agedDays.held : s.agedDays.inTransit;
    case "returnRisk":
      if (tail === "watchMinReturned") return s.returnRisk.watchMinReturned;
      return tail === "highMinReturned" ? s.returnRisk.highMinReturned : s.returnRisk.highRateBp;
    case "callbackDeadlineHours":
      return s.callbackDeadlineHours;
    case "codOverdueDays":
      return s.codOverdueDays;
    case "defaultWeightGramsPerItem":
      return s.defaultWeightGramsPerItem;
    default:
      return s.quoteValidMinutes;
  }
}

/** Cài đặt đang lưu → chữ của các ô (tỷ lệ: phần vạn → %). */
export function settingInputs(s: FulfillmentSettings): SettingInputs {
  const out: Record<string, string> = {};
  for (const r of ALL_ROWS) {
    const v = numberAt(s, r.path);
    out[r.path] = r.unit === "%" ? percentInput(v) : String(v);
  }
  return out;
}

/** Mặc định của máy chủ (internal/modules/fulfillment/settings.go DefaultSettings; nút "Về mặc định" chỉ điền ô). */
export const DEFAULT_SETTINGS: Omit<FulfillmentSettings, "version" | "pollMinutes"> = {
  stuckHours: { awaiting_pickup: 48, shipping: 72, delivery_failed: 24, returning: 168, cancel_requested: 24 },
  callbackDeadlineHours: 24,
  defaultWeightGramsPerItem: 300,
  quoteValidMinutes: 30,
  returnRisk: { watchMinReturned: 1, highMinReturned: 2, highRateBp: 3000 },
  codOverdueDays: 14,
  agedDays: { held: 20, inTransit: 180 },
};

/** Một ô → số của API; sai → câu lỗi tại ô. "30%" và "30" như nhau; tỷ lệ 1–100%. */
export function readSetting(unit: Unit, raw: string): number | string {
  if (unit === "%") {
    const bp = percentOrZero(raw.replace("%", ""));
    return bp === null || bp < 1 || bp > 10_000 ? TEXT_RATE : bp;
  }
  const t = raw.trim();
  if (!/^\d{1,7}$/.test(t)) return TEXT_POSITIVE;
  const n = Number(t);
  return n < 1 || n > MAX_INT ? TEXT_POSITIVE : n;
}

/** Các ô → thân PUT (giữ nguyên pollMinutes đang lưu, HV06); ô sai → body null kèm lỗi theo đường. */
export function readSettings(
  s: FulfillmentSettings,
  inputs: SettingInputs,
): { body: UpdateFulfillmentSettingsRequest | null; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const v: Record<string, number> = {};
  for (const r of ALL_ROWS) {
    const got = readSetting(r.unit, inputs[r.path] ?? "");
    if (typeof got === "string") errors[r.path] = got;
    else v[r.path] = got;
  }
  if (Object.keys(errors).length > 0) return { body: null, errors };
  const n = (p: SettingPath) => v[p] ?? 0;
  const stuckHours: Record<string, number> = {};
  for (const r of STUCK_ROWS) stuckHours[r.path.slice("stuckHours.".length)] = n(r.path);
  return {
    body: {
      version: s.version,
      stuckHours,
      pollMinutes: s.pollMinutes,
      callbackDeadlineHours: n("callbackDeadlineHours"),
      codOverdueDays: n("codOverdueDays"),
      agedDays: { held: n("agedDays.held"), inTransit: n("agedDays.inTransit") },
      returnRisk: {
        watchMinReturned: n("returnRisk.watchMinReturned"),
        highMinReturned: n("returnRisk.highMinReturned"),
        highRateBp: n("returnRisk.highRateBp"),
      },
      defaultWeightGramsPerItem: n("defaultWeightGramsPerItem"),
      quoteValidMinutes: n("quoteValidMinutes"),
    },
    errors,
  };
}

/** Tên trường trong lỗi 422 của máy chủ ("returnRisk.highRateBP") → đường của ô; lạ → undefined. */
export function rowOfServerField(field: string): SettingRow | undefined {
  const f = field.toLowerCase();
  return ALL_ROWS.find((r) => r.path.toLowerCase() === f);
}
