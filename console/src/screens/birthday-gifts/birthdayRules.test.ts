// Luật thuần của màn Quà sinh nhật (kế hoạch M5 Task 209): chữ tám trạng thái, che SĐT, liên kết Zalo, tin chép theo
// mẫu của website cũ, ngày, kiểm form cài đặt theo giới hạn của API (Task 468).
import { describe, expect, it } from "vitest";
import type { BirthdaySettings } from "../../api";
import {
  birthdayInText,
  callName,
  codeState,
  domesticPhone,
  giftMessage,
  lastDay,
  maskPhone,
  readSettings,
  settingsDraft,
  stateChip,
  thanksText,
  zaloLink,
} from "./birthdayRules";

const SETTINGS: BirthdaySettings = {
  enabled: true,
  amount: 50_000,
  minOrder: 0,
  daysBefore: 7,
  validDays: 14,
  minLeadDays: 30,
  version: 3,
};

describe("SĐT", () => {
  it("che giữa, đổi +84 sang 0 cho Zalo; số lạ giữ nguyên", () => {
    expect(maskPhone("+84912345678")).toBe("0912 *** 678");
    expect(maskPhone("+842412345678")).toBe("0241 *** 678");
    expect(domesticPhone("+84912345678")).toBe("0912345678");
    expect(domesticPhone("0912345678")).toBe("0912345678");
    expect(zaloLink("+84912345678")).toBe("https://zalo.me/0912345678");
  });
});

describe("Ngày", () => {
  it("số ngày tới sinh nhật thành chữ", () => {
    expect(birthdayInText(0)).toBe("hôm nay");
    expect(birthdayInText(3)).toBe("còn 3 ngày");
    expect(birthdayInText(-2)).toBe("đã qua 2 ngày");
  });
  it("hạn dùng: ngày cuối theo giờ VN (validTo là 0:00 ngày sau)", () => {
    expect(lastDay("2026-11-30T17:00:00Z")).toBe("30/11/2026");
  });
});

describe("Tám trạng thái", () => {
  it("chữ và màu như mockup; Đến hạn khi quà tắt ghi rõ", () => {
    expect(stateChip("manual", true)).toEqual({ label: "Cần nhắn", className: "chip chip-err" });
    expect(stateChip("due", true)).toEqual({ label: "Đến hạn, chờ tạo mã", className: "chip chip-err" });
    expect(stateChip("due", false)).toEqual({ label: "Đến hạn · quà đang tắt", className: "chip" });
    expect(stateChip("stopped", true).label).toBe("Đã hủy nhận");
    expect(stateChip("used", true).className).toBe("chip chip-ok");
  });
});

describe("Mã trong đợt", () => {
  const now = Date.parse("2026-11-24T03:00:00Z");
  const c = { usedCount: 0, validTo: "2026-12-01T17:00:00Z", givenAt: undefined, status: "active" as const };
  it("đã dùng, đã tặng, chưa tặng, hết hạn", () => {
    expect(codeState({ ...c, usedCount: 1 }, now).label).toBe("Đã dùng");
    expect(codeState({ ...c, givenAt: "2026-11-08T03:00:00Z" }, now).label).toBe("Đã tặng 08/11");
    expect(codeState(c, now).label).toBe("Chưa tặng");
    expect(codeState({ ...c, validTo: "2026-11-09T17:00:00Z" }, now)).toEqual({
      label: "Hết hạn, chưa dùng",
      className: "chip",
      muted: true,
    });
  });
});

describe("Tin chép", () => {
  it("như giftMessage của website cũ: tên gọi, mã, mức giảm, đơn tối thiểu, hạn, số che, liên kết", () => {
    expect(callName("  Nguyễn Thị Lan ")).toBe("Lan");
    const text = giftMessage({
      fullName: "Nguyễn Thị Lan",
      phone: "+84912345678",
      code: "SN4E1C95",
      amount: 50_000,
      minOrder: 300_000,
      validFrom: "2026-11-16T17:00:00Z",
      validTo: "2026-12-01T17:00:00Z",
    });
    expect(text).toBe(
      [
        "Mừng sinh nhật Lan! ELIMAZ tặng bạn mã SN4E1C95 giảm 50.000 ₫ cho đơn từ 300.000 ₫.",
        "Dùng từ 17/11 đến 1/12, cho đơn giao tới số 0912 *** 678.",
        "Mua sắm với quà sinh nhật: https://elimaz.com.vn/?qua=SN4E1C95",
      ].join("\n"),
    );
  });
  it("không đơn tối thiểu, tên rỗng", () => {
    const text = giftMessage({
      fullName: "",
      phone: "+84912345678",
      code: "A-1",
      amount: 10_000,
      minOrder: 0,
      validFrom: "2026-11-16T17:00:00Z",
      validTo: "2026-11-17T17:00:00Z",
    });
    expect(text.split("\n")[0]).toBe("Mừng sinh nhật! ELIMAZ tặng bạn mã A-1 giảm 10.000 ₫.");
  });
});

describe("Form cài đặt", () => {
  it("bản nháp từ cài đặt, đọc lại đúng số", () => {
    const d = settingsDraft(SETTINGS);
    expect(d.amount).toBe("50.000");
    expect(readSettings(d)).toEqual({
      fields: { enabled: true, amount: 50_000, minOrder: 0, daysBefore: 7, validDays: 14, minLeadDays: 30 },
      errors: {},
    });
  });
  it("lỗi theo giới hạn của API: mệnh giá bội 1.000, khoảng từng ô", () => {
    const d = settingsDraft(SETTINGS);
    const r = readSettings({
      ...d,
      amount: "10.500",
      minOrder: "100.000.001",
      daysBefore: "31",
      validDays: "0",
      minLeadDays: "abc",
    });
    expect(r.fields).toBeNull();
    expect(r.errors).toEqual({
      amount: "Mức giảm từ 10.000 tới 1.000.000 ₫, bội số của 1.000.",
      minOrder: "Đơn tối thiểu từ 0 tới 100.000.000 ₫.",
      daysBefore: "Nhập số nguyên 0–30.",
      validDays: "Nhập số nguyên 1–60.",
      minLeadDays: "Nhập số nguyên 0–120.",
    });
    expect(readSettings({ ...d, amount: "9.000" }).errors.amount).toBeDefined();
    expect(readSettings({ ...d, amount: "1.001.000" }).errors.amount).toBeDefined();
    expect(readSettings({ ...d, amount: "1.000.000", minOrder: "" }).errors).toEqual({});
  });
  it("câu trên trang cảm ơn", () => {
    expect(thanksText({ amount: 50_000, daysBefore: 7, validDays: 14 })).toBe(
      "Trước sinh nhật 7 ngày, bạn nhận mã giảm 50.000 ₫, dùng trong 14 ngày.",
    );
    expect(thanksText({ amount: 50_000, daysBefore: 0, validDays: 14 })).toBe(
      "Đúng ngày sinh nhật, bạn nhận mã giảm 50.000 ₫, dùng trong 14 ngày.",
    );
  });
});
