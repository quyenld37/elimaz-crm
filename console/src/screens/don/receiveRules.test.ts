// Quy tắc nhận hàng theo món (HV26): kiểm tổng từng dòng, câu báo, thân gửi máy chủ, câu "nhận xong thì…".
import { describe, expect, it } from "vitest";
import type { Return } from "../../api";
import { checkReceive, parseQty, receiveEffects, receiveLines, type Split } from "./receiveRules";

const ret = (kind: Return["kind"]): Return => ({
  id: "r",
  code: "TH1",
  orderId: "o",
  kind,
  status: "requested",
  refundAmount: 0,
  items: [
    { id: "1", orderItemId: "a", qty: 2, refundAmount: 0 },
    { id: "2", orderItemId: "b", qty: 1, refundAmount: 0 },
  ],
  createdAt: "",
  version: 1,
});
const sp = (s: string, d: string, m: string): Split => ({ sellable: s, damaged: d, missing: m });
const sku = (id: string) => id.toUpperCase();

describe("parseQty", () => {
  it.each([
    ["", 0],
    [" 3 ", 3],
    ["0", 0],
    ["-1", null],
    ["1.5", null],
    ["a", null],
    ["123456", null],
  ])("%j → %j", (s, want) => {
    expect(parseQty(s)).toBe(want);
  });
});

describe("checkReceive", () => {
  it("chưa nhập gì: thiếu cả 3 món, hai dòng đỏ", () => {
    const c = checkReceive(ret("carrier_return"), new Map(), sku);
    expect(c.ok).toBe(false);
    expect(c.message).toBe("Còn thiếu 3 món chưa phân loại (A, B).");
    expect([...c.badLines]).toEqual(["a", "b"]);
  });
  it("gửi 2 nhập 1/0/0 → thiếu 1", () => {
    const c = checkReceive(
      ret("carrier_return"),
      new Map([
        ["a", sp("1", "0", "0")],
        ["b", sp("1", "", "")],
      ]),
      sku,
    );
    expect(c.message).toBe("Còn thiếu 1 món chưa phân loại (A).");
    expect([...c.badLines]).toEqual(["a"]);
  });
  it("thừa thì báo thừa trước thiếu; chữ thì báo chữ trước", () => {
    const over = checkReceive(ret("carrier_return"), new Map([["a", sp("2", "1", "0")]]), sku);
    expect(over.message).toBe("Tổng vượt số trên phiếu (A thừa 1).");
    const bad = checkReceive(
      ret("carrier_return"),
      new Map([
        ["a", sp("x", "1", "0")],
        ["b", sp("5", "", "")],
      ]),
      sku,
    );
    expect(bad.message).toBe("Chỉ nhập số nguyên không âm (A).");
  });
  it("đủ: ok, tổng ba loại", () => {
    const c = checkReceive(
      ret("carrier_return"),
      new Map([
        ["a", sp("1", "1", "0")],
        ["b", sp("0", "0", "1")],
      ]),
      sku,
    );
    expect(c).toMatchObject({ ok: true, message: null, totals: { sellable: 1, damaged: 1, missing: 1 } });
  });
});

describe("receiveLines, receiveEffects", () => {
  it("mỗi dòng đủ ba số; condition theo loại đầu tiên khác 0", () => {
    const r = ret("carrier_return");
    expect(
      receiveLines(
        r,
        new Map([
          ["a", sp("0", "2", "")],
          ["b", sp("", "", "1")],
        ]),
      ),
    ).toEqual([
      { orderItemId: "a", condition: "damaged", sellable: 0, damaged: 2, missing: 0 },
      { orderItemId: "b", condition: "missing", sellable: 0, damaged: 0, missing: 1 },
    ]);
  });
  it("món thiếu: hãng → thất lạc, khiếu nại; khách → không hoàn", () => {
    const t = { sellable: 0, damaged: 0, missing: 2 };
    expect(receiveEffects(ret("carrier_return"), t)).toEqual([
      '2 món thiếu ghi thất lạc (trừ tồn) và vào "Cần xem" để khiếu nại hãng',
    ]);
    expect(receiveEffects(ret("customer_return"), t)).toEqual(["2 món thiếu không nhập kho, không hoàn tiền"]);
  });
});
