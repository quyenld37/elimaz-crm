// Luật thuần của khối Vận chuyển sau khi gửi (Task 134): giờ Việt Nam của ô "Lúc", giờ kẹt, câu lỗi, lý do bỏ qua.
import { describe, expect, it } from "vitest";
import { cancelledText, fromVnLocal, hoursSilent, ignoredText, syncErrorText, toVnLocal } from "./shipmentRules";
import { shipment } from "./shippingTestkit";

describe("giờ Việt Nam", () => {
  it("đi về hai chiều, không phụ thuộc múi của máy", () => {
    expect(toVnLocal(Date.parse("2026-11-21T17:05:00Z"))).toBe("2026-11-22T00:05");
    expect(fromVnLocal("2026-11-22T00:05")).toBe("2026-11-21T17:05:00.000Z");
  });
  it("sai dạng, ngày không có → null", () => {
    for (const v of ["", "2026-11-22", "2026-02-30T10:00", "2026-11-22T24:00", "22/11/2026 16:30"]) {
      expect(fromVnLocal(v)).toBeNull();
    }
  });
});

describe("giờ hãng chưa cập nhật", () => {
  const now = Date.parse("2026-11-22T07:00:00Z");
  it("lấy sự kiện hãng gần nhất, không có thì lúc đổi trạng thái, rồi lúc tạo; làm tròn xuống", () => {
    expect(hoursSilent(shipment({ lastCarrierEventAt: "2026-11-18T23:00:00Z" }), now)).toBe(80);
    expect(hoursSilent(shipment({ lastStatusAt: "2026-11-22T05:01:00Z" }), now)).toBe(1);
    expect(hoursSilent(shipment({ createdAt: "2026-11-22T06:59:00Z" }), now)).toBe(0);
  });
});

describe("câu chữ", () => {
  it("mã lạ không bao giờ hiện chữ thô", () => {
    expect(syncErrorText("timeout")).toBe("hãng không trả lời");
    expect(syncErrorText("HTTP 500: <html>")).toBe("hãng báo lỗi");
    expect(cancelledText("replace")).toBe("Đã hủy (đổi hãng)");
    expect(cancelledText("la")).toBe("Đã hủy");
    expect(cancelledText(undefined)).toBe("Đã hủy");
  });
  it("chỉ sự kiện applied false là bị bỏ qua", () => {
    const e = { occurredAt: "", recordedAt: "", source: "carrier_api" as const, rawStatus: "X" };
    expect(ignoredText({ ...e, applied: true })).toBeNull();
    expect(ignoredText(e)).toBeNull();
    expect(ignoredText({ ...e, applied: false, ignoredReason: "not_current" })).toBe(
      "Bỏ qua: vận đơn không còn là vận đơn hiện hành của đơn",
    );
    expect(ignoredText({ ...e, applied: false, ignoredReason: "zzz" })).toBe("Bỏ qua: không đổi trạng thái");
  });
});
