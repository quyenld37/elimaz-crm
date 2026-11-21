// Luật thuần của màn Kết nối vận chuyển và Cài đặt vận chuyển (Task 132): nhãn trạng thái, lần kiểm, ô khóa, ô cài đặt.
import { describe, expect, it } from "vitest";
import type { CarrierAccount, FulfillmentSettings } from "../../api";
import {
  DEFAULT_SETTINGS,
  TEXT_POSITIVE,
  TEXT_RATE,
  accountKind,
  credentialFields,
  credentialsBody,
  isChecking,
  lastCheckCell,
  parseWebSort,
  readSetting,
  readSettings,
  rowOfServerField,
  settingInputs,
  statusChip,
  webText,
} from "./carrierRules";

const acc = (p: Partial<CarrierAccount> = {}): CarrierAccount => ({
  id: "a1",
  carrierKey: "vtp",
  mode: "api",
  environment: "production",
  name: "Viettel Post — Kho chính",
  status: "active",
  warehouseId: "w1",
  isDefaultForWarehouse: false,
  showOnWeb: false,
  webSort: 0,
  hasCredentials: true,
  needsRewrap: false,
  version: 1,
  ...p,
});

const settings = (p: Partial<FulfillmentSettings> = {}): FulfillmentSettings => ({
  ...DEFAULT_SETTINGS,
  pollMinutes: { awaiting_pickup: 120, shipping: 60, delivery_failed: 30, returning: 360, cancel_requested: 15 },
  version: 4,
  ...p,
});

describe("trạng thái tài khoản", () => {
  it.each([
    [acc({ status: "draft", hasCredentials: false }), "Nháp · chưa có khóa", "chip chip-line"],
    [acc({ status: "draft" }), "Nháp", "chip chip-line"],
    [acc({ status: "checking" }), "Đang kiểm tra…", "chip"],
    [acc({ status: "active" }), "Đang hoạt động", "chip chip-ok"],
    [acc({ status: "auth_failed" }), "Cần cấp quyền lại", "chip chip-err"],
    [acc({ status: "paused" }), "Tạm ngừng", "chip"],
    [acc({ status: "disabled" }), "Tạm ngừng", "chip"],
    [acc({ status: "revoked" }), "Đã thu hồi", "chip chip-dark"],
  ])("%#: %s", (a, text, cls) => {
    expect(statusChip(a)).toEqual({ text, cls });
  });

  it("đang kiểm: máy chủ checking, hoặc màn vừa gửi lệnh mà lần kiểm chưa đổi", () => {
    expect(isChecking(acc({ status: "checking" }), undefined)).toBe(true);
    expect(isChecking(acc({ lastCheckAt: "2026-11-21T02:12:00Z" }), "2026-11-21T02:12:00Z")).toBe(true);
    expect(isChecking(acc({ lastCheckAt: "2026-11-21T03:00:00Z" }), "2026-11-21T02:12:00Z")).toBe(false);
    expect(isChecking(acc({}), "")).toBe(true);
    expect(isChecking(acc({ lastCheckAt: "2026-11-21T03:00:00Z" }), "")).toBe(false);
  });

  it("lần kiểm gần nhất: giờ Việt Nam · kết quả; nhập tay, chưa kiểm, đang hỏi", () => {
    expect(lastCheckCell(acc({ lastCheckAt: "2026-11-21T02:12:00Z", lastCheckResult: "ok" }), false)).toEqual({
      text: "09:12 · 21/11 · Đạt",
      err: false,
      muted: false,
    });
    expect(
      lastCheckCell(acc({ status: "auth_failed", lastCheckAt: "2026-11-21T01:00:00Z", lastCheckResult: "x" }), false),
    ).toEqual({ text: "08:00 · 21/11 · Hãng từ chối khóa", err: true, muted: false });
    expect(lastCheckCell(acc({ mode: "manual" }), false).text).toBe("Không kiểm (nhập tay)");
    expect(lastCheckCell(acc(), false).text).toBe("Chưa kiểm");
    expect(lastCheckCell(acc(), true).text).toBe("Đang hỏi hãng");
  });

  it("dòng phụ và cột web", () => {
    expect(accountKind(acc())).toBe("Viettel Post · API");
    expect(accountKind(acc({ mode: "manual", carrierKey: "spx" }))).toBe("SPX · Nhập mã tay · không gọi API");
    expect(accountKind(acc({ mode: "manual", carrierKey: "self" }))).toBe("Tự giao · Nhập tay");
    expect(webText(acc({ showOnWeb: true, webSort: 3 }))).toBe("Có · thứ tự 3");
    expect(webText(acc())).toBe("Không");
  });
});

describe("khóa API", () => {
  it("ô theo hãng: Viettel Post tên đăng nhập, mật khẩu; hãng khác một ô token", () => {
    expect(credentialFields("vtp").map((f) => f.key)).toEqual(["username", "password"]);
    expect(credentialFields("best").map((f) => f.key)).toEqual(["token"]);
  });

  it("bỏ ô trống; rỗng hết → null", () => {
    expect(credentialsBody({ username: "u", password: "" })).toEqual({ username: "u" });
    expect(credentialsBody({ username: "", password: "" })).toBeNull();
  });

  it("thứ tự trên web 0–1000", () => {
    expect(parseWebSort("4")).toBe(4);
    expect(parseWebSort("0")).toBe(0);
    expect(parseWebSort("1001")).toBeNull();
    expect(parseWebSort("a")).toBeNull();
  });
});

describe("cài đặt vận chuyển", () => {
  it('tỷ lệ "30%" và "30" → 3000 phần vạn; "0", "101", chữ → lỗi tại ô', () => {
    expect(readSetting("%", "30%")).toBe(3000);
    expect(readSetting("%", "30")).toBe(3000);
    expect(readSetting("%", "12,5")).toBe(1250);
    expect(readSetting("%", "0")).toBe(TEXT_RATE);
    expect(readSetting("%", "101")).toBe(TEXT_RATE);
    expect(readSetting("%", "ba")).toBe(TEXT_RATE);
  });

  it("số nguyên > 0", () => {
    expect(readSetting("giờ", "48")).toBe(48);
    expect(readSetting("giờ", "0")).toBe(TEXT_POSITIVE);
    expect(readSetting("giờ", "1,5")).toBe(TEXT_POSITIVE);
    expect(readSetting("giờ", "")).toBe(TEXT_POSITIVE);
  });

  it("đọc rồi ghi lại không đổi gì; pollMinutes giữ nguyên, version đang xem", () => {
    const s = settings();
    const { body, errors } = readSettings(s, settingInputs(s));
    expect(errors).toEqual({});
    const { version, ...rest } = s;
    expect(body).toEqual({ ...rest, version });
  });

  it("ô sai → không có thân, lỗi theo đường", () => {
    const s = settings();
    const r = readSettings(s, { ...settingInputs(s), "returnRisk.highRateBp": "0", "stuckHours.shipping": "x" });
    expect(r.body).toBeNull();
    expect(r.errors).toEqual({ "returnRisk.highRateBp": TEXT_RATE, "stuckHours.shipping": TEXT_POSITIVE });
  });

  it("tên trường của lỗi máy chủ (highRateBP) khớp ô không phân biệt hoa thường", () => {
    expect(rowOfServerField("returnRisk.highRateBP")?.label).toBe("và tỷ lệ hoàn từ");
    expect(rowOfServerField("stuckHours.returning")?.label).toBe("Đang chuyển hoàn");
    expect(rowOfServerField("pollMinutes.shipping")).toBeUndefined();
  });
});
