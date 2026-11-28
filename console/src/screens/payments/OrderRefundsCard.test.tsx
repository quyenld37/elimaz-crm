// @vitest-environment jsdom
// Thẻ Nghĩa vụ hoàn trên chi tiết đơn (Task 524; mockup docs/mockups/m3/tien-tren-don.html trạng thái B): đọc
// listRefundObligations?orderId của đúng đơn; nghĩa vụ mở "Chưa chi — chưa tính là đã hoàn", Ghi phiếu chi gửi đúng
// nghĩa vụ; Kế toán thấy Miễn hoàn mờ kèm lý do; đã chi đủ không có nút; không có nghĩa vụ / không quyền → không thẻ;
// lỗi đọc hiện câu, không giả rỗng.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RefundObligation } from "../../api";
import { O1, O2, grantsOf, installFake, order, renderAt, stubDialog, type Fake } from "../don/donTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.grants = grantsOf("cash.cod.read", "cash.refunds.record");
  fake.orders.set(O1, order({ status: "cancelled", label: "cancelled" }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const OB1 = "0195f3b2-0000-7000-8000-0000000ob001";

function ob(p: Partial<RefundObligation> = {}): RefundObligation {
  return {
    id: OB1,
    orderId: O1,
    orderCode: "DH0001235",
    source: "order_cancel",
    amount: 200_000,
    paidAmount: 0,
    status: "open",
    createdAt: "2026-11-22T02:00:00Z",
    version: 1,
    ...p,
  };
}

const card = () => screen.findByRole("region", { name: "Nghĩa vụ hoàn" });

describe("Thẻ Nghĩa vụ hoàn trên đơn", () => {
  it("chỉ nghĩa vụ của đơn này; mở: chưa chi, Ghi phiếu chi gửi đúng nghĩa vụ; Kế toán: Miễn hoàn mờ", async () => {
    fake.obligations = [ob(), ob({ id: "0195f3b2-0000-7000-8000-0000000ob002", orderId: O2, orderCode: "DH0009999" })];
    renderAt(`/don/${O1}`);
    const c = await card();
    expect(fake.obligationCalls.at(-1)?.get("orderId")).toBe(O1);
    expect(c.textContent).toContain("Nghĩa vụ hoàn · DH0001235");
    expect(c.textContent).not.toContain("DH0009999");
    expect(c.textContent).toContain("Hủy đơn có cọc");
    expect(c.textContent).toContain("Chưa chi — chưa tính là đã hoàn.");
    expect(c.textContent).toContain("Còn phải hoàn 200.000 ₫");
    await waitFor(() => {
      expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Miễn hoàn" }).disabled).toBe(true);
    });
    expect(c.textContent).toContain("Chỉ Chủ được miễn hoàn");
    fireEvent.click(within(c).getByRole("button", { name: "Ghi phiếu chi" }));
    const dlg = await screen.findByRole("dialog", { name: "Chi hoàn · DH0001235" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền chi" }), { target: { value: "100.000" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi phiếu chi" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/refund-obligations/${OB1}/payments`,
        body: { amount: 100_000 },
      });
    });
    expect(fake.writes.at(-1)?.key).toBeTruthy();
  });

  it("chi một phần: câu còn nợ khách; đã chi đủ: không nút; Chủ: Miễn hoàn bấm được", async () => {
    fake.grants = grantsOf("cash.cod.read", "cash.refunds.record", "cash.refunds.waive");
    fake.obligations = [
      ob({ paidAmount: 150_000 }),
      ob({
        id: "0195f3b2-0000-7000-8000-0000000ob003",
        source: "exchange",
        status: "paid",
        paidAmount: 27_049,
        amount: 27_049,
      }),
    ];
    renderAt(`/don/${O1}`);
    const c = await card();
    expect(c.textContent).toContain("Đã chi 150.000 ₫, còn nợ khách 50.000 ₫.");
    expect(c.textContent).toContain("Còn phải hoàn 50.000 ₫");
    const paid = within(c).getByLabelText("Nghĩa vụ Đổi hàng (dư tiền món trả)");
    expect(within(paid).queryByRole("button")).toBeNull();
    expect(paid.textContent).toContain("Đã chi đủ");
    await waitFor(() => {
      expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Miễn hoàn" }).disabled).toBe(false);
    });
  });

  it("đơn không có nghĩa vụ: không có thẻ", async () => {
    renderAt(`/don/${O1}`);
    await waitFor(() => {
      expect(fake.obligationCalls).toHaveLength(1);
    });
    expect(screen.queryByRole("region", { name: "Nghĩa vụ hoàn" })).toBeNull();
  });

  it("không có cash.cod.read: không đọc, không thẻ", async () => {
    fake.grants = grantsOf();
    fake.obligations = [ob()];
    renderAt(`/don/${O1}`);
    expect(await screen.findByRole("heading", { name: "Phụ trách, nguồn" })).toBeTruthy();
    expect(fake.obligationCalls).toHaveLength(0);
    expect(screen.queryByRole("region", { name: "Nghĩa vụ hoàn" })).toBeNull();
  });

  it("đọc lỗi: câu lỗi, không vẽ như rỗng", async () => {
    fake.obligations = null;
    renderAt(`/don/${O1}`);
    const c = await card();
    expect(within(c).getByText(/quyền/i)).toBeTruthy();
  });
});
