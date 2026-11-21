// @vitest-environment jsdom
// Chi tiết đơn (mockup docs/mockups/m2/don-chi-tiet.html, trạng thái A–F; HV16–HV24). Nút hiện theo editScope và quyền
// trong /api/v1/me; sửa, chuyển trạng thái gửi version; 409 version_conflict giữ phần đang gõ (kế hoạch M2 Task M2-28).
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  CARE_ONLY,
  I1,
  O1,
  O2,
  R1,
  U2,
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
  stubDialog();
  fake = installFake();
  fake.orders.set(O1, order());
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ALL = [
  "order.update",
  "order.confirm",
  "order.cancel",
  "order.discount.approve",
  "order.return",
  "order.return.receive",
];
const CONFLICT = { status: 409, body: { error: "version_conflict", request_id: "t" } };
const actions = () => screen.getByRole("group", { name: "Thao tác đơn" });

async function open(perms: string[] = ALL) {
  fake.grants = grantsOf(...perms);
  const r = renderAt(`/don/${O1}`);
  await screen.findByRole("heading", { name: "DH0001235" });
  // Chờ /me để nút theo quyền đã hiện.
  await waitFor(() => {
    expect(actions().getAttribute("data-ready")).toBe("true");
  });
  return r;
}

describe("Chi tiết đơn — nút theo trạng thái và quyền", () => {
  it("chờ duyệt giảm giá, người xem có quyền duyệt: nút Duyệt; Xác nhận, Chờ khách mờ kèm lý do", async () => {
    fake.orders.set(O1, order({ label: "awaiting_discount_approval", pendingReason: "discount_approval" }));
    await open();
    const a = within(actions());
    expect(a.getByRole("button", { name: "Duyệt giảm giá và xác nhận" })).toBeTruthy();
    expect(a.getByRole("button", { name: "Sửa đơn" })).toBeTruthy();
    expect(a.getByRole<HTMLButtonElement>("button", { name: "Xác nhận" }).disabled).toBe(true);
    expect(a.getByText("Cần duyệt giảm giá trước")).toBeTruthy();
    expect(a.getByRole<HTMLButtonElement>("button", { name: "Chờ khách" }).disabled).toBe(true);
    expect(a.getByText("Đơn đang chờ duyệt giảm giá")).toBeTruthy();
    expect(a.getByRole("button", { name: "Hủy đơn" })).toBeTruthy();
    fireEvent.click(a.getByRole("button", { name: "Duyệt giảm giá và xác nhận" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ path: `/api/v1/orders/${O1}/approve-discount`, body: { version: 3 } });
    });
    expect(fake.writes.at(-1)?.key).toMatch(/.+/);
  });

  it("chờ duyệt, người xem không có quyền duyệt: không có nút Duyệt, có câu đang chờ", async () => {
    fake.orders.set(O1, order({ label: "awaiting_discount_approval", pendingReason: "discount_approval" }));
    await open(["order.update", "order.confirm", "order.cancel"]);
    expect(screen.queryByRole("button", { name: /Duyệt giảm giá/ })).toBeNull();
    expect(screen.getByText("Đang chờ Chủ hoặc Quản lý duyệt giảm giá")).toBeTruthy();
  });

  it("đơn nháp chờ duyệt: nút chỉ Duyệt giảm giá (không xác nhận); nháp thiếu địa chỉ: Xác nhận mờ kèm lý do", async () => {
    fake.orders.set(
      O1,
      order({ status: "draft", label: "draft", pendingReason: "discount_approval", addressComplete: false }),
    );
    await open();
    const a = within(actions());
    expect(a.getByRole("button", { name: "Duyệt giảm giá" })).toBeTruthy();
    expect(a.queryByRole("button", { name: "Chờ khách" })).toBeNull();
    fake.orders.set(O1, order({ status: "draft", label: "draft", addressComplete: false }));
    cleanup();
    await open();
    expect(within(actions()).getByRole<HTMLButtonElement>("button", { name: "Xác nhận" }).disabled).toBe(true);
    expect(within(actions()).getByText("Cần đủ địa chỉ giao (tỉnh, xã/phường) trước")).toBeTruthy();
  });

  it("đơn mới: Xác nhận và Chờ khách gửi version; đơn đang chờ khách có nút Bỏ chờ", async () => {
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Xác nhận" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ path: `/api/v1/orders/${O1}/confirm`, body: { version: 3 } });
    });
    fireEvent.click(within(actions()).getByRole("button", { name: "Chờ khách" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/orders/${O1}/pending`,
        body: { version: 3, reason: "awaiting_customer" },
      });
    });
    cleanup();
    fake.orders.set(O1, order({ label: "awaiting_customer", pendingReason: "awaiting_customer" }));
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Bỏ chờ" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ path: `/api/v1/orders/${O1}/pending`, body: { version: 3 } });
    });
    expect(fake.writes.at(-1)?.body.reason).toBeUndefined();
  });

  it("sau thao tác: đọc lại đơn (thân trả về của lệnh ghi không có editScope, khách, phiếu trả)", async () => {
    await open();
    const sparse = { ...order({ status: "confirmed", label: "confirmed", version: 4 }) };
    delete sparse.editScope;
    delete sparse.customer;
    fake.replies.push({ status: 200, body: sparse });
    fake.orders.set(O1, order({ status: "confirmed", label: "confirmed", version: 4 }));
    const before = fake.getCalls.length;
    fireEvent.click(within(actions()).getByRole("button", { name: "Xác nhận" }));
    await waitFor(() => {
      expect(fake.getCalls.length).toBeGreaterThan(before);
    });
    await waitFor(() => {
      expect(within(actions()).getByRole("button", { name: "Sửa đơn" })).toBeTruthy();
    });
    expect(screen.getByText("KH000877")).toBeTruthy();
  });

  it("thành công, chỉ sửa được phụ trách: không có Sửa đơn, Xác nhận, Hủy; có Tạo phiếu trả/đổi", async () => {
    fake.orders.set(O1, order({ status: "delivered", label: "delivered", editScope: CARE_ONLY }));
    await open();
    const a = within(actions());
    expect(a.queryByRole("button", { name: "Sửa đơn" })).toBeNull();
    expect(a.queryByRole("button", { name: "Xác nhận" })).toBeNull();
    expect(a.queryByRole("button", { name: "Hủy đơn" })).toBeNull();
    expect(screen.getByRole("button", { name: "Tạo phiếu trả/đổi" })).toBeTruthy();
    // Phụ trách sửa được, nguồn thì không.
    expect(screen.getByRole<HTMLSelectElement>("combobox", { name: "Người phụ trách" }).disabled).toBe(false);
    expect(screen.getByRole<HTMLSelectElement>("combobox", { name: "Nguồn đơn" }).disabled).toBe(true);
  });

  it("không có quyền sửa, hủy: không hiện nút đó (máy chủ vẫn chặn)", async () => {
    await open(["order.confirm"]);
    expect(within(actions()).queryByRole("button", { name: "Sửa đơn" })).toBeNull();
    expect(within(actions()).queryByRole("button", { name: "Hủy đơn" })).toBeNull();
    expect(within(actions()).getByRole("button", { name: "Xác nhận" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Thêm ghi chú" })).toBeNull();
  });
});

describe("Chi tiết đơn — đổi phiên bản (version_conflict)", () => {
  it("409 khi sửa: câu báo, phần đang gõ còn nguyên; Tải lại đơn mở bản mới", async () => {
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Sửa đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Sửa đơn DH0001235" });
    const discount = within(dlg).getByRole("textbox", { name: "Giảm cấp đơn" });
    fireEvent.change(discount, { target: { value: "100000" } });
    fake.replies.push(CONFLICT);
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu thay đổi" }));
    expect(await within(dlg).findByText("Đơn đã thay đổi, tải lại để xem bản mới.")).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({
      method: "PATCH",
      path: `/api/v1/orders/${O1}`,
      body: { version: 3, orderDiscount: 100000 },
    });
    expect(within(dlg).getByRole<HTMLInputElement>("textbox", { name: "Giảm cấp đơn" }).value).toBe("100000");
    // Người khác đã sửa: version 4, giảm cấp đơn 50.000.
    fake.orders.set(O1, order({ version: 4, orderDiscount: 50_000 }));
    fireEvent.click(within(dlg).getByRole("button", { name: "Tải lại đơn" }));
    await waitFor(() => {
      expect(
        within(screen.getByRole("dialog", { name: "Sửa đơn DH0001235" })).getByRole<HTMLInputElement>("textbox", {
          name: "Giảm cấp đơn",
        }).value,
      ).toBe("50.000");
    });
  });

  it("báo hiệu cho chính đơn này khi đang sửa: không mất phần đang gõ", async () => {
    const { qc } = await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Sửa đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Sửa đơn DH0001235" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Giảm cấp đơn" }), { target: { value: "7000" } });
    fake.orders.set(O1, order({ version: 4 }));
    const before = fake.getCalls.length;
    act(() => {
      onRealtimeChanged(qc, { topic: "order.changed", entity: { type: "order", id: O1 } });
    });
    await waitFor(() => {
      expect(fake.getCalls.length).toBeGreaterThan(before);
    });
    expect(
      within(screen.getByRole("dialog", { name: "Sửa đơn DH0001235" })).getByRole<HTMLInputElement>("textbox", {
        name: "Giảm cấp đơn",
      }).value,
    ).toBe("7000");
  });

  it("sửa chỉ gửi trường đã đổi, cùng version; lưu xong đóng hộp", async () => {
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Sửa đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Sửa đơn DH0001235" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số lượng DAM0123-BE-M" }), { target: { value: "2" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu thay đổi" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Sửa đơn DH0001235" })).toBeNull();
    });
    expect(fake.writes.at(-1)?.body).toEqual({
      version: 3,
      lines: [{ itemId: I1, variantId: orderItem().variantId, qty: 2, lineDiscount: 210_000 }],
    });
  });

  it("đơn chưa có nguồn (đơn đổi hàng do máy chủ tạo): mở sửa không tự gán nguồn mặc định; chưa đổi gì thì không lưu", async () => {
    fake.orders.set(O1, order({ trafficSourceId: undefined }));
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Sửa đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Sửa đơn DH0001235" });
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Lưu thay đổi" }).disabled).toBe(true);
    expect(within(dlg).getByText("Chưa có thay đổi nào để lưu.")).toBeTruthy();
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số lượng DAM0123-BE-M" }), { target: { value: "2" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu thay đổi" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.method).toBe("PATCH");
    });
    expect(fake.writes.at(-1)?.body.trafficSourceId).toBeUndefined();
  });

  it("409 khi xác nhận: câu báo đơn đã thay đổi và nút Tải lại đơn", async () => {
    await open();
    fake.replies.push(CONFLICT);
    fireEvent.click(within(actions()).getByRole("button", { name: "Xác nhận" }));
    expect(await screen.findByText("Đơn đã thay đổi, tải lại để xem bản mới.")).toBeTruthy();
    const before = fake.getCalls.length;
    fireEvent.click(screen.getByRole("button", { name: "Tải lại đơn" }));
    await waitFor(() => {
      expect(fake.getCalls.length).toBeGreaterThan(before);
    });
  });

  it("báo hiệu cho đơn khác không tải lại đơn này; cho đơn này thì tải lại đơn và lịch sử", async () => {
    const { qc } = await open();
    const [g0, t0] = [fake.getCalls.length, fake.timelineCalls];
    act(() => {
      onRealtimeChanged(qc, { topic: "order.changed", entity: { type: "order", id: O2 } });
    });
    await new Promise((r) => setTimeout(r, 30));
    expect([fake.getCalls.length, fake.timelineCalls]).toEqual([g0, t0]);
    act(() => {
      onRealtimeChanged(qc, { topic: "order.changed", entity: { type: "order", id: O1 } });
    });
    await waitFor(() => {
      expect(fake.getCalls.length).toBe(g0 + 1);
      expect(fake.timelineCalls).toBe(t0 + 1);
    });
  });
});

// D-02 (Task 437): sửa đơn giữ mã cũ không gọi checkCoupon (lượt của chính đơn); máy chủ giữ số giảm cũ khi tạm tính
// sau giảm (tạm tính − giảm dòng − giảm cấp đơn) không đổi, đổi thì tính lại (mã %, mức tối thiểu) — edit.go applyCoupon.
describe("Chi tiết đơn — sửa đơn giữ mã giảm giá", () => {
  // Tạm tính 2.070.000, giảm dòng 210.000 → sau giảm 1.860.000; mã GIAM10 đang giảm 186.000; tổng 1.674.000.
  const withCoupon = () =>
    order({
      couponCode: "GIAM10",
      couponDiscount: 186_000,
      discountTotal: 396_000,
      grandTotal: 1_674_000,
      codAmount: 1_674_000,
    });
  const RECALC =
    "Tiền hàng hoặc giảm giá đã đổi: mã GIAM10 sẽ được tính lại khi lưu; đơn dưới mức tối thiểu của mã thì không lưu được.";

  async function openEdit() {
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Sửa đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Sửa đơn DH0001235" });
    return { dlg, sum: within(dlg).getByRole("region", { name: "Tiền" }) };
  }
  const row = (sum: HTMLElement, label: string) => {
    const r = within(sum).getByText(label).closest(".sum-r");
    if (!(r instanceof HTMLElement)) throw new Error(`không có dòng ${label}`);
    return r.textContent;
  };
  const qty = (dlg: HTMLElement, v: string) => {
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số lượng DAM0123-BE-M" }), { target: { value: v } });
  };

  it("tạm tính sau giảm chưa đổi: hiện số giảm đang lưu, tổng chắc; không kiểm mã", async () => {
    fake.orders.set(O1, withCoupon());
    const { sum } = await openEdit();
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mã−186.000 ₫");
    expect(row(sum, "Tổng")).toBe("Tổng1.674.000 ₫");
    expect(row(sum, "Thu hộ (COD)")).toBe("Thu hộ (COD)1.674.000 ₫");
    expect(within(sum).queryByText(RECALC)).toBeNull();
    expect(fake.couponChecks).toBe(0);
  });

  it("đổi số lượng: giảm theo mã 'Tính lại khi lưu', tổng ghi chưa trừ mã; đổi lại như cũ thì số cũ trở lại", async () => {
    fake.orders.set(O1, withCoupon());
    const { dlg, sum } = await openEdit();
    qty(dlg, "4"); // 4 × 690.000 − 210.000 = 2.550.000
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mãTính lại khi lưu");
    expect(row(sum, "Tổng (chưa trừ mã)")).toBe("Tổng (chưa trừ mã)2.550.000 ₫");
    expect(row(sum, "Thu hộ (COD, chưa trừ mã)")).toBe("Thu hộ (COD, chưa trừ mã)2.550.000 ₫");
    expect(within(sum).queryByText("−186.000 ₫")).toBeNull();
    expect(within(sum).getByText(RECALC)).toBeTruthy();
    qty(dlg, "3");
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mã−186.000 ₫");
    expect(row(sum, "Tổng")).toBe("Tổng1.674.000 ₫");
    expect(within(sum).queryByText(RECALC)).toBeNull();
    expect(fake.couponChecks).toBe(0);
  });

  it("số giảm cũ lớn hơn tiền hàng sau sửa: không báo lỗi giả 'giảm quá tiền hàng', lưu được (máy chủ quyết)", async () => {
    // 1 × 690.000 − giảm dòng 600.000 = 90.000 < 186.000 (số giảm của mã đang lưu).
    fake.orders.set(O1, withCoupon());
    const { dlg, sum } = await openEdit();
    qty(dlg, "1");
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Giảm dòng DAM0123-BE-M (đồng)" }), {
      target: { value: "600.000" },
    });
    expect(row(sum, "Tổng (chưa trừ mã)")).toBe("Tổng (chưa trừ mã)90.000 ₫");
    expect(within(sum).getByText(RECALC)).toBeTruthy();
    expect(within(dlg).queryAllByText("Giảm giá đơn lớn hơn tạm tính")).toHaveLength(0);
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Lưu thay đổi" }).disabled).toBe(false);
  });

  it("đổi giảm cấp đơn: tính lại khi lưu; chỉ đổi phí ship: giữ số giảm, tổng chắc", async () => {
    fake.orders.set(O1, withCoupon());
    const { dlg, sum } = await openEdit();
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Phí ship báo khách" }), { target: { value: "30.000" } });
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mã−186.000 ₫");
    expect(row(sum, "Tổng")).toBe("Tổng1.704.000 ₫");
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Giảm cấp đơn" }), { target: { value: "60.000" } });
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mãTính lại khi lưu");
    expect(row(sum, "Tổng (chưa trừ mã)")).toBe("Tổng (chưa trừ mã)1.830.000 ₫"); // 1.860.000 − 60.000 + 30.000
    expect(within(sum).getByText(RECALC)).toBeTruthy();
  });

  it("bỏ mã rồi đổi số lượng: giảm theo mã 0, tổng chắc; đơn không có mã: không có câu tính lại", async () => {
    fake.orders.set(O1, withCoupon());
    const { dlg, sum } = await openEdit();
    fireEvent.click(within(dlg).getByRole("button", { name: "Bỏ mã" }));
    qty(dlg, "4");
    expect(row(sum, "Giảm theo mã")).toBe("Giảm theo mã0 ₫");
    expect(row(sum, "Tổng")).toBe("Tổng2.550.000 ₫");
    expect(within(sum).queryByText(RECALC)).toBeNull();
    cleanup();
    fake.orders.set(O1, order());
    const b = await openEdit();
    qty(b.dlg, "4");
    expect(row(b.sum, "Giảm theo mã")).toBe("Giảm theo mã0 ₫");
    expect(row(b.sum, "Tổng")).toBe("Tổng2.550.000 ₫");
    expect(within(b.sum).queryByText(/sẽ được tính lại khi lưu/)).toBeNull();
  });
});

describe("Chi tiết đơn — hủy", () => {
  it("chưa gửi hãng: 5 lý do, ghi chú; gửi version, lý do, ghi chú", async () => {
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Hủy đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Hủy đơn DH0001235?" });
    expect(within(dlg).getAllByRole("radio")).toHaveLength(5);
    fireEvent.click(within(dlg).getByRole("radio", { name: "Không liên lạc được" }));
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Ghi chú" }), { target: { value: "Gọi 3 lần" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy đơn" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/orders/${O1}/cancel`,
        body: { version: 3, reason: "unreachable", note: "Gọi 3 lần" },
      });
    });
  });

  it("đã gửi hãng: hộp Yêu cầu hủy, nút Gửi yêu cầu hủy", async () => {
    fake.orders.set(O1, order({ status: "confirmed", label: "awaiting_pickup", currentShipmentId: R1 }));
    await open();
    fireEvent.click(within(actions()).getByRole("button", { name: "Hủy đơn" }));
    const dlg = await screen.findByRole("dialog", { name: "Yêu cầu hủy DH0001235?" });
    expect(within(dlg).getByText(/hàng chưa trả về kho cho tới khi hãng xác nhận hủy/)).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Gửi yêu cầu hủy" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ body: { version: 3, reason: "customer_changed_mind" } });
    });
  });
});

describe("Chi tiết đơn — phiếu trả", () => {
  const delivered = () => {
    fake.orders.set(
      O1,
      order({
        status: "delivered",
        label: "delivered",
        editScope: CARE_ONLY,
        returns: [{ id: R1, code: "TH0000012", kind: "customer_return", status: "requested" }],
      }),
    );
    fake.returns.set(R1, {
      id: R1,
      code: "TH0000012",
      orderId: O1,
      kind: "customer_return",
      status: "requested",
      refundAmount: 1_240_000,
      items: [{ id: "ri1", orderItemId: I1, qty: 2, refundAmount: 1_240_000 }],
      createdAt: "2026-11-18T00:00:00Z",
      version: 1,
    });
  };

  it("thiếu order.return.receive: không có nút Nhận hàng trả; có thì có", async () => {
    delivered();
    await open(["order.return"]);
    const row = await screen.findByRole("row", { name: /TH0000012/ });
    expect(within(row).getByText("Chờ nhận hàng")).toBeTruthy();
    expect(within(row).getByText("1.240.000 ₫")).toBeTruthy();
    expect(within(row).queryByRole("button", { name: "Nhận hàng trả" })).toBeNull();
    expect(within(row).getByRole("button", { name: "Hủy phiếu" })).toBeTruthy();
    cleanup();
    await open(["order.return", "order.return.receive"]);
    expect(
      within(await screen.findByRole("row", { name: /TH0000012/ })).getByRole("button", { name: "Nhận hàng trả" }),
    ).toBeTruthy();
  });

  it("đọc phiếu trả lỗi: câu lỗi riêng, nút tạo phiếu mờ (không treo ở đang tải)", async () => {
    delivered();
    fake.returns.delete(R1);
    await open(["order.return"]);
    expect(await screen.findByText("Không tìm thấy, có thể đã bị xóa")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Tạo phiếu trả/đổi" }).disabled).toBe(true);
  });

  it("nhận hàng trả: tình trạng từng dòng, gửi version của phiếu", async () => {
    delivered();
    await open(["order.return.receive"]);
    const row = await screen.findByRole("row", { name: /TH0000012/ });
    fireEvent.click(within(row).getByRole("button", { name: "Nhận hàng trả" }));
    const dlg = await screen.findByRole("dialog", { name: "Nhận hàng trả TH0000012" });
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Tình trạng DAM0123-BE-M" }), {
      target: { value: "damaged" },
    });
    fireEvent.click(within(dlg).getByRole("button", { name: "Nhận và nhập kho" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/returns/${R1}/receive`,
        body: { version: 1, lines: [{ orderItemId: I1, condition: "damaged" }] },
      });
    });
  });
});

describe("Chi tiết đơn — nội dung", () => {
  it("dòng, tổng, khách, giao tới, lịch sử (chỉ tên trường đã đổi), ghi chú; thêm ghi chú", async () => {
    fake.timeline = [
      {
        at: "2026-11-18T06:22:00Z",
        kind: "edit",
        refId: "a1",
        actor: { id: U2, name: "Lê Văn Đức" },
        summary: { action: "order.update", lines: "changed", shipAddress: "changed" },
      },
      {
        at: "2026-11-18T06:06:00Z",
        kind: "note",
        refId: "n1",
        actor: { id: U2, name: "Lê Văn Đức" },
        note: "Khách sỉ",
      },
      {
        at: "2026-11-18T06:05:00Z",
        kind: "status",
        refId: "h1",
        actor: { id: U2, name: "Lê Văn Đức" },
        summary: { to: "new", toPending: "discount_approval", reason: "manual_discount_over_threshold" },
      },
    ];
    await open();
    expect(screen.getByText("KH000877")).toBeTruthy();
    expect(screen.getByText("23")).toBeTruthy();
    expect(screen.getByText("21 thành công · 1 hoàn")).toBeTruthy();
    expect(screen.getByText("5%")).toBeTruthy();
    expect(screen.getByText("Chị Châu")).toBeTruthy();
    expect(screen.getByText("0903 111 222")).toBeTruthy();
    expect(screen.getByText("Tầng 3, 45 Lê Lợi, Phường Bến Thành, TP. Hồ Chí Minh")).toBeTruthy();
    const tl = await screen.findByRole("list", { name: "Lịch sử thao tác" });
    expect(within(tl).getByText("Sản phẩm: đã đổi · Địa chỉ giao: đã đổi")).toBeTruthy();
    expect(within(tl).getByText("Tạo đơn → Mới · Chờ duyệt giảm giá")).toBeTruthy();
    expect(within(tl).getByText("Giảm tay vượt mức duyệt")).toBeTruthy();
    expect(screen.getAllByText("Khách sỉ").length).toBeGreaterThan(0);
    fireEvent.change(screen.getByRole("textbox", { name: "Thêm ghi chú" }), { target: { value: "Gọi lại 17h" } });
    fireEvent.click(screen.getByRole("button", { name: "Thêm ghi chú" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ path: `/api/v1/orders/${O1}/notes`, body: { body: "Gọi lại 17h" } });
    });
  });

  it("đổi người phụ trách gửi ngay cùng version (người có quyền xem danh sách nhân viên)", async () => {
    await open([...ALL, "identity.users.manage"]);
    const sel = screen.getByRole("combobox", { name: "Người phụ trách" });
    await waitFor(() => {
      expect(within(sel).getAllByRole("option").length).toBeGreaterThan(1);
    });
    fireEvent.change(sel, { target: { value: "0195f3b2-0000-7000-8000-0000000000e1" } });
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        method: "PATCH",
        body: { version: 3, careBy: "0195f3b2-0000-7000-8000-0000000000e1" },
      });
    });
  });
});
