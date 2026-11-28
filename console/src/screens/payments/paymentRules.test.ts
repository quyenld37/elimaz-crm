// Luật tiền của màn (kế hoạch M3 Task 138; mockup docs/mockups/m3/tien-tren-don.html, tra-doi.html).
import { describe, expect, it } from "vitest";
import type { RefundObligation } from "../../api";
import { TEXT_COD_LOCKED, codAfter, exchangeMoney, obligationState, paymentBlock, shortCredit } from "./paymentRules";

const ob = (p: Partial<RefundObligation> = {}): RefundObligation => ({
  id: "o1",
  orderId: "d1",
  source: "order_cancel",
  amount: 200_000,
  paidAmount: 0,
  status: "open",
  createdAt: "2026-11-22T02:00:00Z",
  version: 1,
  ...p,
});

describe("codAfter", () => {
  it("COD = tổng − đã thu − thu thêm, không âm", () => {
    expect(codAfter(2_160_000, 200_000, 500_000)).toBe(1_460_000);
    expect(codAfter(2_160_000, 0, 0)).toBe(2_160_000);
    expect(codAfter(100_000, 50_000, 80_000)).toBe(0);
  });
});

describe("paymentBlock", () => {
  it("đơn đã gửi hãng → câu khóa COD; trạng thái không nhận tiền; đã thu đủ; còn lại → null", () => {
    expect(paymentBlock({ status: "confirmed", currentShipmentId: "s1", grandTotal: 10, prepaidTotal: 0 })).toBe(
      TEXT_COD_LOCKED,
    );
    expect(TEXT_COD_LOCKED).toContain("Đơn đã gửi hãng, không đổi được tiền thu hộ");
    expect(paymentBlock({ status: "delivered", grandTotal: 10, prepaidTotal: 0 })).toBe(
      "Đơn ở trạng thái này không ghi phiếu thu được",
    );
    expect(paymentBlock({ status: "new", grandTotal: 10, prepaidTotal: 10 })).toBe("Đơn đã thu đủ tổng đơn");
    for (const status of ["draft", "new", "confirmed"] as const) {
      expect(paymentBlock({ status, grandTotal: 10, prepaidTotal: 0 })).toBeNull();
    }
  });
});

describe("obligationState", () => {
  it("chưa chi, chi một phần, đã chi đủ, miễn", () => {
    expect(obligationState(ob())).toEqual({
      chip: "Chưa chi",
      note: "Chưa chi — chưa tính là đã hoàn",
      remaining: 200_000,
    });
    expect(obligationState(ob({ amount: 620_000, paidAmount: 300_000 }))).toEqual({
      chip: "Còn 320.000 ₫",
      note: "Đã chi 300.000 ₫, còn nợ khách 320.000 ₫",
      remaining: 320_000,
    });
    expect(obligationState(ob({ status: "paid", paidAmount: 200_000 })).chip).toBe("Đã chi đủ");
    expect(obligationState(ob({ status: "waived", waiveReason: "khách đồng ý mất cọc" }))).toEqual({
      chip: "Miễn",
      note: "Miễn · khách đồng ý mất cọc",
      remaining: 0,
    });
  });
});

describe("exchangeMoney (refunds.creditAmount với đơn đổi chưa thu, phí ship 0)", () => {
  it("TD06: món mới 180.000, món trả 177.049 → cấn trừ 177.049, COD 2.951, không dư", () => {
    expect(exchangeMoney(177_049, 180_000)).toEqual({ credit: 177_049, cod: 2_951, surplus: 0 });
  });
  it("món trả đắt hơn: cấn trừ bằng món mới, COD 0, dư thành nghĩa vụ hoàn", () => {
    expect(exchangeMoney(495_000, 450_000)).toEqual({ credit: 450_000, cod: 0, surplus: 45_000 });
  });
  it("tiền 0 hai phía", () => {
    expect(exchangeMoney(0, 0)).toEqual({ credit: 0, cod: 0, surplus: 0 });
  });
});

describe("shortCredit (Task 116: kho nhận thiếu)", () => {
  it("đã cấn trừ nhiều hơn tiền hoàn cuối → phần chênh; còn lại 0", () => {
    expect(shortCredit(177_049, 0)).toBe(177_049);
    expect(shortCredit(177_049, 100_000)).toBe(77_049);
    expect(shortCredit(150_000, 177_049)).toBe(0);
  });
});
