// @vitest-environment jsdom
// Đảo "Thành công" và giao một phần trên khối Vận chuyển (kế hoạch M3 Task 138, TI08, TD09; mockup
// docs/mockups/m3/tien-tren-don.html trạng thái D, tra-doi.html trạng thái C; HV29, HV35): hai bước xem trước → xác
// nhận gửi token; 409 preview_stale → bản xem trước mới; bị chặn → câu Blocking, không có nút xác nhận, chỉ có lối sang
// "Điều chỉnh COD"; giao một phần: món khách nhận, tiền thực thu, lý do; người không có fulfillment.correct thấy nút mờ.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Shipment, ShipmentCorrectionPreview } from "../../api";
import { I1, I2, orderItem, stubDialog } from "../don/donTestkit";
import {
  SENDER,
  SH1,
  WH,
  confirmedOrder,
  grantsAt,
  installFake,
  renderPanel,
  shipment,
  type Fake,
} from "../shipping/shippingTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const panel = () => screen.getByRole("region", { name: "Vận chuyển" });

function sent(p: Partial<Shipment>, ...perms: string[]) {
  fake.grants = grantsAt(WH, ...SENDER, "cash.cod.read", ...perms);
  fake.shipments.set(SH1, shipment({ createState: "created", trackingCode: "1234567890321", ...p }));
  return confirmedOrder({
    currentShipmentId: SH1,
    codAmount: 1_330_000,
    items: [
      orderItem({ id: I1, sku: "DAM0201-BE-L", qty: 1, unitPrice: 990_000, netAmount: 990_000, lineDiscount: 0 }),
      orderItem({ id: I2, sku: "QU0220-DEN-27", qty: 1, unitPrice: 340_000, netAmount: 340_000, lineDiscount: 0 }),
    ],
  });
}

function preview(p: Partial<ShipmentCorrectionPreview> = {}): ShipmentCorrectionPreview {
  return {
    shipmentId: SH1,
    from: "delivered",
    to: "returning",
    effects: [
      "Vận đơn từ Thành công sang Đang chuyển hoàn; đơn bỏ Thành công, mở phiếu Hãng hoàn chờ kho nhận",
      "Đảo bút toán tiền hãng đã thu 1.960.000 đ",
    ],
    blocking: [],
    token: "tok-1",
    ...p,
  };
}

async function openCorrection() {
  const b = await within(panel()).findByRole<HTMLButtonElement>("button", { name: 'Đảo "Thành công"' });
  await waitFor(() => {
    expect(b.disabled).toBe(false);
  });
  fireEvent.click(b);
  return screen.findByRole("dialog", { name: 'Đảo "Thành công" · DH0001235' });
}

describe('Đảo "Thành công" (TI08)', () => {
  it("lý do bắt buộc → xem trước hiện tác động → xác nhận gửi token của bản xem trước", async () => {
    renderPanel(sent({ status: "delivered", label: "Thành công" }, "fulfillment.correct"));
    const dlg = await openCorrection();
    expect(dlg.textContent).toContain("Vận đơn 1234567890321 · Thành công → Đang chuyển hoàn");
    const look = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Xem trước" });
    expect(look.disabled).toBe(true);
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), {
      target: { value: "Hãng xác nhận giao nhầm" },
    });
    fake.replies.push({ status: 200, body: preview() });
    fireEvent.click(look);
    expect(await within(dlg).findByText(/đơn bỏ Thành công, mở phiếu Hãng hoàn/)).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({
      path: `/api/v1/shipments/${SH1}/corrections/preview`,
      body: { to: "returning", reason: "Hãng xác nhận giao nhầm" },
    });
    fake.replies.push({ status: 200, body: shipment({ status: "returning" }) });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xác nhận đảo" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/shipments/${SH1}/corrections`,
        body: { to: "returning", reason: "Hãng xác nhận giao nhầm", previewToken: "tok-1" },
      });
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: 'Đảo "Thành công" · DH0001235' })).toBeNull();
    });
  });

  it("409 preview_stale: tự xem trước lại, hiện câu bản mới, xác nhận lần sau gửi token mới", async () => {
    renderPanel(sent({ status: "delivered", label: "Thành công" }, "fulfillment.correct"));
    const dlg = await openCorrection();
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), { target: { value: "Nhầm" } });
    fake.replies.push({ status: 200, body: preview() });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xem trước" }));
    await within(dlg).findByRole("button", { name: "Xác nhận đảo" });
    fake.replies.push(
      { status: 409, body: { error: "preview_stale", request_id: "t" } },
      { status: 200, body: preview({ token: "tok-2", effects: ["Tác động mới"] }) },
    );
    fireEvent.click(within(dlg).getByRole("button", { name: "Xác nhận đảo" }));
    expect(await within(dlg).findByText("Số liệu vừa đổi, đây là bản xem trước mới.")).toBeTruthy();
    expect(within(dlg).getByText("Tác động mới")).toBeTruthy();
    fake.replies.push({ status: 200, body: shipment({ status: "returning" }) });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xác nhận đảo" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toMatchObject({ previewToken: "tok-2" });
    });
  });

  it("bị chặn: câu Blocking, không có nút xác nhận, chỉ có lối sang Điều chỉnh COD", async () => {
    fake.cod = {
      balances: {
        toCollect: 0,
        carrierHolding: 0,
        shopReceived: 1_168_000,
        carrierFee: 32_000,
        salesClearing: -1_200_000,
      },
      transfers: [],
    };
    renderPanel(sent({ status: "delivered", label: "Thành công" }, "fulfillment.correct", "cash.adjust"));
    const dlg = await openCorrection();
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), { target: { value: "Nhầm" } });
    const why = "Không đảo được: hãng đã chuyển tiền cho vận đơn này (bảng kê đã chốt)";
    fake.replies.push({ status: 200, body: preview({ blocking: [why] }) });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xem trước" }));
    expect(await within(dlg).findByText(why)).toBeTruthy();
    expect(within(dlg).queryByRole("button", { name: "Xác nhận đảo" })).toBeNull();
    fireEvent.click(within(dlg).getByRole("button", { name: "Điều chỉnh COD" }));
    expect(await screen.findByRole("dialog", { name: "Điều chỉnh COD · DH0001235" })).toBeTruthy();
  });

  it("không có fulfillment.correct: nút mờ kèm lý do", async () => {
    renderPanel(sent({ status: "delivered", label: "Thành công" }));
    const b = await within(panel()).findByRole<HTMLButtonElement>("button", { name: 'Đảo "Thành công"' });
    await waitFor(() => {
      expect(panel().textContent).toContain('Cần quyền "Sửa vận đơn" (Chủ, Quản lý)');
    });
    expect(b.disabled).toBe(true);
  });
});

describe("Giao một phần (TD09, HV29)", () => {
  async function openPartial() {
    const b = await within(panel()).findByRole<HTMLButtonElement>("button", { name: "Ghi giao một phần" });
    await waitFor(() => {
      expect(b.disabled).toBe(false);
    });
    fireEvent.click(b);
    return screen.findByRole("dialog", { name: "Giao một phần · DH0001235" });
  }

  it("chọn món khách nhận, tiền thực thu, lý do → tác động; gửi đúng mọi dòng", async () => {
    renderPanel(
      sent({ status: "delivery_failed", label: "Giao thất bại", codAmount: 1_330_000 }, "fulfillment.correct"),
    );
    const dlg = await openPartial();
    expect(dlg.textContent).toContain("COD gửi hãng 1.330.000 ₫");
    const send = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Ghi giao một phần" });
    // Mặc định khách nhận đủ: chưa phải giao một phần.
    expect(send.disabled).toBe(true);
    expect(dlg.textContent).toContain("Khách nhận đủ mọi món thì không phải giao một phần");
    const row = within(dlg).getByRole("row", { name: /QU0220-DEN-27/ });
    fireEvent.click(within(row).getByRole("button", { name: "Bớt 1" }));
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Tiền shipper thực thu" }), {
      target: { value: "990000" },
    });
    expect(send.disabled).toBe(true);
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), { target: { value: "Khách chỉ lấy đầm" } });
    expect(dlg.textContent).toContain("Đơn thành công một phần: 1 món xuất kho");
    expect(dlg.textContent).toContain('1 món (QU0220-DEN-27) thành phiếu hoàn "giao một phần", kho nhận khi hàng về');
    expect(dlg.textContent).toContain("Sổ COD ghi thu 990.000 ₫, phần còn lại 340.000 ₫ không thu");
    fake.replies.push({ status: 200, body: shipment({ status: "delivered", partial: true }) });
    fireEvent.click(send);
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/shipments/${SH1}/partial-delivery`,
        body: {
          items: [
            { orderItemId: I1, deliveredQty: 1 },
            { orderItemId: I2, deliveredQty: 0 },
          ],
          codCollected: 990_000,
          reason: "Khách chỉ lấy đầm",
        },
      });
    });
  });

  it("tiền thu vượt COD: nút khóa kèm câu", async () => {
    renderPanel(sent({ status: "shipping", label: "Đang chuyển", codAmount: 1_330_000 }, "fulfillment.correct"));
    const dlg = await openPartial();
    fireEvent.click(
      within(within(dlg).getByRole("row", { name: /QU0220-DEN-27/ })).getByRole("button", { name: "Bớt 1" }),
    );
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Tiền shipper thực thu" }), {
      target: { value: "2000000" },
    });
    expect(within(dlg).getByText("Vượt COD gửi hãng 1.330.000 ₫")).toBeTruthy();
  });

  it("chờ thu gom: chỉ có nút khi hãng đã báo giao một phần (vận đơn cần xem)", async () => {
    const view = renderPanel(sent({ status: "awaiting_pickup", label: "Chờ thu gom" }, "fulfillment.correct"));
    expect(await within(panel()).findByRole("button", { name: "Đồng bộ lại" })).toBeTruthy();
    expect(within(panel()).queryByRole("button", { name: "Ghi giao một phần" })).toBeNull();
    view.unmount();
    renderPanel(sent({ status: "awaiting_pickup", label: "Chờ thu gom", needsReview: true }, "fulfillment.correct"));
    expect(await within(panel()).findByRole("button", { name: "Ghi giao một phần" })).toBeTruthy();
  });

  it("không có quyền Sửa vận đơn: nút mờ kèm lý do", async () => {
    renderPanel(sent({ status: "shipping", label: "Đang chuyển" }));
    const b = await within(panel()).findByRole<HTMLButtonElement>("button", { name: "Ghi giao một phần" });
    await waitFor(() => {
      expect(panel().textContent).toContain('Cần quyền "Sửa vận đơn" (Chủ, Quản lý)');
    });
    expect(b.disabled).toBe(true);
  });
});
