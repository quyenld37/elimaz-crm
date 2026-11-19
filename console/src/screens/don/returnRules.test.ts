// Tiền hoàn dự kiến và số còn trả được của hộp thoại Trả/đổi (kế hoạch M2 Task M2-28): cùng cách tính với máy chủ
// (pricing.RefundFor, order.priceReturnLines của Task M2-22). Ca lấy từ Step 1 của Task M2-22.
import { describe, expect, it } from "vitest";
import type { OrderItem, Return, TimelineItem } from "../../api";
import { deliveredAt, daysSince, refundFor, returnableLines } from "./returnRules";

const item = (id: string, qty: number, netAmount: number): OrderItem => ({
  id,
  position: 1,
  variantId: `v-${id}`,
  sku: `SKU-${id}`,
  name: `Món ${id}`,
  qty,
  unitPrice: netAmount,
  netAmount,
});

const ret = (p: Partial<Return> & Pick<Return, "items">): Return => ({
  id: "r",
  code: "TH0000001",
  orderId: "o",
  kind: "customer_return",
  status: "requested",
  refundAmount: 0,
  createdAt: "2026-11-18T00:00:00Z",
  version: 1,
  ...p,
});

describe("refundFor (bản của pricing.RefundFor)", () => {
  it("trả hết một dòng 1 món → đủ net", () => {
    expect(refundFor(250000, 1, 0, 1, 0)).toBe(250000);
  });
  it("làm tròn: net 500001 × 2, trả lần lượt 1 rồi 1 → 250000 rồi 250001 (món cuối nhận phần lẻ)", () => {
    const first = refundFor(500001, 2, 0, 1, 0);
    expect(first).toBe(250000);
    expect(refundFor(500001, 2, 1, 1, first)).toBe(250001);
  });
  it("đầu vào sai (trả quá, số âm, trả 0) → 0", () => {
    expect(refundFor(100, 2, 1, 2, 0)).toBe(0);
    expect(refundFor(-1, 2, 0, 1, 0)).toBe(0);
    expect(refundFor(100, 2, 0, 0, 0)).toBe(0);
    expect(refundFor(100, 2, 0, 1, 101)).toBe(0);
  });
  it("số lớn (gần 2^53) không sai vì nhân trước khi chia", () => {
    const net = 9_007_199_254_740_991;
    expect(refundFor(net, 3, 0, 1, 0)).toBe(3_002_399_751_580_330);
  });
});

describe("returnableLines", () => {
  const items = [item("a", 3, 1_860_000), item("b", 1, 290_000)];

  it("chưa có phiếu: còn trả được bằng số mua", () => {
    const ls = returnableLines(items, []);
    expect(ls.map((l) => [l.item.id, l.returned, l.remaining])).toEqual([
      ["a", 0, 3],
      ["b", 0, 1],
    ]);
    expect(ls[0]?.refund(1)).toBe(620_000);
  });

  it("phiếu chưa hủy trừ số còn trả được (cả phiếu đang mở); phiếu đã hủy không trừ", () => {
    const rs = [
      ret({ status: "requested", items: [{ id: "i1", orderItemId: "a", qty: 2, refundAmount: 1_240_000 }] }),
      ret({ status: "cancelled", items: [{ id: "i2", orderItemId: "b", qty: 1, refundAmount: 290_000 }] }),
      ret({ status: "restocked", items: [{ id: "i3", orderItemId: "b", qty: 1, refundAmount: 290_000 }] }),
    ];
    const ls = returnableLines(items, rs);
    expect(ls.map((l) => [l.item.id, l.returned, l.remaining])).toEqual([
      ["a", 2, 1],
      ["b", 1, 0],
    ]);
    // Món cuối của dòng a nhận phần còn lại: 1.860.000 − 1.240.000.
    expect(ls[0]?.refund(1)).toBe(620_000);
    expect(ls[1]?.refund(1)).toBe(0);
  });

  it("dòng phiếu 'thiếu' (tiền hoàn đã về 0): tính phần lẽ ra của nó vào đã hoàn, như máy chủ", () => {
    const rs = [
      ret({
        status: "restocked",
        items: [{ id: "i1", orderItemId: "a", qty: 1, refundAmount: 0, condition: "missing" }],
      }),
    ];
    const [a] = returnableLines([item("a", 3, 1_000_000)], rs);
    // Đã hoàn trước = floor(1.000.000 × 1 / 3) = 333.333; trả nốt 2 món → 666.667.
    expect(a?.refund(2)).toBe(666_667);
    expect(a?.refund(1)).toBe(333_333);
  });
});

describe("ngày giao thành công", () => {
  const tl: TimelineItem[] = [
    { at: "2026-11-10T03:00:00Z", kind: "status", refId: "h2", summary: { from: "confirmed", to: "delivered" } },
    { at: "2026-11-09T03:00:00Z", kind: "note", refId: "n1" },
    { at: "2026-11-01T03:00:00Z", kind: "status", refId: "h1", summary: { to: "confirmed" } },
  ];
  it("lấy lúc của mục đổi trạng thái sang delivered; không có → null", () => {
    expect(deliveredAt(tl)).toBe("2026-11-10T03:00:00Z");
    expect(deliveredAt(tl.slice(1))).toBeNull();
  });
  it("số ngày đã qua (làm tròn xuống)", () => {
    expect(daysSince("2026-11-10T03:00:00Z", new Date("2026-11-19T02:00:00Z"))).toBe(8);
    expect(daysSince("2026-11-10T03:00:00Z", new Date("2026-11-19T04:00:00Z"))).toBe(9);
  });
});
