// @vitest-environment jsdom
// Phiếu nhặt hàng (kế hoạch M3 Task 136; mockup docs/mockups/m3/kho.html trạng thái C; HV25): gom theo mẫu rồi kê
// theo đơn, không địa chỉ, SĐT; "In" gọi hộp in của trình duyệt.
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WH, installFake, renderAt, shipId, type Fake } from "./warehouseTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.picking = {
    lines: [
      {
        sku: "AO0101-DEN-M",
        name: "Áo thun cổ tròn basic",
        variant: "Đen · M",
        qty: 21,
        orderCodes: ["DH0001240", "DH0001242"],
      },
      {
        sku: "DAM0123-BE-M",
        name: "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa thủ công tay phồng nhẹ dáng xòe xếp ly nhiều tầng",
        variant: "Be · M",
        qty: 44,
        orderCodes: ["DH0001242"],
      },
    ],
    orders: [
      {
        code: "DH0001240",
        shipmentCode: "E1",
        trackingCode: "1234567890123",
        carrier: "vtp",
        itemCount: 3,
        codAmount: 1_960_000,
      },
      {
        code: "DH0001242",
        shipmentCode: "E2",
        trackingCode: "1234567890125",
        carrier: "vtp",
        itemCount: 64,
        codAmount: 112_450_000,
      },
      { code: "DH0001225", shipmentCode: "E261121R2W8KN", carrier: "self", itemCount: 1, codAmount: 450_000 },
    ],
  };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("gom theo mẫu, kê theo đơn (tự giao hiện mã vận đơn của shop); đầu phiếu có kho, số đơn, số món; In gọi print", async () => {
  const print = vi.fn();
  vi.stubGlobal("print", print);
  renderAt(`/warehouse/picking-list?shipmentIds=${shipId(40)}&shipmentIds=${shipId(42)}&warehouseId=${WH}`);
  const paper = await screen.findByLabelText("Phiếu nhặt hàng");
  expect(fake.pickingCalls[0]?.getAll("shipmentIds")).toEqual([shipId(40), shipId(42)]);
  expect(await within(paper).findByRole("heading", { name: "Phiếu nhặt hàng · Kho chính" })).toBeTruthy();
  expect(within(paper).getByText(/· 3 đơn · 68 món$/)).toBeTruthy();
  const sku = within(paper).getByRole("table", { name: "Gom theo mẫu" });
  expect(
    within(sku).getByRole("row", { name: /AO0101-DEN-M Áo thun cổ tròn basic · Đen · M 21 DH0001240, DH0001242/ }),
  ).toBeTruthy();
  const byOrder = within(paper).getByRole("table", { name: "Kê theo đơn" });
  expect(within(byOrder).getByRole("row", { name: /DH0001225 E261121R2W8KN Tự giao 1 450.000 ₫/ })).toBeTruthy();
  expect(
    within(byOrder).getByRole("row", { name: /DH0001242 1234567890125 Viettel Post 64 112.450.000 ₫/ }),
  ).toBeTruthy();
  expect(paper.textContent).not.toMatch(/\+84|Lê Lợi/);
  fireEvent.click(within(paper).getByRole("button", { name: "In" }));
  expect(print).toHaveBeenCalledOnce();
});

it("không có mã vận đơn: câu hướng dẫn, không gọi API", async () => {
  renderAt("/warehouse/picking-list");
  expect(await screen.findByText("Chưa chọn vận đơn")).toBeTruthy();
  expect(fake.pickingCalls).toHaveLength(0);
});
