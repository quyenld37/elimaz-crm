// @vitest-environment jsdom
// Hộp thoại Trả/đổi (mockup docs/mockups/m2/don-chi-tiet.html trạng thái D; HV19): không cho chọn quá số còn trả được,
// tiền hoàn dự kiến khớp máy chủ (pricing.RefundFor), quá số ngày đổi trả chỉ cảnh báo (kế hoạch M2 Task M2-28).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CARE_ONLY,
  I1,
  I2,
  O1,
  R1,
  R2,
  V3,
  grantsOf,
  installFake,
  order,
  orderItem,
  renderAt,
  stubDialog,
  type Fake,
} from "./donTestkit";

let fake: Fake;
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-19T04:00:00Z"));
  stubDialog();
  fake = installFake();
  fake.grants = grantsOf("order.return");
  fake.orders.set(
    O1,
    order({
      status: "delivered",
      label: "delivered",
      editScope: CARE_ONLY,
      items: [
        orderItem(),
        orderItem({
          id: I2,
          variantId: V3,
          sku: "TUI0090-NAU",
          qty: 1,
          unitPrice: 290_000,
          netAmount: 290_000,
          lineDiscount: 0,
        }),
      ],
      returns: [
        { id: R1, code: "TH0000012", kind: "customer_return", status: "requested" },
        { id: R2, code: "TH0000009", kind: "customer_return", status: "restocked" },
      ],
    }),
  );
  const base = { orderId: O1, kind: "customer_return" as const, createdAt: "2026-11-18T00:00:00Z", version: 1 };
  fake.returns.set(R1, {
    ...base,
    id: R1,
    code: "TH0000012",
    status: "requested",
    refundAmount: 1_240_000,
    items: [{ id: "ri1", orderItemId: I1, qty: 2, refundAmount: 1_240_000 }],
  });
  fake.returns.set(R2, {
    ...base,
    id: R2,
    code: "TH0000009",
    status: "restocked",
    refundAmount: 290_000,
    items: [{ id: "ri2", orderItemId: I2, qty: 1, refundAmount: 290_000, condition: "sellable" }],
  });
  fake.timeline = [
    { at: "2026-11-10T03:00:00Z", kind: "status", refId: "h2", summary: { from: "confirmed", to: "delivered" } },
  ];
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function openDialog() {
  renderAt(`/don/${O1}`);
  await screen.findByRole("row", { name: /TH0000009/ });
  fireEvent.click(screen.getByRole("button", { name: "Tạo phiếu trả/đổi" }));
  return screen.findByRole("dialog", { name: "Trả/đổi hàng · DH0001235" });
}

describe("Hộp thoại Trả/đổi", () => {
  it("số còn trả được trừ cả phiếu đang mở; dòng đã trả hết không chọn được; + mờ ở số còn lại", async () => {
    const dlg = await openDialog();
    const rowA = within(dlg).getByRole("row", { name: /DAM0123-BE-M/ });
    expect(within(rowA).getByText("3")).toBeTruthy();
    expect(within(rowA).getByText("2")).toBeTruthy();
    const rowB = within(dlg).getByRole("row", { name: /TUI0090-NAU/ });
    expect(within(rowB).getByText("Đã trả hết")).toBeTruthy();
    expect(within(rowB).getByRole<HTMLInputElement>("checkbox").disabled).toBe(true);
    fireEvent.click(within(rowA).getByRole("checkbox"));
    expect(within(rowA).getByRole<HTMLButtonElement>("button", { name: "Thêm 1" }).disabled).toBe(true);
    expect(within(rowA).getByRole<HTMLInputElement>("textbox", { name: "Số lượng trả DAM0123-BE-M" }).value).toBe("1");
  });

  it("gõ quá số còn trả được: ô báo đỏ, không tạo được phiếu", async () => {
    const dlg = await openDialog();
    const rowA = within(dlg).getByRole("row", { name: /DAM0123-BE-M/ });
    fireEvent.click(within(rowA).getByRole("checkbox"));
    fireEvent.change(within(rowA).getByRole("textbox", { name: "Số lượng trả DAM0123-BE-M" }), {
      target: { value: "2" },
    });
    expect(within(dlg).getByText("Dòng DAM0123-BE-M chỉ còn trả được 1")).toBeTruthy();
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Tạo phiếu trả" }).disabled).toBe(true);
  });

  it("tiền hoàn dự kiến khớp máy chủ (món cuối nhận phần còn lại); tạo phiếu gửi đúng dòng, số, lý do", async () => {
    const dlg = await openDialog();
    expect(within(dlg).getByText(/quá 7 ngày đổi trả của shop/)).toBeTruthy();
    expect(within(dlg).getByText(/9 ngày trước/)).toBeTruthy();
    const rowA = within(dlg).getByRole("row", { name: /DAM0123-BE-M/ });
    fireEvent.click(within(rowA).getByRole("checkbox"));
    expect(within(rowA).getByText("620.000 ₫")).toBeTruthy();
    expect(within(dlg).getByText(/Tổng:/).textContent).toContain("620.000 ₫");
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do trả" }), { target: { value: "Không vừa size" } });
    fake.replies.push({
      status: 201,
      body: {
        id: "0195f3b2-0000-7000-8000-00000000a103",
        code: "TH0000013",
        orderId: O1,
        kind: "customer_return",
        status: "requested",
        refundAmount: 620_000,
        items: [{ id: "ri3", orderItemId: I1, qty: 1, refundAmount: 620_000 }],
        createdAt: "2026-11-19T04:00:00Z",
        version: 1,
      },
    });
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo phiếu trả" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/orders/${O1}/returns`,
        body: { kind: "customer_return", lines: [{ orderItemId: I1, qty: 1 }], reason: "Không vừa size" },
      });
    });
    expect(await screen.findByText("Đã tạo phiếu TH0000013, hoàn dự kiến 620.000 ₫.")).toBeTruthy();
  });

  it("trong số ngày đổi trả: không cảnh báo", async () => {
    fake.returnWarningDays = 10;
    const dlg = await openDialog();
    expect(within(dlg).queryByText(/quá 10 ngày/)).toBeNull();
  });

  it("đổi hàng: cần chọn hàng đổi mới tạo được phiếu", async () => {
    const dlg = await openDialog();
    fireEvent.click(within(dlg).getByRole("radio", { name: "Đổi hàng" }));
    fireEvent.click(within(within(dlg).getByRole("row", { name: /DAM0123-BE-M/ })).getByRole("checkbox"));
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Tạo phiếu đổi" }).disabled).toBe(true);
    expect(within(dlg).getByText("Chọn hàng đổi cho khách")).toBeTruthy();
  });
});
