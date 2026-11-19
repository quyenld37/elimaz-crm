// Luật thuần của màn Cài đặt đơn: ô nhập theo giờ, %, ngày ↔ giá trị của API (phút, điểm cơ bản, ngày).
import { describe, expect, it } from "vitest";
import type { OrderSettings } from "../../api";
import { SETTING_ROWS, outOfRangeText, readSettings, settingInputs } from "./settingsRules";

const cur: OrderSettings = {
  draftTtlMinutes: 1440,
  duplicateWindowMinutes: 1440,
  discountApprovalThresholdBp: 1000,
  returnWarningDays: 7,
  version: 5,
};

describe("đổi đơn vị", () => {
  it("giá trị hiện hành → ô (giờ, %, ngày)", () => {
    expect(settingInputs(cur)).toEqual({
      draftTtlMinutes: "24",
      duplicateWindowMinutes: "24",
      discountApprovalThresholdBp: "10",
      returnWarningDays: "7",
    });
    expect(settingInputs({ ...cur, draftTtlMinutes: 10, discountApprovalThresholdBp: 1250 })).toMatchObject({
      draftTtlMinutes: "0,17",
      discountApprovalThresholdBp: "12,5",
    });
  });
  it("ô → giá trị gửi (giờ lẻ làm tròn tới phút)", () => {
    const r = readSettings({
      draftTtlMinutes: "0,2",
      duplicateWindowMinutes: "0",
      discountApprovalThresholdBp: "12,5",
      returnWarningDays: "30",
    });
    expect(r.errors).toEqual({});
    expect(r.values).toEqual({
      draftTtlMinutes: 12,
      duplicateWindowMinutes: 0,
      discountApprovalThresholdBp: 1250,
      returnWarningDays: 30,
    });
  });
  it("ô không phải số → lỗi theo tên ô; không kiểm khoảng ở màn (máy chủ kiểm)", () => {
    const r = readSettings({ ...settingInputs(cur), draftTtlMinutes: "200", returnWarningDays: "7,5" });
    expect(r.values).toBeNull();
    expect(Object.keys(r.errors)).toEqual(["returnWarningDays"]);
  });
});

it("câu ngoài khoảng theo tên trường; tên lạ → câu chung", () => {
  expect(outOfRangeText("draftTtlMinutes")).toBe("Giữ đơn nháp chỉ từ 10 phút đến 7 ngày");
  expect(outOfRangeText("returnWarningDays")).toBe("Cảnh báo đổi trả muộn chỉ từ 0 đến 365 ngày");
  expect(outOfRangeText("khac")).toBe("Giá trị cài đặt ngoài khoảng cho phép");
  expect(SETTING_ROWS.map((r) => r.field)).toEqual([
    "draftTtlMinutes",
    "duplicateWindowMinutes",
    "discountApprovalThresholdBp",
    "returnWarningDays",
  ]);
});
