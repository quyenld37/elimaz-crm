// @vitest-environment jsdom
// Danh sách đơn (mockup docs/mockups/m2/don-danh-sach.html, trạng thái A–C; HV14, HV15). Báo hiệu order.changed cho
// đơn X chỉ tải lại đúng đơn X (kế hoạch M2 Task M2-28, Step 1).
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { LONG_COMPANY, O1, O2, O3, installFake, listItem, order, renderAt, type Fake } from "./donTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.list = [
    listItem({ duplicateSuspect: true }),
    listItem({
      id: O2,
      code: "DH0001235",
      customerName: LONG_COMPANY,
      label: "awaiting_discount_approval",
      pendingReason: "discount_approval",
      itemCount: 64,
      careBy: undefined,
    }),
  ];
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const changed = (id: string) => ({ topic: "order.changed", entity: { type: "order", id } });
const table = () => screen.getByRole("table");

describe("Danh sách đơn", () => {
  it("mặc định nhãn Mới, 7 ngày qua; dòng có mã, khách, SĐT, nhãn, tiền, nguồn, phụ trách, dấu Nghi trùng", async () => {
    renderAt("/don");
    const row = await screen.findByRole("row", { name: /DH0001236/ });
    const first = fake.listCalls[0];
    expect(first?.getAll("label")).toEqual(["new"]);
    expect(first?.get("createdFrom")).not.toBeNull();
    expect(first?.get("limit")).toBe("50");
    expect(within(row).getByText("Nghi trùng")).toBeTruthy();
    expect(within(row).getByText("0987 654 321")).toBeTruthy();
    expect(within(row).getAllByText("1.800.000 ₫")).toHaveLength(2);
    expect(within(row).getByText("Zalo")).toBeTruthy();
    expect(within(row).getByText("Phạm Thu Trang")).toBeTruthy();
    const row2 = screen.getByRole("row", { name: /DH0001235/ });
    expect(within(row2).getByText(LONG_COMPANY)).toBeTruthy();
    expect(within(row2).getByText("Chờ duyệt giảm giá")).toBeTruthy();
    expect(within(row2).getByText("Chưa giao ai")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Mới" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("thanh nhãn không có nhãn vận chuyển (M3); bấm nhãn lọc theo nhãn; Tất cả bỏ lọc nhãn", async () => {
    renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    const bar = screen.getByRole("group", { name: "Lọc theo nhãn đơn" });
    const names = within(bar)
      .getAllByRole("button")
      .map((b) => b.textContent);
    expect(names).not.toContain("Đang chuyển");
    expect(names).not.toContain("Chờ thu gom");
    expect(names.at(-1)).toBe("Tất cả");
    fireEvent.click(within(bar).getByRole("button", { name: "Đang yêu cầu hủy" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.getAll("label")).toEqual(["cancel_requested"]);
    });
    fireEvent.click(within(bar).getByRole("button", { name: "Tất cả" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.getAll("label")).toEqual([]);
    });
  });

  it("báo hiệu cho đơn X đang có trong danh sách: chỉ tải lại đơn X (tìm theo mã), không tải lại cả trang", async () => {
    const { qc } = renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    const before = fake.listCalls.length;
    fake.list = [listItem({ label: "confirmed", status: "confirmed", grandTotal: 2_000_000 }), ...fake.list.slice(1)];
    act(() => {
      onRealtimeChanged(qc, changed(O1));
    });
    await waitFor(() => {
      expect(within(screen.getByRole("row", { name: /DH0001236/ })).getByText("Đã xác nhận")).toBeTruthy();
    });
    const calls = fake.listCalls.slice(before);
    expect(calls.map((c) => c.get("q"))).toEqual(["DH0001236"]);
    expect(fake.getCalls).toEqual([]);
    // Dòng vừa đổi tô nền (HV15); đơn kia giữ nguyên.
    expect(screen.getByRole("row", { name: /DH0001236/ }).className).toContain("is-new");
    expect(screen.getByRole("row", { name: /DH0001235/ }).className).not.toContain("is-new");
  });

  it("báo hiệu cho đơn không có trong danh sách: đọc đơn đó; nhãn khớp bộ lọc → tải lại danh sách, không khớp → thôi", async () => {
    const { qc } = renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    fake.orders.set(O3, order({ id: O3, code: "DH0001300", label: "confirmed", status: "confirmed" }));
    let before = fake.listCalls.length;
    act(() => {
      onRealtimeChanged(qc, changed(O3));
    });
    await waitFor(() => {
      expect(fake.getCalls).toEqual([O3]);
    });
    await new Promise((r) => setTimeout(r, 30));
    expect(fake.listCalls.length).toBe(before);

    fake.orders.set(O3, order({ id: O3, code: "DH0001300", label: "new", status: "new" }));
    before = fake.listCalls.length;
    act(() => {
      onRealtimeChanged(qc, changed(O3));
    });
    await waitFor(() => {
      expect(fake.listCalls.length).toBeGreaterThan(before);
    });
    expect(fake.listCalls.at(-1)?.get("q")).toBeNull();
  });

  it("nhãn không có đơn: câu riêng theo nhãn; tìm không ra: nút Tìm trong mọi ngày bỏ lọc ngày", async () => {
    fake.list = [];
    renderAt("/don");
    expect(await screen.findByText('Không có đơn "Mới"')).toBeTruthy();
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm đơn" }), { target: { value: "DH0009999" } });
    expect(await screen.findByText("Không tìm thấy đơn nào")).toBeTruthy();
    expect(screen.getByText('Không có đơn khớp "DH0009999" trong 7 ngày qua.')).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tìm trong mọi ngày" }));
    await waitFor(() => {
      const last = fake.listCalls.at(-1);
      expect(last?.get("q")).toBe("DH0009999");
      expect(last?.get("createdFrom")).toBeNull();
    });
  });

  it("Tải thêm 50 đơn gửi con trỏ của trang trước; hết trang thì không còn nút", async () => {
    fake.listNextCursor = "c2";
    renderAt("/don");
    fireEvent.click(await screen.findByRole("button", { name: "Tải thêm 50 đơn" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("cursor")).toBe("c2");
    });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Tải thêm 50 đơn" })).toBeNull();
    });
  });

  it("lọc nghi trùng, nguồn; số cặp đơn trùng ở nút đầu trang; bấm dòng mở chi tiết", async () => {
    fake.duplicates = [
      { order: listItem(), otherOrder: listItem({ id: O2 }), matchedBy: "phone", createdAt: "2026-11-18T07:31:00Z" },
    ];
    fake.orders.set(O1, order());
    renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    expect(await screen.findByRole("link", { name: "Đơn trùng (1)" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Nghi trùng" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("duplicateSuspect")).toBe("true");
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Nguồn đơn" }), {
      target: { value: fake.list[0]?.trafficSourceId },
    });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.getAll("trafficSourceId")).toEqual([fake.list[0]?.trafficSourceId]);
    });
    fireEvent.click(within(table()).getAllByRole("row")[1] ?? table());
    expect(await screen.findByRole("heading", { name: "DH0001235" })).toBeTruthy();
  });

  // Review độc lập M2, D-03: màn mở qua nửa đêm (giờ Việt Nam) thì "Hôm nay" chuyển sang ngày mới ở lần tải lại kế tiếp.
  it("lọc Hôm nay theo ngày hiện tại khi màn mở qua nửa đêm", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-11-19T16:59:50Z")); // 23:59:50 ngày 19/11 giờ Việt Nam
    renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    fireEvent.change(screen.getByRole("combobox", { name: "Ngày tạo" }), { target: { value: "today" } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("createdFrom")).toBe("2026-11-18T17:00:00.000Z");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_500);
    });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("createdFrom")).toBe("2026-11-19T17:00:00.000Z");
    });
  });

  it("tự tải lại cả trang mỗi 20 giây (khi không có báo hiệu)", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderAt("/don");
    await screen.findByRole("row", { name: /DH0001236/ });
    const before = fake.listCalls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_500);
    });
    expect(fake.listCalls.length).toBeGreaterThan(before);
    expect(fake.listCalls.at(-1)?.get("q")).toBeNull();
  });
});
