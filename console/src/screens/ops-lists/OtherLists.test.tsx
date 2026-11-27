// @vitest-environment jsdom
// Đơn kẹt, Cần xem, Giữ hàng quá hạn (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái D, E, F;
// VC16, VC18, KH09) và số đếm cạnh mục menu (HV02).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgedReservation, ShipmentReviewItem, StuckShipment } from "../../api";
import { OpsNavCount } from "./OpsNavCount";
import { LONG_NAME, ORDER, SHIP, WORKER, grantsOf, installFake, renderAt, stubDialog } from "./opsTestkit";

beforeEach(() => {
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const stuck = (p: Partial<StuckShipment> = {}): StuckShipment => ({
  shipmentId: SHIP,
  orderId: ORDER,
  orderCode: "DH0001199",
  carrierKey: "best",
  trackingCode: "BE0098812736VN",
  status: "shipping",
  stuckSince: "2026-11-19T01:00:00Z",
  lastSyncedAt: "2026-11-22T07:00:00Z",
  lastSyncError: "timeout",
  hoursSinceUpdate: 80,
  ...p,
});

const item = (p: Partial<ShipmentReviewItem> = {}): ShipmentReviewItem => ({
  anomalyId: "a1",
  shipmentId: SHIP,
  orderId: ORDER,
  orderCode: "DH0001240",
  kind: "in_doubt_create",
  label: "Tìm mã E261121K7Q2MX trên trang Viettel Post. Có: bấm Đã tạo. Không có: bấm Chưa tạo.",
  createdAt: "2026-11-22T01:00:00Z",
  ...p,
});

const aged = (p: Partial<AgedReservation> = {}): AgedReservation => ({
  reservationId: "r1",
  orderItemId: "i1",
  orderId: ORDER,
  orderCode: "DH0001002",
  variantId: "v1",
  sku: "DAM0123-BE-M",
  warehouseId: "w1",
  state: "held",
  quantity: 2,
  since: "2026-10-30T03:00:00Z",
  ageDays: 23,
  ...p,
});

const rowOfCode = (code: string) => screen.getByRole("link", { name: code }).closest("tr") as HTMLElement;

describe("Đơn kẹt", () => {
  it("giờ không cập nhật, lần CRM hỏi hãng kèm lỗi; đang gửi hãng không có Đồng bộ lại; Đồng bộ lại gửi khóa chống trùng", async () => {
    const fake = installFake({
      stuck: [
        stuck(),
        stuck({
          shipmentId: "s2",
          orderCode: "DH0001232",
          status: "creating",
          trackingCode: undefined,
          lastSyncedAt: undefined,
          hoursSinceUpdate: 26,
          carrierKey: "spx",
        }),
        stuck({
          shipmentId: "s3",
          orderCode: "DH0001170",
          status: "awaiting_pickup",
          lastSyncError: undefined,
          hoursSinceUpdate: 52,
        }),
      ],
    });
    renderAt("/ops/stuck");
    await screen.findByRole("table");
    const a = rowOfCode("DH0001199");
    const b = rowOfCode("DH0001232");
    const c = rowOfCode("DH0001170");
    expect(within(a).getByText("80 giờ")).toBeTruthy();
    expect(within(a).getByText("Best Express · BE0098812736VN")).toBeTruthy();
    expect(within(a).getByText(/hãng không trả lời/)).toBeTruthy();
    expect(within(b).getByText("Lệnh gửi không chạy xong")).toBeTruthy();
    expect(within(b).getByText("SPX · Chưa có mã hãng")).toBeTruthy();
    expect(within(b).queryByRole("button", { name: "Đồng bộ lại" })).toBeNull();
    expect(within(b).getByRole("link", { name: "Mở đơn, xử lý" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    expect(within(c).getByText(/đạt/)).toBeTruthy();
    fireEvent.click(within(a).getByRole("button", { name: "Đồng bộ lại" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/shipments/${SHIP}/sync`);
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("rỗng và lỗi 403 là hai trạng thái khác nhau", async () => {
    installFake({});
    renderAt("/ops/stuck");
    await screen.findByText("Không có đơn kẹt");
    cleanup();
    const fake = installFake({});
    fake.failReads.push(/shipments\/stuck/);
    renderAt("/ops/stuck");
    await screen.findByRole("alert");
    expect(screen.queryByText("Không có đơn kẹt")).toBeNull();
  });
});

describe("Cần xem", () => {
  const five = () => [
    item(),
    item({
      anomalyId: "a2",
      orderCode: "DH0001088",
      kind: "missing_items",
      label: "Kho nhận thiếu 1 món. Làm khiếu nại rồi bấm Đã xử lý.",
    }),
    item({
      anomalyId: "a3",
      orderCode: "DH0001150",
      kind: "partial_delivery",
      label: "Hãng báo khách chỉ nhận một phần.",
    }),
    item({ anomalyId: "a4", orderCode: "DH0001201", kind: "tracking_conflict", label: "Mã đã gắn với DH0001180." }),
    item({ anomalyId: "a5", orderCode: "DH0001202", kind: "missing_items", label: LONG_NAME }),
  ];

  it("mỗi dòng một câu hướng dẫn theo loại; lọc theo loại; nút theo loại", async () => {
    installFake({ review: five(), counts: { review: 5, stuck: 0, callbacksOpen: 0, callbacksOverdue: 0, inDoubt: 1 } });
    renderAt("/ops/review");
    await screen.findByText(/Tìm mã E261121K7Q2MX/);
    expect(screen.getByRole("button", { name: "Mọi loại 5" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Kiện hoàn thiếu món 2" })).toBeTruthy();
    expect(within(rowOfCode("DH0001240")).getByRole("button", { name: "Đã tạo" })).toBeTruthy();
    expect(within(rowOfCode("DH0001240")).getByRole("button", { name: "Chưa tạo" })).toBeTruthy();
    expect(within(rowOfCode("DH0001088")).getByRole("button", { name: "Đã xử lý" })).toBeTruthy();
    expect(within(rowOfCode("DH0001150")).queryByRole("button", { name: "Đã xử lý" })).toBeNull();
    expect(within(rowOfCode("DH0001150")).getByRole("link", { name: "Ghi giao một phần" }).getAttribute("href")).toBe(
      `/don/${ORDER}`,
    );
    fireEvent.click(screen.getByRole("button", { name: "Kiện hoàn thiếu món 2" }));
    expect(screen.getAllByRole("link", { name: /^DH/ })).toHaveLength(2);
    expect(screen.queryByText(/Tìm mã E261121K7Q2MX/)).toBeNull();
  });

  it("'Đã xử lý' đòi ghi cách xử lý rồi gửi resolution; 409 hiện câu lỗi", async () => {
    const fake = installFake({ review: five() });
    renderAt("/ops/review");
    await screen.findByText(/Kho nhận thiếu 1 món/);
    fireEvent.click(within(rowOfCode("DH0001088")).getByRole("button", { name: "Đã xử lý" }));
    const dlg = await screen.findByRole("dialog", { hidden: true });
    const save = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Lưu", hidden: true });
    expect(save.disabled).toBe(true);
    fireEvent.change(within(dlg).getByLabelText("Đã làm gì *"), { target: { value: "  Đã gửi khiếu nại KN-2211 " } });
    expect(save.disabled).toBe(false);
    fake.replies.push({ status: 409, body: { error: "invalid_transition", request_id: "t" } });
    fireEvent.click(save);
    await within(dlg).findByText(/Đơn đang ở trạng thái không làm được việc này/);
    expect(fake.writes[0]?.path).toBe("/api/v1/shipment-anomalies/a2/resolve");
    expect(fake.writes[0]?.body).toEqual({ resolution: "Đã gửi khiếu nại KN-2211" });
  });

  it("'Đã tạo' đòi mã hãng và ghi chú; 'Chưa tạo' chỉ đòi ghi chú", async () => {
    const fake = installFake({ review: [item()] });
    renderAt("/ops/review");
    await screen.findByText(/Tìm mã E261121K7Q2MX/);
    fireEvent.click(screen.getByRole("button", { name: "Đã tạo" }));
    let dlg = await screen.findByRole("dialog", { hidden: true });
    expect(within(dlg).getByText("Nhập mã vận đơn của hãng")).toBeTruthy();
    fireEvent.change(within(dlg).getByLabelText("Mã vận đơn của hãng *"), { target: { value: "E261121K7Q2MX" } });
    fireEvent.change(within(dlg).getByLabelText("Đã kiểm ở đâu *"), { target: { value: "Trang Viettel Post" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi đã tạo", hidden: true }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/shipments/${SHIP}/resolve-create`);
    expect(fake.writes[0]?.body).toEqual({
      result: "created",
      trackingCode: "E261121K7Q2MX",
      reason: "Trang Viettel Post",
    });
    cleanup();
    fake.writes.length = 0;
    renderAt("/ops/review");
    await screen.findByText(/Tìm mã E261121K7Q2MX/);
    fireEvent.click(screen.getByRole("button", { name: "Chưa tạo" }));
    dlg = await screen.findByRole("dialog", { hidden: true });
    expect(within(dlg).queryByLabelText("Mã vận đơn của hãng *")).toBeNull();
    fireEvent.change(within(dlg).getByLabelText("Đã kiểm ở đâu *"), { target: { value: "Không có trên trang hãng" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi chưa tạo, gửi lại", hidden: true }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ result: "not_created", reason: "Không có trên trang hãng" });
  });

  it("thiếu quyền: nút xử lý mờ", async () => {
    installFake({ review: five(), grants: grantsOf("fulfillment.read") });
    renderAt("/ops/review");
    await screen.findByText(/Tìm mã E261121K7Q2MX/);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Đã tạo" }).disabled).toBe(true);
    expect(screen.getAllByRole<HTMLButtonElement>("button", { name: "Đã xử lý" })[0]?.disabled).toBe(true);
  });
});

describe("Giữ hàng quá hạn", () => {
  it("không có nút nhả hàng, chỉ 'Mở đơn'; tên kho, trạng thái, số ngày; lọc theo trạng thái", async () => {
    installFake({
      aged: [
        aged(),
        aged({
          reservationId: "r2",
          orderCode: "DH0000412",
          sku: "AO0101-DEN-M",
          state: "in_transit",
          ageDays: 184,
          quantity: 1,
          since: "2026-05-21T03:00:00Z",
        }),
      ],
      grants: grantsOf(...WORKER, "inventory.read"),
    });
    renderAt("/ops/aged-holds");
    await screen.findByRole("table");
    const a = rowOfCode("DH0001002");
    expect(within(a).getByText("Đang giữ · 23 ngày")).toBeTruthy();
    expect(within(a).getByText("30/10")).toBeTruthy();
    await within(a).findByText("DAM0123-BE-M · Kho chính");
    expect(within(rowOfCode("DH0000412")).getByText("Đang giao · 184 ngày")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /nhả/i })).toBeNull();
    expect(within(a).getByRole("link", { name: "Mở đơn" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    fireEvent.click(screen.getByRole("button", { name: "Đang giao" }));
    await waitFor(() => {
      expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(2);
    });
  });

  it("rỗng có câu riêng", async () => {
    installFake({});
    renderAt("/ops/aged-holds");
    await screen.findByText("Không có hàng giữ quá hạn");
  });
});

describe("Số đếm cạnh mục menu", () => {
  const view = (which: "callbacks" | "stuck" | "review") =>
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <OpsNavCount which={which} />
      </QueryClientProvider>,
    );
  it("Cần gọi lại 12 có 3 quá hạn (nền đỏ), Đơn kẹt 4, Cần xem 6; số 0 thì không vẽ", async () => {
    const fake = installFake({ counts: { review: 6, stuck: 4, callbacksOpen: 12, callbacksOverdue: 3, inDoubt: 0 } });
    const a = view("callbacks");
    await screen.findByText(", 12 việc, 3 quá hạn");
    expect(a.container.querySelector(".nav-n.is-late")?.textContent).toBe("12");
    cleanup();
    view("stuck");
    await screen.findByText(", 4 việc");
    cleanup();
    view("review");
    await screen.findByText(", 6 việc");
    cleanup();
    fake.counts = { review: 0, stuck: 0, callbacksOpen: 0, callbacksOverdue: 0, inDoubt: 0 };
    const reads = fake.listCalls.length;
    const z = view("review");
    await waitFor(() => {
      expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(reads);
    });
    expect(z.container.querySelector(".nav-n")).toBeNull();
  });
});
