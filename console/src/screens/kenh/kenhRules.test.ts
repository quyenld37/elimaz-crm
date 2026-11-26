// Luật tại chỗ của màn cài đặt hộp chat (Task 178).
import { describe, expect, it } from "vitest";
import {
  charCount,
  hasOnPage,
  intInRange,
  isFacebookAuthorizeUrl,
  quickReplyError,
  slotErrors,
  tagClass,
  unknownVariables,
} from "./kenhRules";

describe("đếm và giới hạn", () => {
  it("đếm theo ký tự như máy chủ (rune), không theo đơn vị UTF-16", () => {
    expect(charCount("Tư vấn")).toBe(6);
    expect(charCount("😀a")).toBe(2);
  });
  it("nút trả lời nhanh: rỗng, 20 được, 21 lỗi", () => {
    expect(quickReplyError(" ")).toBe("Nhập chữ trên nút.");
    expect(quickReplyError("a".repeat(20))).toBeNull();
    expect(quickReplyError("Tư vấn size áo và váy")).toBe("Tối đa 20 ký tự (đang 21).");
  });
  it("số nguyên trong khoảng", () => {
    expect(intInRange("24", 1, 72)).toBe(24);
    expect(intInRange("0", 1, 72)).toBeNull();
    expect(intInRange("1.5", 1, 72)).toBeNull();
    expect(intInRange("", 1, 72)).toBeNull();
  });
});

it("biến lạ theo mẫu biến của máy chủ, không trùng, theo thứ tự", () => {
  expect(unknownVariables("Chào {ten_khach} {abc} {x_y} {abc} {Hoa}")).toEqual(["abc", "x_y"]);
  expect(unknownVariables("{ma_don}{ma_van_don}")).toEqual([]);
});

describe("khung giờ live (Task 160 quyết định 6)", () => {
  const s = (from: string, to: string, src = "s") => ({ from, to, trafficSourceId: src });
  it("chạm mép không chồng; chồng thì cả hai khung báo", () => {
    expect(slotErrors([s("11:00", "12:00"), s("12:00", "13:00")])).toEqual([undefined, undefined]);
    const e = slotErrors([s("19:00", "23:00"), s("22:00", "23:30")]);
    expect(e[0]).toBe("Chồng với khung 22:00–23:30.");
    expect(e[1]).toBe("Chồng với khung 19:00–23:00.");
  });
  it("giờ sai dạng, bắt đầu không trước kết thúc, thiếu nguồn", () => {
    expect(slotErrors([s("7:00", "08:00")])[0]).toBe("Giờ dạng HH:MM, ví dụ 19:00.");
    expect(slotErrors([s("23:00", "01:00")])[0]).toMatch(/trước giờ kết thúc/);
    expect(slotErrors([s("10:00", "11:00", "")])[0]).toBe("Chọn nguồn đơn.");
  });
});

it("quyền theo Page: cả shop hoặc đúng Page", () => {
  const g = [{ permission: "conversation.settings", scope: { type: "page" as const, id: "p1" } }];
  expect(hasOnPage(g, "conversation.settings", "p1")).toBe(true);
  expect(hasOnPage(g, "conversation.settings", "p2")).toBe(false);
  expect(
    hasOnPage([{ permission: "conversation.settings", scope: { type: "all", id: "" } }], "conversation.settings", "p2"),
  ).toBe(true);
});

it("địa chỉ đăng nhập chỉ là https://www.facebook.com", () => {
  expect(isFacebookAuthorizeUrl("https://www.facebook.com/v23.0/dialog/oauth?x=1")).toBe(true);
  expect(isFacebookAuthorizeUrl("http://www.facebook.com/dialog")).toBe(false);
  expect(isFacebookAuthorizeUrl("https://www.facebook.com.evil.example/")).toBe(false);
  expect(isFacebookAuthorizeUrl("javascript:alert(1)")).toBe(false);
});

it("màu nhãn lạ vẽ như Xám", () => {
  expect(tagClass("success")).toBe("lb-3");
  expect(tagClass("tím")).toBe("lb-2");
});
