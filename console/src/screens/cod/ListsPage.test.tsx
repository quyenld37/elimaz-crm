// @vitest-environment jsdom
// Lệch đối soát và Quá hạn chưa có tiền (mockup docs/mockups/m3/doi-soat-cod.html trạng thái E, F; HV32; TI05).
import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ORDER, S_MATCHED, SHIP, grantsOf, installFake, renderAt, type Fake } from "./codTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Lệch đối soát", () => {
  it("dòng thiếu còn mở, hãng chuyển dư, chuyển tiền rồi báo hoàn; Điều chỉnh mở đơn; không có nút bỏ qua", async () => {
    fake.discrepancies = [
      {
        kind: "statement_line",
        statementId: S_MATCHED,
        lineNo: 12,
        shipmentId: SHIP,
        orderId: ORDER,
        orderCode: "DH0001101",
        carrierKey: "vtp",
        trackingCode: "1234567890001",
        codAmount: 1_200_000,
        expectedAmount: 1_290_000,
        diff: -90_000,
        matchStatus: "amount_mismatch",
      },
      {
        kind: "statement_line",
        statementId: S_MATCHED,
        lineNo: 140,
        shipmentId: SHIP,
        orderId: ORDER,
        orderCode: "DH0001133",
        carrierKey: "vtp",
        trackingCode: "1234567890102",
        codAmount: 490_000,
        expectedAmount: 390_000,
        diff: 100_000,
        matchStatus: "amount_mismatch",
      },
      {
        kind: "remitted_then_returned",
        shipmentId: SHIP,
        orderId: ORDER,
        orderCode: "DH0000987",
        carrierKey: "best",
        trackingCode: "BE0098800000VN",
        codAmount: 1_290_000,
      },
      {
        kind: "statement_line",
        statementId: S_MATCHED,
        lineNo: 88,
        carrierKey: "vtp",
        trackingCode: "0987654321777",
        codAmount: 520_000,
        matchStatus: "external",
      },
    ];
    renderAt("/cod/discrepancies");
    const t = await screen.findByRole("table");
    const rows = within(t).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(4);
    const [a, b, c, d] = rows;
    if (a === undefined || b === undefined || c === undefined || d === undefined) throw new Error("thiếu dòng");
    expect(within(a).getByRole("link", { name: "DH0001101" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    expect(within(a).getByText("Viettel Post · 1234567890001")).toBeTruthy();
    expect(within(a).getByText(/^Phần thiếu còn mở/)).toBeTruthy();
    expect(within(a).getByRole("link", { name: "bảng kê dòng 12" }).getAttribute("href")).toBe(
      `/cod/statements/${S_MATCHED}`,
    );
    expect(within(a).getByText("1.290.000 ₫")).toBeTruthy();
    expect(within(a).getByText("−90.000 ₫").className).toContain("err-text");
    expect(within(a).getByRole("link", { name: "Điều chỉnh" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    expect(within(b).getByText(/^Hãng chuyển dư/)).toBeTruthy();
    expect(within(b).getByText("+100.000 ₫")).toBeTruthy();
    expect(within(c).getByText("Hãng chuyển tiền rồi báo hoàn")).toBeTruthy();
    expect(within(c).getByText("Best Express · BE0098800000VN")).toBeTruthy();
    expect(within(d).getByText(/Ngoài hệ thống · không tính vào số liệu/)).toBeTruthy();
    expect(within(d).queryByRole("link", { name: "Điều chỉnh" })).toBeNull();
    expect(screen.queryByRole("button", { name: /bỏ qua/i })).toBeNull();
    expect(
      screen.getByText('Không có nút "bỏ qua": lệch chỉ đóng khi có bảng kê bù hoặc điều chỉnh COD có lý do.'),
    ).toBeTruthy();
  });

  it("không có quyền điều chỉnh: không có nút Điều chỉnh; 0 dòng: trạng thái rỗng", async () => {
    fake.grants = grantsOf();
    fake.discrepancies = [
      {
        kind: "open_remainder",
        shipmentId: SHIP,
        orderId: ORDER,
        orderCode: "DH1",
        carrierKey: "spx",
        trackingCode: "X",
        codAmount: 1,
        expectedAmount: 5,
        diff: -5,
      },
    ];
    renderAt("/cod/discrepancies");
    expect(await screen.findByText("DH1")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Điều chỉnh" })).toBeNull();
    cleanup();
    fake.discrepancies = [];
    renderAt("/cod/discrepancies");
    expect(await screen.findByText("Không có lệch đối soát")).toBeTruthy();
  });
});

describe("Quá hạn chưa có tiền", () => {
  it("đơn, hãng, ngày giao kèm số ngày đỏ, hãng đang giữ 9 chữ số", async () => {
    fake.overdue = [
      {
        shipmentId: SHIP,
        orderId: ORDER,
        orderCode: "DH0001001",
        carrierKey: "vtp",
        trackingCode: "1234567812345",
        deliveredAt: "2026-11-03T05:00:00Z",
        carrierHolding: 112_450_000,
        days: 19,
      },
    ];
    renderAt("/cod/overdue");
    const row = (await screen.findByText("DH0001001")).closest("tr");
    if (row === null) throw new Error("không có dòng");
    expect(within(row).getByText("Viettel Post · 1234567812345")).toBeTruthy();
    expect(within(row).getByText("19 ngày").className).toContain("err-text");
    expect(row.textContent).toContain("03/11 · 19 ngày");
    expect(within(row).getByText("112.450.000 ₫").className).toContain("money");
  });

  it("0 dòng: câu rỗng của mockup; API lỗi: hộp lỗi", async () => {
    renderAt("/cod/overdue");
    expect(await screen.findByText("Không có đơn quá hạn chưa có tiền")).toBeTruthy();
    expect(screen.getByText("Đơn giao quá 14 ngày mà hãng chưa chuyển tiền sẽ vào đây.")).toBeTruthy();
    cleanup();
    fake.failReads = [/overdue$/];
    renderAt("/cod/overdue");
    expect(await screen.findByRole("alert")).toBeTruthy();
  });
});
