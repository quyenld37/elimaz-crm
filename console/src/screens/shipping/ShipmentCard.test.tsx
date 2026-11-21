// @vitest-environment jsdom
// Khối Vận chuyển sau khi gửi (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng thái D–H;
// HV12–HV16): cập nhật lần cuối, vận đơn kẹt, đồng bộ lại, hủy / đổi hãng (lý do bắt buộc), đang yêu cầu hủy, đổi địa
// chỉ (hãng không sửa qua API → xác nhận tay), cập nhật trạng thái tay, khung COD bốn ngăn, dòng thời gian có nguồn.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AddressChange, ShipmentEvent } from "../../api";
import { stubDialog } from "../don/donTestkit";
import { DeliveryStatsBadge } from "../len-don/DeliveryStatsBadge";
import { OrderShipmentCards } from "./Timeline";
import {
  O1,
  SENDER,
  SETTINGS,
  SH1,
  WH,
  confirmedOrder,
  grantsAt,
  installFake,
  renderPanel,
  shipment,
  type Fake,
} from "./shippingTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const AC1 = "0195f3b2-0000-7000-8000-00000000ac99";
const ALL = [
  ...SENDER,
  "fulfillment.print",
  "fulfillment.cancel",
  "fulfillment.manual_status",
  "order.cancel",
  "cash.cod.read",
];
const panel = () => screen.getByRole("region", { name: "Vận chuyển" });
const writesTo = (suffix: string) => fake.writes.filter((w) => w.path.endsWith(suffix));
/** Thân của lệnh hủy vận đơn duy nhất đã gửi. */
async function cancelBody() {
  await waitFor(() => {
    expect(writesTo(`/shipments/${SH1}/cancel`)).toHaveLength(1);
  });
  return writesTo(`/shipments/${SH1}/cancel`)[0]?.body;
}

/** Đơn có vận đơn SH1 đã gửi hãng (mặc định Viettel Post, đang chuyển). */
function sent(p: Parameters<typeof shipment>[0] = {}, orderStatus: "confirmed" | "cancel_requested" = "confirmed") {
  fake.grants = grantsAt(WH, ...ALL);
  const s = shipment({
    status: "shipping",
    createState: "created",
    label: "Đang chuyển",
    trackingCode: "1234567890123",
    service: "Chuyển phát nhanh",
    fee: 32_000,
    weightGrams: 900,
    lastCarrierEventAt: "2026-11-22T07:20:00Z",
    lastSyncedAt: "2026-11-22T07:21:00Z",
    ...p,
  });
  fake.shipments.set(SH1, s);
  return confirmedOrder({ status: orderStatus, currentShipmentId: SH1, version: 7 });
}

function renderCards(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Thẻ vận đơn đã gửi (trạng thái D, E)", () => {
  it("mã hãng, nhãn, cập nhật lần cuối theo giờ hãng kèm lần CRM hỏi và lỗi; đủ bốn nút; phí, cân nặng, thu hộ", async () => {
    renderPanel(sent({ lastSyncError: "timeout" }));
    expect(await within(panel()).findByText("1234567890123")).toBeTruthy();
    const p = panel();
    expect(within(p).getByText("Đang chuyển")).toBeTruthy();
    expect(within(p).getByText("Mã CRM E261121K7Q2MX")).toBeTruthy();
    expect(p.textContent).toContain(
      "Cập nhật lần cuối lúc 14:20 22/11 (hãng) · CRM hỏi lúc 14:21 · lần hỏi gần nhất lỗi: hãng không trả lời",
    );
    for (const name of ["Đồng bộ lại", "Đổi địa chỉ", "Hủy / đổi hãng"]) {
      expect(within(p).getByRole("button", { name })).toBeTruthy();
    }
    // Nút thứ tư: "In nhãn" sang màn Đóng gói (in theo lô), cần fulfillment.print.
    expect(within(p).getByRole("link", { name: "In nhãn" }).getAttribute("href")).toBe("/warehouse/pack");
    expect(p.textContent).toContain("32.000 ₫");
    expect(p.textContent).toContain("900 g");
    expect(p.textContent).toContain("1.960.000 ₫");
    // Đã lấy hàng: không hủy được, có lý do mờ (VC12 đường 409 shipment_not_cancellable của máy chủ).
    const cancel = within(p).getByRole("button", { name: "Hủy / đổi hãng" });
    expect((cancel as HTMLButtonElement).disabled).toBe(true);
    expect(p.textContent).toContain("Hãng đã lấy hàng: không hủy được");
  });

  it("thiếu fulfillment.print: không có In nhãn", async () => {
    const o = sent();
    fake.grants = grantsAt(WH, ...ALL.filter((x) => x !== "fulfillment.print"));
    renderPanel(o);
    expect(await within(panel()).findByRole("button", { name: "Đồng bộ lại" })).toBeTruthy();
    expect(within(panel()).queryByRole("link", { name: "In nhãn" })).toBeNull();
  });

  it("chưa có tin nào của hãng: nói rõ, không ghi giờ giả", async () => {
    const o = sent({ status: "awaiting_pickup", label: "Chờ thu gom" });
    fake.shipments.set(SH1, shipment({ status: "awaiting_pickup", createState: "created", trackingCode: "X1" }));
    renderPanel(o);
    expect(await within(panel()).findByText(/Hãng chưa gửi cập nhật nào/)).toBeTruthy();
  });

  it("VC18 vận đơn kẹt: băng 'Hãng chưa cập nhật N giờ (ngưỡng 72 giờ)' kèm lần hỏi lỗi", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: new Date("2026-11-22T07:00:00Z") });
    try {
      fake.settings = { ...SETTINGS, stuckHours: { shipping: 72 } };
      renderPanel(
        sent({
          lastCarrierEventAt: "2026-11-18T23:00:00Z",
          stuckSince: "2026-11-21T23:00:00Z",
          lastSyncError: "timeout",
        }),
      );
      expect(await within(panel()).findByText("Hãng chưa cập nhật 80 giờ")).toBeTruthy();
      await waitFor(() => {
        expect(panel().textContent).toContain("(ngưỡng 72 giờ). Cập nhật lần cuối 06:00 19/11.");
      });
      expect(panel().textContent).toContain("lần hỏi 14:21 22/11 lỗi: hãng không trả lời");
    } finally {
      vi.useRealTimers();
    }
  });

  it("Đồng bộ lại: một yêu cầu có khóa, câu đã nhờ hỏi hãng", async () => {
    fake.replies.push({ status: 202, body: { jobId: "j1" } });
    renderPanel(sent());
    fireEvent.click(await within(panel()).findByRole("button", { name: "Đồng bộ lại" }));
    expect(await within(panel()).findByText(/Đã nhờ CRM hỏi hãng ngay/)).toBeTruthy();
    const w = writesTo(`/shipments/${SH1}/sync`);
    expect(w).toHaveLength(1);
    expect(w[0]?.key).toBeTruthy();
  });

  it("TD03 đang yêu cầu hủy: băng 'Đang chờ hãng xác nhận hủy — hàng chưa được trả về kho', nút hủy mờ có lý do", async () => {
    renderPanel(sent({ status: "cancel_requested", label: "Đang yêu cầu hủy" }, "cancel_requested"));
    expect(await within(panel()).findByText("Đang chờ hãng xác nhận hủy — hàng chưa được trả về kho.")).toBeTruthy();
    expect(panel().textContent).toContain("đơn hủy hẳn");
    expect(within(panel()).getByRole<HTMLButtonElement>("button", { name: "Hủy / đổi hãng" }).disabled).toBe(true);
    expect(within(panel()).getByText("Đang chờ hãng trả lời yêu cầu hủy")).toBeTruthy();
  });

  it("đã giao xong: không còn nút đồng bộ, hủy, đổi địa chỉ", async () => {
    renderPanel(sent({ status: "delivered", label: "Thành công" }));
    expect(await within(panel()).findByText("Thành công")).toBeTruthy();
    expect(within(panel()).queryByRole("button", { name: "Đồng bộ lại" })).toBeNull();
    expect(within(panel()).queryByRole("button", { name: "Hủy / đổi hãng" })).toBeNull();
    expect(within(panel()).queryByRole("button", { name: "Đổi địa chỉ" })).toBeNull();
  });
});

describe("Hủy / đổi hãng (VC11, TD03, HV12)", () => {
  const openDialog = async () => {
    renderPanel(sent({ status: "awaiting_pickup", label: "Chờ thu gom" }));
    fireEvent.click(await within(panel()).findByRole("button", { name: "Hủy / đổi hãng" }));
    return screen.getByRole("dialog", { name: "Hủy vận đơn 1234567890123" });
  };

  it("đổi hãng: lý do bắt buộc; gửi cancelShipment {reason}, không đụng đơn", async () => {
    fake.replies.push({ status: 200, body: shipment({ status: "cancel_requested", createState: "created" }) });
    const d = await openDialog();
    const send = within(d).getByRole("button", { name: "Gửi yêu cầu hủy" });
    expect(within(d).getByRole<HTMLInputElement>("radio", { name: "Đổi hãng khác (giữ đơn)" }).checked).toBe(true);
    expect((send as HTMLButtonElement).disabled).toBe(true);
    expect(within(d).getByText("Ghi lý do")).toBeTruthy();
    fireEvent.change(within(d).getByLabelText("Lý do *"), {
      target: { value: "Viettel Post báo không lấy hàng được hôm nay" },
    });
    fireEvent.click(send);
    expect(await cancelBody()).toEqual({ reason: "Viettel Post báo không lấy hàng được hôm nay" });
    expect(writesTo(`/orders/${O1}/cancel`)).toHaveLength(0);
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("hủy đơn: đi cancelOrder của M2 với version của đơn, lý do vào ghi chú", async () => {
    fake.replies.push({ status: 200, body: confirmedOrder({ status: "cancel_requested" }) });
    const d = await openDialog();
    fireEvent.click(within(d).getByRole("radio", { name: "Hủy đơn (khách không lấy nữa)" }));
    fireEvent.change(within(d).getByLabelText("Lý do *"), { target: { value: "Khách đổi ý" } });
    fireEvent.click(within(d).getByRole("button", { name: "Gửi yêu cầu hủy" }));
    await waitFor(() => {
      expect(writesTo(`/orders/${O1}/cancel`)).toHaveLength(1);
    });
    expect(writesTo(`/orders/${O1}/cancel`)[0]?.body).toEqual({ version: 7, reason: "other", note: "Khách đổi ý" });
    expect(writesTo(`/shipments/${SH1}/cancel`)).toHaveLength(0);
  });

  it("không có quyền hủy đơn: lựa chọn Hủy đơn mờ", async () => {
    fake.grants = grantsAt(WH, ...SENDER, "fulfillment.cancel");
    const o = confirmedOrder({ currentShipmentId: SH1 });
    fake.shipments.set(
      SH1,
      shipment({ status: "awaiting_pickup", createState: "created", trackingCode: "1234567890123" }),
    );
    renderPanel(o);
    fireEvent.click(await within(panel()).findByRole("button", { name: "Hủy / đổi hãng" }));
    const d = screen.getByRole("dialog");
    expect(within(d).getByRole<HTMLInputElement>("radio", { name: "Hủy đơn (khách không lấy nữa)" }).disabled).toBe(
      true,
    );
  });

  /** Vận đơn đang tạo có lệnh tạo đã kết thúc (state): bấm "Hủy để đổi hãng", trả hộp hủy (chỉ đổi hãng). */
  async function replaceDialog(state: "unknown" | "not_sent") {
    fake.grants = grantsAt(WH, ...ALL);
    fake.shipments.set(SH1, shipment({ createState: state }));
    fake.replies.push({ status: 200, body: shipment({ status: "cancelled" }) });
    renderPanel(confirmedOrder({ currentShipmentId: SH1 }));
    fireEvent.click(await within(panel()).findByRole("button", { name: "Hủy để đổi hãng" }));
    const d = screen.getByRole("dialog");
    expect(within(d).queryByRole("radio", { name: "Hủy đơn (khách không lấy nữa)" })).toBeNull();
    expect(d.textContent).not.toContain("Hủy đơn: đơn thành");
    return d;
  }

  it("vận đơn 'Không rõ hãng đã tạo chưa': 'Hủy để đổi hãng' đòi tick đã tra hãng, gửi carrierChecked", async () => {
    const d = await replaceDialog("unknown");
    fireEvent.change(within(d).getByLabelText("Lý do *"), { target: { value: "Đã tra trang VTP" } });
    const send = within(d).getByRole<HTMLButtonElement>("button", { name: "Gửi yêu cầu hủy" });
    expect(send.disabled).toBe(true);
    fireEvent.click(within(d).getByRole("checkbox"));
    fireEvent.click(send);
    expect(await cancelBody()).toEqual({ reason: "Đã tra trang VTP", carrierChecked: true });
  });

  it("'Chưa gửi được': hộp chỉ có đổi hãng, không tick, gửi cancelShipment {reason}", async () => {
    const d = await replaceDialog("not_sent");
    expect(within(d).queryByRole("checkbox")).toBeNull();
    fireEvent.change(within(d).getByLabelText("Lý do *"), { target: { value: "Đổi sang GHN" } });
    fireEvent.click(within(d).getByRole("button", { name: "Gửi yêu cầu hủy" }));
    expect(await cancelBody()).toEqual({ reason: "Đổi sang GHN" });
  });
});

const manualChange = (p: Partial<AddressChange> = {}): AddressChange => ({
  id: AC1,
  shipmentId: SH1,
  mode: "manual",
  status: "pending",
  createdAt: "2026-11-21T03:58:00Z",
  ...p,
});

describe("Đổi địa chỉ sau khi gửi (DH15, HV13)", () => {
  it("gửi yêu cầu: địa chỉ mới, tỉnh, xã; người nhận, SĐT giữ như đơn; hãng không sửa qua API → khung xác nhận tay", async () => {
    fake.adminUnits = [
      { code: "01", name: "Hà Nội", level: "province" },
      { code: "00163", name: "Phường Láng Hạ", level: "ward", parentCode: "01" },
    ];
    fake.replies.push({ status: 201, body: manualChange() });
    renderPanel(sent());
    const open = await within(panel()).findByRole("button", { name: "Đổi địa chỉ" });
    await waitFor(() => {
      expect((open as HTMLButtonElement).disabled).toBe(false);
    });
    fireEvent.click(open);
    const d = screen.getByRole("dialog", { name: "Đổi địa chỉ giao" });
    expect(d.textContent).toContain("Đang giao tới: Tầng 3, 45 Lê Lợi, Phường Bến Thành, TP. Hồ Chí Minh");
    fireEvent.change(within(d).getByLabelText("Địa chỉ mới *"), { target: { value: "Tầng 5, 89 Láng Hạ" } });
    const province = within(d).getByLabelText("Tỉnh *");
    await within(province).findByRole("option", { name: "Hà Nội" });
    fireEvent.change(province, { target: { value: "01" } });
    const ward = within(d).getByLabelText("Xã/phường *");
    fireEvent.focus(ward);
    fireEvent.change(ward, { target: { value: "Láng" } });
    fireEvent.click(await within(d).findByRole("option", { name: "Phường Láng Hạ" }));
    fake.addressChanges = [manualChange()]; // máy chủ: yêu cầu vừa tạo là yêu cầu đang mở
    fireEvent.click(within(d).getByRole("button", { name: "Gửi yêu cầu cho hãng" }));
    await waitFor(() => {
      expect(writesTo(`/shipments/${SH1}/address-changes`)).toHaveLength(1);
    });
    expect(writesTo(`/shipments/${SH1}/address-changes`)[0]?.body).toEqual({
      address: {
        name: "Chị Châu",
        phone: "+84903111222",
        line: "Tầng 5, 89 Láng Hạ",
        provinceCode: "01",
        wardCode: "00163",
      },
    });
    const box = await screen.findByRole("region", { name: "Đổi địa chỉ" });
    expect(box.textContent).toContain('Gọi hãng đổi địa chỉ, sau đó bấm "Hãng đã nhận".');
    expect(box.textContent).toContain("Địa chỉ trên đơn vẫn là địa chỉ cũ tới khi bấm.");
    // Yêu cầu đang mở: không gửi yêu cầu thứ hai.
    expect(within(panel()).getByRole<HTMLButtonElement>("button", { name: "Đổi địa chỉ" }).disabled).toBe(true);
  });

  it("tải lại trang: yêu cầu đang mở đọc từ máy chủ; 'Hãng đã nhận' gửi confirmed rồi đọc lại đơn", async () => {
    fake.addressChanges = [manualChange()];
    fake.replies.push({ status: 200, body: manualChange({ status: "confirmed", resolvedAt: "2026-11-21T04:10:00Z" }) });
    renderPanel(sent());
    const box = await screen.findByRole("region", { name: "Đổi địa chỉ" });
    fake.addressChanges = [];
    fireEvent.click(within(box).getByRole("button", { name: "Hãng đã nhận" }));
    await waitFor(() => {
      expect(writesTo(`/address-changes/${AC1}/resolve`)).toHaveLength(1);
    });
    expect(writesTo(`/address-changes/${AC1}/resolve`)[0]?.body).toEqual({ result: "confirmed" });
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Đổi địa chỉ" })).toBeNull();
    });
  });

  it("yêu cầu qua API của hãng còn chờ: không có nút xác nhận tay", async () => {
    fake.addressChanges = [manualChange({ mode: "api", status: "pending" })];
    renderPanel(sent());
    const box = await screen.findByRole("region", { name: "Đổi địa chỉ" });
    expect(box.textContent).toContain("Đã gửi yêu cầu cho Viettel Post");
    expect(within(box).queryByRole("button", { name: "Hãng đã nhận" })).toBeNull();
  });
});

describe("Cập nhật trạng thái tay (VC14, HV16)", () => {
  const manual = () =>
    sent({
      mode: "manual",
      carrierKey: "ghn",
      accountName: "GHN — nhập mã tay",
      status: "awaiting_pickup",
      label: "Chờ thu gom",
      trackingCode: "GHN5KX92MQ",
      version: 4,
    });

  it("vận đơn nhập mã tay: câu hãng không báo trạng thái, nút Cập nhật trạng thái, không có Đồng bộ lại", async () => {
    renderPanel(manual());
    expect(await within(panel()).findByText("Hãng không báo trạng thái cho CRM. Cập nhật tay khi biết.")).toBeTruthy();
    expect(within(panel()).getByRole("button", { name: "Cập nhật trạng thái" })).toBeTruthy();
    expect(within(panel()).queryByRole("button", { name: "Đồng bộ lại" })).toBeNull();
  });

  it("đòi lý do; Thành công gửi tiền hãng đã thu, lúc theo giờ Việt Nam, version của vận đơn", async () => {
    fake.replies.push({ status: 200, body: shipment({ status: "delivered", mode: "manual", version: 5 }) });
    renderPanel(manual());
    fireEvent.click(await within(panel()).findByRole("button", { name: "Cập nhật trạng thái" }));
    const d = screen.getByRole("dialog", { name: "Cập nhật trạng thái" });
    const status = within(d).getByLabelText("Trạng thái mới *");
    expect(within(status).queryByRole("option", { name: "Đã hoàn về kho" })).toBeNull();
    fireEvent.change(status, { target: { value: "delivered" } });
    fireEvent.change(within(d).getByLabelText("Lúc *"), { target: { value: "2026-11-21T16:30" } });
    expect(within(d).getByLabelText<HTMLInputElement>(/^Tiền hãng đã thu/).value).toBe("1.960.000");
    const save = within(d).getByRole("button", { name: "Lưu" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(within(d).getByLabelText("Lý do *"), { target: { value: "Shipper GHN báo qua Zalo" } });
    fireEvent.click(save);
    await waitFor(() => {
      expect(writesTo(`/shipments/${SH1}/manual-status`)).toHaveLength(1);
    });
    expect(writesTo(`/shipments/${SH1}/manual-status`)[0]?.body).toEqual({
      to: "delivered",
      at: "2026-11-21T09:30:00.000Z",
      reason: "Shipper GHN báo qua Zalo",
      codCollected: 1_960_000,
      version: 4,
    });
  });

  it("không có quyền cập nhật tay: nút mờ có lý do", async () => {
    const o = manual();
    fake.grants = grantsAt(WH, ...SENDER);
    renderPanel(o);
    const b = await within(panel()).findByRole("button", { name: "Cập nhật trạng thái" });
    await waitFor(() => {
      expect((b as HTMLButtonElement).disabled).toBe(true);
    });
    expect(panel().textContent).toContain("Bạn không có quyền cập nhật trạng thái tay");
  });
});

describe("Khung COD bốn ngăn (HV15)", () => {
  it("bốn ngăn đúng số, ngăn đang có tiền viền đậm; đơn sỉ 9 chữ số", async () => {
    fake.cod = {
      balances: { toCollect: 0, carrierHolding: 112_450_000, shopReceived: 0, carrierFee: 0, salesClearing: 0 },
      transfers: [],
    };
    renderPanel(sent({ status: "delivered", label: "Thành công" }));
    const box = await screen.findByRole("region", { name: "Tiền thu hộ (COD)" });
    const cell = (name: string) => within(box).getByLabelText(name);
    await waitFor(() => {
      expect(cell("Hãng đang giữ").textContent).toContain("112.450.000 ₫");
    });
    expect(cell("Đang đi thu").textContent).toContain("0 ₫");
    expect(cell("Đã về tài khoản shop").textContent).toContain("0 ₫");
    expect(cell("Phí hãng").textContent).toContain("0 ₫");
    expect(cell("Hãng đang giữ").className).toContain("is-on");
    expect(cell("Đang đi thu").className).not.toContain("is-on");
  });

  it("không có quyền xem COD: câu riêng, không đọc", async () => {
    fake.grants = grantsAt(WH, ...SENDER);
    const o = confirmedOrder({ currentShipmentId: SH1 });
    fake.shipments.set(SH1, shipment({ status: "shipping", createState: "created" }));
    renderPanel(o);
    const box = await screen.findByRole("region", { name: "Tiền thu hộ (COD)" });
    expect(box.textContent).toContain('quyền "Xem tiền COD"');
  });
});

function ev(i: number, p: Partial<ShipmentEvent> = {}): ShipmentEvent {
  const at = new Date(Date.UTC(2026, 10, 22, 7, 20) - i * 3_600_000).toISOString();
  return {
    id: `0195f3b2-0000-7000-8000-0000000e${String(i).padStart(4, "0")}`,
    occurredAt: at,
    recordedAt: at,
    source: "carrier_api",
    rawStatus: `S${String(i)}`,
    label: `Sự kiện ${String(i)}`,
    applied: true,
    ...p,
  };
}

describe("Dòng thời gian vận đơn (HV14)", () => {
  it("12 sự kiện có nguồn Hãng / Nhân viên / Hệ thống; sự kiện bị bỏ qua hiện mờ kèm lý do", async () => {
    const items = Array.from({ length: 12 }, (_, i) => ev(i));
    items[1] = ev(1, { applied: false, ignoredReason: "duplicate_state" });
    items[3] = ev(3, { applied: false, ignoredReason: "older" });
    items[6] = ev(6, { source: "staff", label: "Đã đóng gói" });
    items[10] = ev(10, { source: "system", label: "Hãng đã tạo vận đơn · Chờ thu gom" });
    fake.events.set("", { items });
    fake.orderShipments = [shipment({ status: "shipping", label: "Đang chuyển", createState: "created" })];
    renderCards(<OrderShipmentCards order={confirmedOrder({ currentShipmentId: SH1 })} />);
    const card = await screen.findByRole("region", { name: "Dòng thời gian vận đơn" });
    const list = await within(card).findByRole("list", { name: "Sự kiện vận đơn" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(12);
    expect(card.textContent).toContain("12 sự kiện · mới nhất trên đầu");
    expect(rows[0]?.textContent).toContain("14:20 · 22/11");
    expect(rows[0]?.textContent).toContain("Hãng (API)");
    expect(rows[1]?.className).toBe("is-ign");
    expect(rows[1]?.textContent).toContain("Bỏ qua: trùng trạng thái hiện tại");
    expect(rows[3]?.textContent).toContain("Bỏ qua: cũ hơn trạng thái hiện tại");
    expect(rows[0]?.className).toBe("");
    expect(within(list).getByText("Nhân viên").className).toBe("src is-staff");
    expect(rows[6]?.textContent).toContain("Đã đóng gói · Nhân viên");
    expect(rows[10]?.textContent).toContain("Hệ thống");
    expect(within(card).queryByRole("button", { name: "Xem thêm" })).toBeNull();
    const history = screen.getByRole("region", { name: "Vận đơn của đơn này" });
    expect(history.textContent).toContain("E261121K7Q2MX");
    expect(history.textContent).toContain("(hiện hành)");
  });

  it("còn trang sau: số sự kiện có dấu +, 'Xem thêm' tải trang sau theo con trỏ", async () => {
    fake.events.set("", { items: [ev(0), ev(1)], nextCursor: "c2" });
    fake.events.set("c2", { items: [ev(2, { label: "Rời bưu cục gửi" })] });
    renderCards(<OrderShipmentCards order={confirmedOrder({ currentShipmentId: SH1 })} />);
    const card = await screen.findByRole("region", { name: "Dòng thời gian vận đơn" });
    await within(card).findByText("2+ sự kiện · mới nhất trên đầu");
    fireEvent.click(within(card).getByRole("button", { name: "Xem thêm" }));
    expect(await within(card).findByText("Rời bưu cục gửi")).toBeTruthy();
    expect(within(card).getByText("3 sự kiện · mới nhất trên đầu")).toBeTruthy();
  });

  it("vận đơn cũ đã hủy để đổi hãng hiện mờ kèm lý do", async () => {
    fake.orderShipments = [
      shipment({ status: "shipping", label: "Đang chuyển", createState: "created" }),
      shipment({
        id: "0195f3b2-0000-7000-8000-00000000af02",
        code: "E261121P3D8RA",
        carrierKey: "best",
        accountName: "Best Express — Kho chính",
        status: "cancelled",
        cancelKind: "replace",
        lastStatusAt: "2026-11-21T03:30:00Z",
      }),
    ];
    renderCards(<OrderShipmentCards order={confirmedOrder({ currentShipmentId: SH1 })} />);
    const history = await screen.findByRole("region", { name: "Vận đơn của đơn này" });
    await waitFor(() => {
      expect(history.textContent).toContain("Best Express · Đã hủy (đổi hãng) · 10:30 21/11");
    });
  });
});

describe("Huy hiệu nhận/hoàn của khách (DH03)", () => {
  it("'Nhận 3 · Hoàn 2 (40%)' kèm 'Hay hoàn'; mức theo dõi 'Có hoàn'; chưa có đơn giao xong", async () => {
    fake.stats = { delivered: 3, returned: 2, returnRateBp: 4000, risk: "high", hint: "Nên xin cọc" };
    const { unmount } = renderCards(<DeliveryStatsBadge customerId="c" />);
    const b = await screen.findByLabelText("Nhận, hoàn của khách");
    expect(b.textContent).toBe("Nhận 3 · Hoàn 2 (40%)Hay hoàn");
    expect(within(b).getByText("Hay hoàn").className).toBe("chip chip-err");
    unmount();
    fake.stats = { delivered: 9, returned: 1, returnRateBp: 1000, risk: "watch" };
    const second = renderCards(<DeliveryStatsBadge customerId="c" />);
    expect((await screen.findByLabelText("Nhận, hoàn của khách")).textContent).toBe("Nhận 9 · Hoàn 1 (10%)Có hoàn");
    second.unmount();
    fake.stats = { delivered: 0, returned: 0, returnRateBp: 0, risk: "none" };
    renderCards(<DeliveryStatsBadge customerId="c" />);
    expect(await screen.findByText("Chưa có đơn giao xong")).toBeTruthy();
  });
});
