// @vitest-environment jsdom
// Hộp thoại Trả/đổi (mockup docs/mockups/m2/don-chi-tiet.html trạng thái D; HV19): không cho chọn quá số còn trả được,
// tiền hoàn dự kiến khớp máy chủ (pricing.RefundFor), quá số ngày đổi trả chỉ cảnh báo (kế hoạch M2 Task M2-28).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const fail = (): never => {
  throw new Error("thiếu phiếu");
};
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

/** Hộp thoại ở chế độ Đổi hàng (có order.create), đã chọn 1 món DAM0123-BE-M để trả. */
async function openExchange() {
  fake.grants = grantsOf("order.return", "order.create");
  const dlg = await openDialog();
  fireEvent.click(within(dlg).getByRole("radio", { name: "Đổi hàng" }));
  fireEvent.click(within(within(dlg).getByRole("row", { name: /DAM0123-BE-M/ })).getByRole("checkbox"));
  return dlg;
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
    fake.grants = grantsOf("order.return", "order.create");
    const dlg = await openDialog();
    expect(within(dlg).queryByText("Đổi hàng cần quyền lên đơn ở kho của đơn này.")).toBeNull();
    fireEvent.click(within(dlg).getByRole("radio", { name: "Đổi hàng" }));
    fireEvent.click(within(within(dlg).getByRole("row", { name: /DAM0123-BE-M/ })).getByRole("checkbox"));
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Tạo phiếu đổi" }).disabled).toBe(true);
    expect(within(dlg).getByText("Chọn hàng đổi cho khách")).toBeTruthy();
  });

  it("chỉ có order.return: khóa Đổi hàng, vẫn tạo phiếu khách trả", async () => {
    const dlg = await openDialog();
    expect(within(dlg).getByRole<HTMLButtonElement>("radio", { name: "Đổi hàng" }).disabled).toBe(true);
    expect(within(dlg).getByText("Đổi hàng cần quyền lên đơn ở kho của đơn này.")).toBeTruthy();
    expect(within(dlg).getByRole<HTMLButtonElement>("radio", { name: "Khách trả hàng" }).disabled).toBe(false);
  });

  it("order.create theo kho khác: máy chủ trả 403, hộp thoại hiện lỗi, không đóng", async () => {
    const dlg = await openExchange();
    fake.replies.push({ status: 403, body: { error: "forbidden", request_id: "t" } });
    fake.variantHits = [
      {
        variantId: V3,
        productId: "p9",
        sku: "DAM0999-DO-M",
        barcode: "",
        productCode: "DAM0999",
        productName: "Đầm đỏ",
        color: "Đỏ",
        size: "M",
        price: 450_000,
        sellable: true,
      },
    ];
    const box = within(dlg).getByRole("combobox", { name: "Thêm sản phẩm theo mã, tên hoặc barcode" });
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: "DAM0999" } });
    fireEvent.click(await within(dlg).findByRole("option", { name: /DAM0999-DO-M/ }));
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo phiếu đổi" }));
    expect(await within(dlg).findByText("Bạn chưa được cấp quyền làm việc này")).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({
      body: { kind: "exchange", replacement: { items: [{ variantId: V3, qty: 1 }] } },
    });
    expect(screen.getByRole("dialog", { name: "Trả/đổi hàng · DH0001235" })).toBeTruthy();
  });

  it("HV27: cột Còn được trả, hộp 'tạo phiếu là đã duyệt' kèm tiền nghĩa vụ hoàn dự kiến", async () => {
    const dlg = await openDialog();
    expect(within(dlg).getByRole("columnheader", { name: "Đã/đang trả" })).toBeTruthy();
    expect(within(dlg).getByRole("columnheader", { name: "Còn được trả" })).toBeTruthy();
    const rowA = within(dlg).getByRole("row", { name: /DAM0123-BE-M/ });
    fireEvent.click(within(rowA).getByRole("checkbox"));
    expect(dlg.textContent).toContain(
      "Tạo phiếu là đã duyệt. Tồn chưa tăng, chưa hoàn tiền: kho bấm nhận hàng thì tồn mới tăng và mở nghĩa vụ hoàn 620.000 ₫ cho kế toán chi.",
    );
  });

  it("dòng hết số trả vì phiếu đang mở: ghi mã phiếu đang mở", async () => {
    fake.returns.set(R1, {
      ...(fake.returns.get(R1) ?? fail()),
      items: [{ id: "ri1", orderItemId: I1, qty: 3, refundAmount: 1_860_000 }],
    });
    const dlg = await openDialog();
    const rowA = within(dlg).getByRole("row", { name: /DAM0123-BE-M/ });
    expect(rowA.textContent).toContain("Đang có phiếu TH0000012");
  });
});

/** Món mới tìm được trong hộp đổi hàng: giá price. */
function hit(price: number) {
  return {
    variantId: V3,
    productId: "p9",
    sku: "AO0105-TRANG-M",
    barcode: "",
    productCode: "AO0105",
    productName: "Áo sơ mi lụa",
    color: "Trắng",
    size: "M",
    price,
    sellable: true,
  };
}

async function pickSwap(dlg: HTMLElement) {
  const box = within(dlg).getByRole("combobox", { name: "Thêm sản phẩm theo mã, tên hoặc barcode" });
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: "AO0105" } });
  fireEvent.click(await within(dlg).findByRole("option", { name: /AO0105-TRANG-M/ }));
}

describe("Đổi hàng: tiền cấn trừ của đơn thay thế (TD06, HV28)", () => {
  it("món mới 180.000, món trả 177.049 → 'Cấn trừ 177.049 ₫ — khách trả thêm 2.951 ₫'", async () => {
    fake.grants = grantsOf("order.return", "order.create");
    fake.orders.set(
      O1,
      order({
        status: "delivered",
        label: "delivered",
        editScope: CARE_ONLY,
        items: [
          orderItem({ sku: "AO0105-TRANG-S", qty: 1, unitPrice: 190_000, netAmount: 177_049, lineDiscount: 12_951 }),
        ],
        returns: [],
      }),
    );
    renderAt(`/don/${O1}`);
    fireEvent.click(await screen.findByRole("button", { name: "Tạo phiếu trả/đổi" }));
    const dlg = await screen.findByRole("dialog", { name: "Trả/đổi hàng · DH0001235" });
    fireEvent.click(within(dlg).getByRole("radio", { name: "Đổi hàng" }));
    fireEvent.click(within(within(dlg).getByRole("row", { name: /AO0105-TRANG-S/ })).getByRole("checkbox"));
    fake.variantHits = [hit(180_000)];
    await pickSwap(dlg);
    const sum = within(dlg).getByRole("region", { name: "Tiền đơn đổi" });
    expect(within(sum).getByLabelText("Món mới").textContent).toContain("180.000 ₫");
    expect(within(sum).getByLabelText("Cấn trừ món trả").textContent).toContain("−177.049 ₫");
    expect(within(sum).getByLabelText("Phí ship đơn đổi").textContent).toContain("0 ₫");
    expect(within(sum).getByLabelText("COD đơn mới").textContent).toContain("2.951 ₫");
    expect(dlg.textContent).toContain("Cấn trừ 177.049 ₫ — khách trả thêm 2.951 ₫.");
  });

  it("món trả đắt hơn: COD 0, phần dư thành nghĩa vụ hoàn khi kho nhận", async () => {
    const dlg = await openExchange();
    fake.variantHits = [hit(450_000)];
    await pickSwap(dlg);
    const sum = within(dlg).getByRole("region", { name: "Tiền đơn đổi" });
    expect(within(sum).getByLabelText("COD đơn mới").textContent).toContain("0 ₫");
    expect(within(sum).getByLabelText("Cấn trừ món trả").textContent).toContain("−450.000 ₫");
    expect(dlg.textContent).toContain(
      "Dư 170.000 ₫ cho khách. Thành nghĩa vụ hoàn khi kho đã nhận món trả; kế toán chi hoặc Chủ miễn kèm lý do.",
    );
    expect(dlg.textContent).toContain("COD đơn mới không bao giờ âm.");
  });
});
