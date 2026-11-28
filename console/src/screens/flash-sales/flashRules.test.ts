// Luật của màn Đợt flash sale (Task 206, mockup docs/mockups/m5/flash-sales.html): chữ trạng thái, giờ, kiểm form, tóm tắt
// lên lịch, lỗi thiếu hàng, số trực tiếp.
import { describe, expect, it } from "vitest";
import type { FlashItem, FlashLive, FlashSaleSummary } from "../../api";
import { ApiError } from "../../app/errors";
import {
  actionsOf,
  barPercents,
  liveTotals,
  longTime,
  parseMoney,
  refreshMs,
  remainingText,
  saleSub,
  scheduleSummary,
  shortagesOf,
  statusChip,
  toItemInput,
  toSaleInput,
  validateItem,
  validateSale,
} from "./flashRules";

const sale = (p: Partial<FlashSaleSummary> = {}): FlashSaleSummary => ({
  id: "s1",
  name: "Đợt",
  startsAt: "2026-12-11T17:00:00Z",
  endsAt: "2026-12-11T19:00:00Z",
  status: "draft",
  version: 1,
  itemCount: 6,
  createdAt: "2026-11-01T00:00:00Z",
  updatedAt: "2026-11-01T00:00:00Z",
  ...p,
});

const item = (sku: string, qty: number, p: Partial<FlashItem> = {}): FlashItem => ({
  id: `i-${sku}`,
  variantId: `v-${sku}`,
  sellerId: "sl",
  sku,
  name: sku,
  listPrice: 690_000,
  salePrice: 199_000,
  quantity: qty,
  perCustomerLimit: 1,
  holdMinutes: 10,
  version: 1,
  ...p,
});

describe("statusChip", () => {
  it("bốn trạng thái có nhãn riêng", () => {
    expect(statusChip("draft").label).toBe("Nháp");
    expect(statusChip("scheduled").label).toBe("Đã lên lịch");
    expect(statusChip("open").label).toBe("Đang mở");
    expect(statusChip("closed").label).toBe("Đã đóng");
  });
});

describe("saleSub / remainingText", () => {
  const now = Date.parse("2026-12-11T17:30:00Z");
  it("còn bao lâu: giờ phút, chỉ phút, đã hết", () => {
    expect(remainingText("2026-12-11T19:00:00Z", now)).toBe("còn 1 giờ 30 phút");
    expect(remainingText("2026-12-11T17:45:00Z", now)).toBe("còn 15 phút");
    expect(remainingText("2026-12-11T17:30:20Z", now)).toBe("còn dưới 1 phút");
    expect(remainingText("2026-12-11T17:00:00Z", now)).toBe("đã quá giờ đóng");
  });
  it("dòng phụ theo trạng thái", () => {
    expect(saleSub(sale(), now)).toBe("6 món · chưa giữ suất nào");
    expect(saleSub(sale({ status: "scheduled" }), now)).toBe("6 món · tự mở lúc 00:00 12/12");
    expect(saleSub(sale({ status: "open" }), now)).toBe("6 món · đang mở, còn 1 giờ 30 phút");
    expect(saleSub(sale({ status: "closed", closedReason: "manual" }), now)).toBe("6 món · đóng sớm");
    expect(saleSub(sale({ status: "closed", closedReason: "ended" }), now)).toBe("6 món · hết giờ");
  });
});

describe("actionsOf (HV29)", () => {
  it("nháp sửa được, đã lên lịch và đang mở chỉ thêm suất, đóng sớm; đã đóng chỉ xem", () => {
    expect(actionsOf("draft", true)).toEqual({ edit: true, schedule: true, addSlots: false, close: false });
    expect(actionsOf("scheduled", true)).toEqual({ edit: false, schedule: false, addSlots: true, close: true });
    expect(actionsOf("open", true)).toEqual({ edit: false, schedule: false, addSlots: true, close: true });
    expect(actionsOf("closed", true)).toEqual({ edit: false, schedule: false, addSlots: false, close: false });
  });
  it("không có quyền quản lý → không nút ghi nào", () => {
    for (const s of ["draft", "scheduled", "open", "closed"] as const) {
      expect(Object.values(actionsOf(s, false)).every((v) => !v)).toBe(true);
    }
  });
});

describe("validateSale / toSaleInput", () => {
  const ok = { name: "Đêm 12.12", start: "2026-12-12T00:00", end: "2026-12-12T02:00" };
  it("hợp lệ → thân gửi theo giờ VN (UTC+7)", () => {
    expect(validateSale(ok)).toEqual({});
    expect(toSaleInput(ok)).toEqual({
      name: "Đêm 12.12",
      startsAt: "2026-12-11T17:00:00.000Z",
      endsAt: "2026-12-11T19:00:00.000Z",
    });
  });
  it("tên trống, quá 120 ký tự, giờ đóng không sau giờ mở, dài quá 7 ngày", () => {
    expect(validateSale({ ...ok, name: "  " }).name).toBe("Nhập tên đợt");
    expect(validateSale({ ...ok, name: "a".repeat(121) }).name).toBe("Tên tối đa 120 ký tự");
    expect(validateSale({ ...ok, name: "Đ".repeat(120) }).name).toBeUndefined();
    expect(validateSale({ ...ok, end: "2026-12-11T23:00" }).end).toBe("Giờ đóng phải sau giờ mở.");
    expect(validateSale({ ...ok, end: "2026-12-12T00:00" }).end).toBe("Giờ đóng phải sau giờ mở.");
    expect(validateSale({ ...ok, end: "2026-12-20T02:00" }).end).toBe("Đợt dài tối đa 7 ngày (đang là 8 ngày 2 giờ).");
    expect(validateSale({ ...ok, end: "2026-12-19T00:00" }).end).toBeUndefined();
    expect(toSaleInput({ ...ok, end: "2026-12-11T23:00" })).toBeNull();
  });
  it("chưa chọn giờ", () => {
    expect(validateSale({ ...ok, start: "" }).start).toBe("Chọn giờ mở");
    expect(validateSale({ ...ok, end: "" }).end).toBe("Chọn giờ đóng");
  });
});

describe("parseMoney", () => {
  it("số nguyên đồng, chấp nhận dấu chấm ngăn nghìn", () => {
    expect(parseMoney("199.000")).toBe(199000);
    expect(parseMoney(" 199000 ")).toBe(199000);
    expect(parseMoney("199,5")).toBeNull();
    expect(parseMoney("")).toBeNull();
    expect(parseMoney("abc")).toBeNull();
    expect(parseMoney("0")).toBe(0);
  });
});

describe("validateItem / toItemInput (HV31)", () => {
  const ok = { price: "199.000", qty: "200", limit: "1", hold: "10" };
  it("hợp lệ", () => {
    expect(validateItem(ok, 690_000)).toEqual({});
    expect(toItemInput(ok, 690_000, "v1")).toEqual({
      variantId: "v1",
      salePrice: 199_000,
      quantity: 200,
      perCustomerLimit: 1,
      holdMinutes: 10,
    });
  });
  it("giá flash không thấp hơn giá thường bị chặn", () => {
    expect(validateItem({ ...ok, price: "690.000" }, 690_000).price).toBe(
      "Giá flash phải thấp hơn giá bán thường 690.000 ₫.",
    );
    expect(validateItem({ ...ok, price: "689.999" }, 690_000).price).toBeUndefined();
    expect(validateItem({ ...ok, price: "0" }, 690_000).price).toBe("Giá flash phải lớn hơn 0");
    expect(validateItem({ ...ok, price: "" }, 690_000).price).toBe("Nhập giá flash");
  });
  it("số suất 1–20.000, mỗi khách 1–10, hạn giữ 5–30", () => {
    expect(validateItem({ ...ok, qty: "0" }, 1).qty).toBe("Số suất từ 1 đến 20.000");
    expect(validateItem({ ...ok, qty: "20001" }, 1).qty).toBe("Số suất từ 1 đến 20.000");
    expect(validateItem({ ...ok, qty: "20000" }, 1).qty).toBeUndefined();
    expect(validateItem({ ...ok, qty: "1,5" }, 1).qty).toBe("Số suất từ 1 đến 20.000");
    expect(validateItem({ ...ok, limit: "11" }, 1).limit).toBe("Mỗi khách từ 1 đến 10");
    expect(validateItem({ ...ok, limit: "10" }, 1).limit).toBeUndefined();
    expect(validateItem({ ...ok, hold: "4" }, 1).hold).toBe("Hạn giữ từ 5 đến 30 phút");
    expect(validateItem({ ...ok, hold: "31" }, 1).hold).toBe("Hạn giữ từ 5 đến 30 phút");
    expect(validateItem({ ...ok, hold: "30" }, 1).hold).toBeUndefined();
  });
  it("để trống mỗi khách, hạn giữ → mặc định 1 và 10", () => {
    expect(validateItem({ ...ok, limit: "", hold: "" }, 690_000)).toEqual({});
    expect(toItemInput({ ...ok, limit: "", hold: "" }, 690_000, "v1")).toMatchObject({
      perCustomerLimit: 1,
      holdMinutes: 10,
    });
  });
});

describe("scheduleSummary (HV27)", () => {
  it("tổng suất giữ, ba món đầu nêu riêng, phần còn lại gộp", () => {
    const items = [item("A", 300), item("B", 140), item("C", 96), item("D", 100), item("E", 100), item("F", 22)];
    const s = scheduleSummary(items);
    expect(s.total).toBe(758);
    expect(s.count).toBe(6);
    expect(s.top.map((l) => [l.sku, l.quantity])).toEqual([
      ["A", 300],
      ["B", 140],
      ["C", 96],
    ]);
    expect(s.rest).toEqual({ count: 3, quantity: 222 });
  });
  it("ít hơn bốn món → nêu hết, không dòng 'và N món khác'", () => {
    const s = scheduleSummary([item("A", 5), item("B", 7)]);
    expect(s.rest).toBeNull();
    expect(s.top).toHaveLength(2);
    const four = scheduleSummary([item("A", 1), item("B", 1), item("C", 1), item("D", 4)]);
    expect(four.rest).toEqual({ count: 1, quantity: 4 });
  });
});

describe("shortagesOf (HV28)", () => {
  const err = (details: Record<string, unknown>) => new ApiError({ status: 409, code: "insufficient_stock", details });
  it("đọc đủ mọi mã thiếu từ thân lỗi, thêm cột thiếu", () => {
    const e = err({
      shortages: [
        { variantId: "v1", sku: "SM0412-TRANG-S", need: 150, sellable: 96 },
        { variantId: "v2", sku: "QA0920-DEN-S", need: 100, sellable: 72 },
      ],
    });
    expect(shortagesOf(e)).toEqual([
      { variantId: "v1", sku: "SM0412-TRANG-S", need: 150, sellable: 96, missing: 54 },
      { variantId: "v2", sku: "QA0920-DEN-S", need: 100, sellable: 72, missing: 28 },
    ]);
  });
  it("không phải insufficient_stock, hay thân lạ → rỗng", () => {
    expect(shortagesOf(new ApiError({ status: 422, code: "flash_overlap", details: {} }))).toEqual([]);
    expect(shortagesOf(err({ shortages: "x" }))).toEqual([]);
    expect(shortagesOf(err({ shortages: [{ sku: 1 }] }))).toEqual([]);
    expect(shortagesOf(new Error("x"))).toEqual([]);
  });
});

describe("liveTotals / barPercents", () => {
  const live: FlashLive = {
    items: [
      { itemId: "a", quantity: 300, available: 26, held: 14, sold: 260, released: 0, buyers: 274 },
      { itemId: "b", quantity: 400, available: 0, held: 6, sold: 394, released: 0, buyers: 322 },
    ],
  };
  it("cộng theo món, người mua cộng theo món", () => {
    expect(liveTotals(live)).toEqual({ quantity: 700, available: 26, held: 20, sold: 654, released: 0, buyers: 596 });
    expect(liveTotals({ items: [] })).toEqual({ quantity: 0, available: 0, held: 0, sold: 0, released: 0, buyers: 0 });
  });
  it("thanh đã bán, đang giữ theo phần trăm của số suất; số suất 0 không chia cho 0", () => {
    expect(
      barPercents({ itemId: "b", quantity: 400, available: 0, held: 6, sold: 394, released: 0, buyers: 322 }),
    ).toEqual({ sold: 98.5, held: 1.5 });
    expect(barPercents({ itemId: "z", quantity: 0, available: 0, held: 0, sold: 0, released: 0, buyers: 0 })).toEqual({
      sold: 0,
      held: 0,
    });
  });
});

describe("refreshMs / longTime", () => {
  it("đang mở làm mới 5 giây, trạng thái khác 20 giây", () => {
    expect(refreshMs("open")).toBe(5000);
    for (const s of ["draft", "scheduled", "closed", undefined] as const) expect(refreshMs(s)).toBe(20_000);
  });
  it("giờ VN có năm", () => {
    expect(longTime("2026-12-11T17:00:00Z")).toBe("00:00 12/12/2026");
    expect(longTime("x")).toBe("");
  });
});
