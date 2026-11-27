// @vitest-environment jsdom
// Cần gọi lại (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái A–C; VC07, KH09).
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  AN,
  DUC,
  LONG_NAME,
  ME,
  PHONE,
  WORKER,
  fakeLayout,
  grantsOf,
  installFake,
  renderAt,
  role,
  staff,
  stubDialog,
  task,
  taskId,
} from "./opsTestkit";

beforeEach(() => {
  fakeLayout();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const rowsOf = () => document.querySelectorAll(".cb-row");
const rowOf = async (code: string) => {
  const link = await screen.findByRole("link", { name: code });
  const row = link.closest<HTMLElement>(".cb-row");
  if (row === null) throw new Error("không thấy dòng");
  return row;
};

/** Mở hộp "Ghi kết quả" của dòng; trả hộp và nút Lưu kết quả. */
async function openOutcome(row: HTMLElement) {
  fireEvent.click(within(row).getByRole("button", { name: "Ghi kết quả" }));
  const dlg = await screen.findByRole("dialog", { hidden: true });
  return { dlg, save: within(dlg).getByRole<HTMLButtonElement>("button", { name: "Lưu kết quả", hidden: true }) };
}

describe("Cần gọi lại", () => {
  it("việc quá hạn đứng đầu có nhãn 'Quá hạn 3 giờ'; việc chưa quá hạn không có nhãn; tên dài không tràn", async () => {
    installFake({
      tasks: [
        task(1, -3, { customerName: LONG_NAME }),
        task(2, -1.5, { assignee: { id: AN, name: "Lê Văn Đức" } }),
        task(3, 6),
      ],
    });
    renderAt("/ops/callbacks");
    const first = await rowOf("DH0001001");
    expect(rowsOf()[0]).toBe(first);
    expect(within(first).getByText("Quá hạn 3 giờ")).toBeTruthy();
    expect(within(first).getByText("Chưa ai nhận")).toBeTruthy();
    expect(within(first).getByText(LONG_NAME).className).toContain("ellip-2");
    expect(within(first).getByText("Nhận 3 · Hoàn 2 (40%)")).toBeTruthy();
    expect(within(first).getByText("Hay hoàn")).toBeTruthy();
    const third = await rowOf("DH0001003");
    expect(within(third).queryByText(/Quá hạn/)).toBeNull();
  });

  it("200 việc: chỉ dựng dưới 30 dòng, cuộn tới cuối thì tải trang kế bằng con trỏ", async () => {
    const fake = installFake({ tasks: Array.from({ length: 200 }, (_, i) => task(i, i + 1)) });
    renderAt("/ops/callbacks");
    await screen.findByText(/200 việc mở/);
    expect(screen.getByText(/đang hiện 50, cuộn để tải tiếp/)).toBeTruthy();
    expect(rowsOf().length).toBeGreaterThan(0);
    expect(rowsOf().length).toBeLessThan(30);
    const list = screen.getByTestId("cblist");
    act(() => {
      list.scrollTop = 50 * 120;
      fireEvent.scroll(list);
    });
    await waitFor(() => {
      expect(fake.listCalls.some((q) => q.get("cursor") === "50")).toBe(true);
    });
    await screen.findByText(/đang hiện 100/);
    expect(rowsOf().length).toBeLessThan(30);
  });

  it("SĐT không có sẵn trong danh sách; 'Hiện số' đọc đơn rồi 'Gọi' là liên kết tel:", async () => {
    const fake = installFake({ tasks: [task(1, -3)] });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    expect(document.body.textContent).not.toContain("0912");
    expect(fake.orderReads).toHaveLength(0);
    fireEvent.click(within(row).getByRole("button", { name: "Hiện số" }));
    const call = await within(row).findByRole("link", { name: "Gọi 0912 888 000" });
    expect(call.getAttribute("href")).toBe(`tel:${PHONE}`);
    expect(fake.orderReads).toHaveLength(1);
  });

  it("thiếu quyền xem đơn: nút 'Hiện số' mờ; thiếu quyền gọi lại: không có nút thao tác", async () => {
    installFake({ tasks: [task(1, -3)], grants: grantsOf("fulfillment.callbacks.work") });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    expect(within(row).getByRole<HTMLButtonElement>("button", { name: "Hiện số" }).disabled).toBe(true);
    cleanup();
    installFake({ tasks: [task(1, -3)], grants: grantsOf("fulfillment.read") });
    renderAt("/ops/callbacks");
    const row2 = await rowOf("DH0001001");
    expect(within(row2).queryByRole("button")).toBeNull();
  });

  it("ghi 'Không nghe máy' hẹn 15:00 → gửi version và giờ; dòng sang 'Đang xử lý · gọi lại 15:00'", async () => {
    const fake = installFake({ tasks: [task(1, -3)] });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    const { dlg, save } = await openOutcome(row);
    expect(save.disabled).toBe(true);
    expect(within(dlg).getByText("Chọn kết quả cuộc gọi")).toBeTruthy();
    fireEvent.click(within(dlg).getByLabelText("Không nghe máy"));
    fireEvent.change(within(dlg).getByLabelText("Gọi lại lúc"), { target: { value: "2026-11-23T15:00" } });
    fireEvent.change(within(dlg).getByLabelText("Ghi chú"), { target: { value: "gọi lúc chiều" } });
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    const w = fake.writes[0];
    expect(w?.path).toBe(`/api/v1/callback-tasks/${taskId(1)}/outcomes`);
    expect(w?.key).toBeTruthy();
    expect(w?.body).toEqual({
      outcome: "no_answer",
      version: 1,
      note: "gọi lúc chiều",
      nextCallAt: "2026-11-23T08:00:00.000Z",
    });
    const after = await rowOf("DH0001001");
    await within(after).findByText("Đang xử lý · gọi lại 15:00");
  });

  it("'Khách hẹn gọi lại' đòi giờ; 'Giao lại tới địa chỉ mới' đòi địa chỉ, tỉnh, xã", async () => {
    installFake({ tasks: [task(1, -3)] });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    const { dlg, save } = await openOutcome(row);
    fireEvent.click(within(dlg).getByLabelText("Khách hẹn gọi lại"));
    expect(within(dlg).getByText("Chọn giờ gọi lại")).toBeTruthy();
    expect(save.disabled).toBe(true);
    fireEvent.click(within(dlg).getByLabelText("Giao lại tới địa chỉ mới"));
    await waitFor(() => {
      expect(within(dlg).getByText("Nhập địa chỉ mới")).toBeTruthy();
    });
    expect(save.disabled).toBe(true);
  });

  it("409 version_conflict: giữ hộp, hiện câu tiếng Việt, tải lại danh sách", async () => {
    const fake = installFake({ tasks: [task(1, -3)] });
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    const before = fake.listCalls.length;
    fireEvent.click(within(row).getByRole("button", { name: "Ghi kết quả" }));
    const dlg = await screen.findByRole("dialog", { hidden: true });
    fireEvent.click(within(dlg).getByLabelText("Khách nhận, giao lại"));
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu kết quả", hidden: true }));
    await within(dlg).findByText(/Người khác vừa sửa mục này/);
    await waitFor(() => {
      expect(fake.listCalls.length).toBeGreaterThan(before);
    });
  });

  it("'Nhận việc' giao cho chính mình kèm version; 'Giao lại' chỉ hiện cho người quản lý nhân viên", async () => {
    const fake = installFake({ tasks: [task(1, -3)] });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    expect(within(row).queryByRole("button", { name: "Giao lại" })).toBeNull();
    fireEvent.click(within(row).getByRole("button", { name: "Nhận việc" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ assigneeId: ME, version: 1 });
    const after = await rowOf("DH0001001");
    await within(after).findByText("Tôi (bạn)");
  });

  it("'Giao lại' chỉ liệt kê nhân viên đang hoạt động có vai trò gọi lại khách", async () => {
    const fake = installFake({
      tasks: [task(1, -3, { assignee: { id: DUC, name: "Lê Văn Đức" } })],
      grants: grantsOf(...WORKER, "identity.users.manage"),
      roles: [role("sale", ["fulfillment.callbacks.work"]), role("warehouse", ["inventory.read"])],
      staff: [
        staff(AN, "Trần Minh An", "sale"),
        staff(DUC, "Lê Văn Đức", "warehouse"),
        staff("0195f3b2-0000-7000-8000-0000000000e4", "Đã khóa", "sale", "locked"),
      ],
    });
    renderAt("/ops/callbacks");
    const row = await rowOf("DH0001001");
    fireEvent.click(await within(row).findByRole("button", { name: "Giao lại" }));
    const dlg = await screen.findByRole("dialog", { hidden: true });
    const select = await within(dlg).findByLabelText("Người nhận việc *");
    const names = within(select)
      .getAllByRole("option", { hidden: true })
      .map((o) => o.textContent);
    expect(names).toEqual(["Chọn nhân viên", "Trần Minh An"]);
    fireEvent.change(select, { target: { value: AN } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Giao", hidden: true }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ assigneeId: AN, version: 1 });
  });

  it("lỗi 403 hiện câu lỗi, không vẽ như danh sách rỗng; danh sách rỗng có câu riêng", async () => {
    const fake = installFake({ tasks: [] });
    fake.failReads.push(/callback-tasks/);
    renderAt("/ops/callbacks");
    await screen.findByRole("alert");
    expect(screen.queryByText("Không có đơn cần gọi lại")).toBeNull();
    cleanup();
    installFake({ tasks: [] });
    renderAt("/ops/callbacks");
    await screen.findByText("Không có đơn cần gọi lại");
  });

  it("còn đúng 1 việc: hiện một dòng; số trên thanh tab theo báo hiệu callback_task.changed", async () => {
    const fake = installFake({
      tasks: [task(1, 5)],
      counts: { review: 6, stuck: 4, callbacksOpen: 1, callbacksOverdue: 0, inDoubt: 0 },
    });
    const { qc } = renderAt("/ops/callbacks");
    await rowOf("DH0001001");
    const tabs = await screen.findByRole("navigation", { name: "Danh sách vận hành" });
    await within(tabs).findByText("4");
    expect(within(tabs).getByText("6")).toBeTruthy();
    fake.counts = { review: 6, stuck: 4, callbacksOpen: 12, callbacksOverdue: 3, inDoubt: 0 };
    act(() => {
      onRealtimeChanged(qc, { topic: "callback_task.changed", entity: { type: "callback_task", id: "x" } });
    });
    await within(tabs).findByText("12 · 3 quá hạn");
  });
});
