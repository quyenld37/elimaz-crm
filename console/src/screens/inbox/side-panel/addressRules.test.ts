// Nhận ra tin khách có địa chỉ để "Lấy địa chỉ từ tin HH:MM của khách" (Task 501; HV26).
import { describe, expect, it } from "vitest";
import type { ChatMessage } from "../../../api";
import { addressSuggestion, looksLikeAddress } from "./addressRules";

const m = (i: number, body: string, extra: Partial<ChatMessage> = {}): ChatMessage => ({
  id: `m${String(i)}`,
  conversationId: "c",
  direction: "in",
  kind: "message",
  origin: "customer",
  body,
  attachments: [],
  isHidden: false,
  removed: false,
  editedCount: 0,
  // 02:14Z = 09:14 giờ Việt Nam.
  sentAt: `2026-11-26T02:1${String(i)}:00Z`,
  ...extra,
});

describe("looksLikeAddress", () => {
  it.each([
    "12 ngõ 45 Trần Duy Hưng, Trung Hòa, Cầu Giấy, Hà Nội",
    "số 8 đường Lê Lợi phường Bến Thành",
    "Thôn 3 xã Hòa Phú, huyện Hòa Vang",
    "123/4 Hẻm 5, P.12, Q. Gò Vấp, TP HCM",
    "Ấp 2, thị trấn Củ Chi",
    "KHU PHỐ 4 PHƯỜNG LINH TRUNG",
  ])("có địa chỉ: %s", (s) => {
    expect(looksLikeAddress(s)).toBe(true);
  });
  it.each(["Chị lấy size M nhé", "0912 345 678", "Phường nào cũng được", "ok shop", "2 cái", ""])(
    "không phải địa chỉ: %s",
    (s) => {
      expect(looksLikeAddress(s)).toBe(false);
    },
  );
});

describe("addressSuggestion", () => {
  const now = Date.parse("2026-11-26T05:00:00Z");
  it("tin khách mới nhất có địa chỉ; giờ HH:MM khi cùng ngày (giờ Việt Nam), kèm ngày khi khác ngày", () => {
    const items = [
      m(1, "12 ngõ 45 Trần Duy Hưng, Cầu Giấy"),
      m(4, "Số 8 đường Lê Lợi, phường Bến Thành"),
      m(5, "Chị lấy size M nhé"),
      m(6, "Dạ shop gửi ạ", { direction: "out", origin: "staff" }),
    ];
    expect(addressSuggestion(items, now)).toEqual({
      label: "Lấy địa chỉ từ tin 09:14 của khách",
      text: "Số 8 đường Lê Lợi, phường Bến Thành",
    });
    const old = [m(1, "12 ngõ 45 Trần Duy Hưng", { sentAt: "2026-11-24T02:10:00Z" })];
    expect(addressSuggestion(old, now)?.label).toBe("Lấy địa chỉ từ tin 09:10 24/11 của khách");
  });
  it("bỏ tin của shop, ghi chú, tin khách đã xóa; không tin nào có địa chỉ → null; cắt 500 ký tự, bỏ khoảng trắng hai đầu", () => {
    expect(
      addressSuggestion(
        [
          m(1, "12 ngõ 45 Trần Duy Hưng", { direction: "out", origin: "staff" }),
          m(2, "12 ngõ 45 Trần Duy Hưng", { kind: "note", direction: "out", origin: "staff" }),
          m(3, "12 ngõ 45 Trần Duy Hưng", { removed: true }),
          m(4, "Chị lấy size M"),
        ],
        now,
      ),
    ).toBeNull();
    const long = `  12 ngõ 45 Trần Duy Hưng ${"x".repeat(600)}  `;
    expect(addressSuggestion([m(1, long)], now)?.text).toHaveLength(500);
    expect(addressSuggestion([m(1, " 12 ngõ 45 ")], now)?.text).toBe("12 ngõ 45");
  });
});
