// Luật thuần của màn Cài đặt đơn (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/cai-dat-don.html). Khoảng hợp lệ do
// máy chủ kiểm (order.UpdateSettings, 422 setting_out_of_range nêu tên trường); ở đây chỉ đổi đơn vị ô nhập (giờ, %,
// ngày) sang đơn vị của API (phút, điểm cơ bản, ngày) và dựng câu theo tên trường. Chữ giới hạn khớp
// internal/modules/order/settings.go: hạn đơn nháp 10–10080 phút, nghi trùng 0–10080 phút, duyệt giảm 0–10000 điểm
// cơ bản, cảnh báo hoàn 0–365 ngày.
import type { OrderSettings } from "../../api";
import { ERROR_MESSAGES } from "../../app/errors";
import { percentInput, percentOrZero } from "../ma-giam-gia/couponRules";

export type SettingField = Exclude<keyof OrderSettings, "version">;
export type SettingInputs = Record<SettingField, string>;
type Unit = "hours" | "percent" | "days";

export interface SettingRow {
  field: SettingField;
  title: string;
  desc: string;
  unit: Unit;
  hint: string;
  outOfRange: string;
}

export const SETTING_ROWS: readonly SettingRow[] = [
  {
    field: "draftTtlMinutes",
    title: "Giữ đơn nháp",
    desc: "Đơn nháp giữ hàng trong thời gian này rồi tự hủy, nhả hàng. Dùng khi đang chờ khách gửi địa chỉ.",
    unit: "hours",
    hint: "10 phút (0,17 giờ) đến 7 ngày (168 giờ); nhập số lẻ được",
    outOfRange: "Giữ đơn nháp chỉ từ 10 phút đến 7 ngày",
  },
  {
    field: "duplicateWindowMinutes",
    title: "Khoảng tìm đơn trùng",
    desc: "Hai đơn cùng khách hoặc cùng SĐT người nhận, có sản phẩm chung, tạo cách nhau dưới khoảng này thì bị đánh dấu nghi trùng. 0 là tắt.",
    unit: "hours",
    hint: "0 đến 7 ngày (168 giờ)",
    outOfRange: "Khoảng tìm đơn trùng chỉ từ 0 đến 7 ngày",
  },
  {
    field: "discountApprovalThresholdBp",
    title: "Mức giảm tay cần duyệt",
    desc: "Giảm dòng cộng giảm cấp đơn vượt mức này (tính trên tạm tính) thì đơn chờ Chủ hoặc Quản lý duyệt. Mã giảm giá không tính.",
    unit: "percent",
    hint: "0 đến 100%",
    outOfRange: "Mức giảm tay cần duyệt chỉ từ 0 đến 100%",
  },
  {
    field: "returnWarningDays",
    title: "Cảnh báo đổi trả muộn",
    desc: "Tạo phiếu trả cho đơn giao xong quá số ngày này thì hiện cảnh báo; vẫn tạo được.",
    unit: "days",
    hint: "0 đến 365",
    outOfRange: "Cảnh báo đổi trả muộn chỉ từ 0 đến 365 ngày",
  },
];

export const UNIT_LABEL: Readonly<Record<Unit, string>> = { hours: "giờ", percent: "%", days: "ngày" };
const BAD_INPUT: Readonly<Record<Unit, string>> = {
  hours: "Nhập số giờ, ví dụ 24 hoặc 0,5",
  percent: "Nhập số %, ví dụ 10 hoặc 12,5",
  days: "Nhập số ngày nguyên",
};

/** Ô giờ ("24", "0,5") → phút, làm tròn tới phút; sai dạng → null. Không kiểm khoảng (máy chủ kiểm). */
function parseHours(s: string): number | null {
  const t = s.trim().replace(",", ".");
  return /^\d{1,6}(\.\d{1,2})?$/.test(t) ? Math.round(Number(t) * 60) : null;
}

const hoursText = (minutes: number) => String(Number((minutes / 60).toFixed(2))).replace(".", ",");

function toInput(unit: Unit, v: number): string {
  if (unit === "hours") return hoursText(v);
  if (unit === "percent") return percentInput(v);
  return String(v);
}

function fromInput(unit: Unit, s: string): number | null {
  if (unit === "hours") return parseHours(s);
  if (unit === "percent") return percentOrZero(s);
  return /^\d{1,6}$/.test(s.trim()) ? Number(s.trim()) : null;
}

/** Giá trị hiện hành → chữ của bốn ô. */
export function settingInputs(s: OrderSettings): SettingInputs {
  const out: Partial<SettingInputs> = {};
  for (const r of SETTING_ROWS) out[r.field] = toInput(r.unit, s[r.field]);
  return {
    draftTtlMinutes: out.draftTtlMinutes ?? "",
    duplicateWindowMinutes: out.duplicateWindowMinutes ?? "",
    discountApprovalThresholdBp: out.discountApprovalThresholdBp ?? "",
    returnWarningDays: out.returnWarningDays ?? "",
  };
}

/** Bốn ô → giá trị gửi; ô sai dạng → values null kèm lỗi theo tên trường. */
export function readSettings(inputs: SettingInputs): {
  values: Record<SettingField, number> | null;
  errors: Partial<Record<SettingField, string>>;
} {
  const errors: Partial<Record<SettingField, string>> = {};
  const values: Partial<Record<SettingField, number>> = {};
  for (const r of SETTING_ROWS) {
    const v = fromInput(r.unit, inputs[r.field]);
    if (v === null) errors[r.field] = BAD_INPUT[r.unit];
    else values[r.field] = v;
  }
  const { draftTtlMinutes, duplicateWindowMinutes, discountApprovalThresholdBp, returnWarningDays } = values;
  if (
    draftTtlMinutes === undefined ||
    duplicateWindowMinutes === undefined ||
    discountApprovalThresholdBp === undefined ||
    returnWarningDays === undefined
  )
    return { values: null, errors };
  return {
    values: { draftTtlMinutes, duplicateWindowMinutes, discountApprovalThresholdBp, returnWarningDays },
    errors,
  };
}

/** Câu khi máy chủ báo trường ngoài khoảng (thân lỗi setting_out_of_range.field); tên lạ → câu chung của mã lỗi. */
export function outOfRangeText(field: string): string {
  return SETTING_ROWS.find((r) => r.field === field)?.outOfRange ?? ERROR_MESSAGES.setting_out_of_range ?? "";
}
