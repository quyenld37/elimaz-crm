// @vitest-environment jsdom
// Khối "Tiền đã thu" trên chi tiết đơn (kế hoạch M3 Task 138, TD10; mockup docs/mockups/m3/tien-tren-don.html trạng
// thái A; HV33): phiếu thu, cọc; COD tự tính lại trước → sau; đơn đã gửi hãng thì nút mờ kèm câu; 409
// cod_locked_by_shipment hiện câu; không quyền thì không có khối; đơn đổi bị kho nhận thiếu → cảnh báo (Task 116).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Payment } from "../../api";
import { O1, R1, grantsOf, installFake, order, renderAt, stubDialog, type Fake } from "../don/donTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.grants = grantsOf("cash.payments.record");
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function pay(p: Partial<Payment> = {}): Payment {
  return {
    id: "0195f3b2-0000-7000-8000-0000000pa001",
    orderId: O1,
    method: "deposit",
    amount: 200_000,
    reference: "FT26325118842",
    receivedAt: "2026-11-21T03:25:00Z",
    createdAt: "2026-11-21T03:25:00Z",
    actor: { id: "u2", name: "Lê Văn Đức" },
    ...p,
  };
}

const box = () => screen.findByRole("region", { name: "Tiền đã thu" });

describe("Khối Tiền đã thu", () => {
  it("phiếu đã ghi, tổng đơn, đã thu, COD; ghi cọc hiện COD trước → sau và gửi đúng thân", async () => {
    fake.orders.set(
      O1,
      order({
        status: "confirmed",
        label: "confirmed",
        grandTotal: 2_160_000,
        prepaidTotal: 200_000,
        codAmount: 1_960_000,
      }),
    );
    fake.payments = [pay()];
    renderAt(`/don/${O1}`);
    const b = await box();
    await waitFor(() => {
      expect(b.textContent).toContain("FT26325118842");
    });
    expect(b.textContent).toContain("Cọc");
    expect(b.textContent).toContain("Lê Văn Đức");
    expect(within(b).getByLabelText("Tổng đơn").textContent).toContain("2.160.000 ₫");
    expect(within(b).getByLabelText("Đã thu").textContent).toContain("200.000 ₫");
    expect(within(b).getByLabelText("Thu hộ (COD)").textContent).toContain("1.960.000 ₫");
    fireEvent.click(within(b).getByRole("button", { name: "Ghi phiếu thu" }));
    const dlg = await screen.findByRole("dialog", { name: "Ghi phiếu thu · DH0001235" });
    fireEvent.click(within(dlg).getByRole("radio", { name: "Cọc" }));
    const send = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Ghi phiếu thu" });
    expect(send.disabled).toBe(true);
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền" }), { target: { value: "500.000" } });
    expect(dlg.textContent).toContain("COD của đơn tự tính lại: 1.960.000 ₫ → 1.460.000 ₫.");
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Mã giao dịch / ghi chú" }), {
      target: { value: "FT1" },
    });
    fireEvent.click(send);
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/orders/${O1}/payments`,
        body: { method: "deposit", amount: 500_000, reference: "FT1" },
      });
    });
    expect(fake.writes.at(-1)?.key).toBeTruthy();
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Ghi phiếu thu · DH0001235" })).toBeNull();
    });
  });

  it("vượt số còn phải thu: nút khóa kèm câu", async () => {
    fake.orders.set(O1, order({ status: "new", grandTotal: 1_860_000, prepaidTotal: 0 }));
    renderAt(`/don/${O1}`);
    fireEvent.click(within(await box()).getByRole("button", { name: "Ghi phiếu thu" }));
    const dlg = await screen.findByRole("dialog", { name: "Ghi phiếu thu · DH0001235" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền" }), { target: { value: "2000000" } });
    expect(within(dlg).getByText("Vượt số còn phải thu 1.860.000 ₫")).toBeTruthy();
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Ghi phiếu thu" }).disabled).toBe(true);
  });

  it("đơn đã gửi hãng: nút mờ kèm 'Đơn đã gửi hãng, không đổi được tiền thu hộ'", async () => {
    fake.orders.set(
      O1,
      order({ status: "confirmed", label: "confirmed", currentShipmentId: "0195f3b2-0000-7000-8000-00000000af01" }),
    );
    renderAt(`/don/${O1}`);
    const b = await box();
    await waitFor(() => {
      expect(within(b).getByRole<HTMLButtonElement>("button", { name: "Ghi phiếu thu" }).disabled).toBe(true);
    });
    expect(b.textContent).toContain(
      "Đơn đã gửi hãng, không đổi được tiền thu hộ. Muốn đổi COD: hủy vận đơn rồi gửi lại.",
    );
  });

  it("máy chủ trả 409 cod_locked_by_shipment (vừa gửi hãng): câu báo trong hộp, không đóng", async () => {
    fake.orders.set(O1, order({ status: "confirmed", label: "confirmed" }));
    renderAt(`/don/${O1}`);
    fireEvent.click(within(await box()).getByRole("button", { name: "Ghi phiếu thu" }));
    const dlg = await screen.findByRole("dialog", { name: "Ghi phiếu thu · DH0001235" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền" }), { target: { value: "100000" } });
    fake.replies.push({ status: 409, body: { error: "cod_locked_by_shipment", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi phiếu thu" }));
    expect(await within(dlg).findByText(/Đơn đã gửi hãng, không đổi được tiền thu hộ/)).toBeTruthy();
    expect(screen.getByRole("dialog", { name: "Ghi phiếu thu · DH0001235" })).toBeTruthy();
  });

  it("không có cash.payments.record: không có khối Tiền đã thu", async () => {
    fake.grants = grantsOf();
    fake.orders.set(O1, order());
    renderAt(`/don/${O1}`);
    await screen.findByRole("region", { name: "Sản phẩm" });
    await waitFor(() => {
      expect(screen.getByRole("group", { name: "Thao tác đơn" }).getAttribute("data-ready")).toBe("true");
    });
    expect(screen.queryByRole("region", { name: "Tiền đã thu" })).toBeNull();
  });

  it("đơn đổi: phiếu cấn trừ hiện 'Cấn trừ món trả'; kho nhận thiếu → cảnh báo chênh lệch và cách xử lý", async () => {
    fake.orders.set(
      O1,
      order({
        status: "confirmed",
        label: "confirmed",
        orderType: "exchange",
        replacedReturnId: R1,
        grandTotal: 180_000,
        prepaidTotal: 177_049,
        codAmount: 2_951,
      }),
    );
    fake.payments = [
      pay({ method: "exchange_credit", amount: 177_049, reference: `exchange:${R1}`, actor: undefined }),
    ];
    fake.returns.set(R1, {
      id: R1,
      code: "TH0000031",
      orderId: "0195f3b2-0000-7000-8000-00000000d0ff",
      kind: "exchange",
      status: "restocked",
      refundAmount: 0,
      items: [{ id: "ri", orderItemId: "x", qty: 1, refundAmount: 0, condition: "missing", missing: 1 }],
      createdAt: "2026-11-20T00:00:00Z",
      version: 3,
    });
    renderAt(`/don/${O1}`);
    const b = await box();
    await waitFor(() => {
      expect(b.textContent).toContain("Cấn trừ món trả");
    });
    expect(
      await within(b).findByText(
        /Kho nhận thiếu hàng của phiếu đổi TH0000031: đã cấn trừ 177.049 ₫ nhưng tiền hoàn cuối chỉ 0 ₫ — khách được cấn nhiều hơn 177.049 ₫/,
      ),
    ).toBeTruthy();
  });
});
