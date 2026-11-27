// @vitest-environment jsdom
// Danh sách chi tiết của một ô số (Task 239, M6-16; hành vi (a); mockup tong-quan.html trạng thái D, E): đầu bảng "N đơn"
// bằng `total` của máy chủ, tải thêm theo con trỏ, bấm mã đơn mở chi tiết đơn, chỉ mã (không tên/SĐT/địa chỉ khách).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Cell } from "./cells";
import { DrilldownPanel } from "./DrilldownPanel";
import { LONG_NAME, fakeReports, orderRow, stubReports } from "./reportsTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function mount(f: ReturnType<typeof fakeReports>, cell: Cell = { metric: "pending_confirm" }, onClose = vi.fn()) {
  stubReports(f);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <DrilldownPanel cell={cell} period={{ from: "2026-11-27", to: "2026-11-27" }} onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return onClose;
}

describe("DrilldownPanel", () => {
  it("Chờ xác nhận: đầu bảng '1 đơn', dòng O4 là liên kết tới chi tiết đơn, gọi đúng tham số", async () => {
    const f = fakeReports();
    mount(f);
    expect(await screen.findByRole("heading", { name: "Chờ xác nhận · 1 đơn" })).toBeTruthy();
    const link = screen.getByRole("link", { name: "DH0004804" });
    expect(link.getAttribute("href")).toMatch(/^\/don\/0195f3b2-/);
    expect(screen.getByText("KH0012044")).toBeTruthy();
    const q = new URLSearchParams(f.gets[0]?.split("?")[1]);
    expect(q.get("metric")).toBe("pending_confirm");
    expect(q.get("from")).toBe("2026-11-27");
    expect(q.get("to")).toBe("2026-11-27");
    expect(q.has("cellDimension")).toBe(false);
  });

  it("ô của một dòng (kênh Facebook, Đơn lên) gửi cellDimension + cellKey + metric", async () => {
    const f = fakeReports({ drill: { placed: [orderRow(1)] } });
    mount(f, { metric: "placed", dimension: "channel", key: "facebook" });
    expect(await screen.findByRole("heading", { name: "Đơn lên · Facebook · 1 đơn" })).toBeTruthy();
    const q = new URLSearchParams(f.gets[0]?.split("?")[1]);
    expect(q.get("cellDimension")).toBe("channel");
    expect(q.get("cellKey")).toBe("facebook");
    expect(q.get("metric")).toBe("placed");
  });

  it("412 đơn: hiện 200 đầu, 'Đang hiện 200 / 412 đơn', Xem thêm gửi con trỏ rồi hiện 400", async () => {
    const all = Array.from({ length: 412 }, (_, i) => orderRow(i + 1, { code: `DH${String(100000 + i)}` }));
    const f = fakeReports({ drill: { placed: all } });
    mount(f, { metric: "placed" });
    expect(await screen.findByRole("heading", { name: "Đơn lên · 412 đơn" })).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText("Đang hiện 200 / 412 đơn")).toBeTruthy();
    });
    expect(new URLSearchParams(f.gets[0]?.split("?")[1]).get("limit")).toBe("200");
    fireEvent.click(screen.getByRole("button", { name: "Xem thêm" }));
    await waitFor(() => {
      expect(screen.getByText("Đang hiện 400 / 412 đơn")).toBeTruthy();
    });
    expect(f.gets.length).toBe(2);
    expect(new URLSearchParams(f.gets[1]?.split("?")[1]).get("cursor")).toBe("200");
    expect(screen.getAllByRole("row").length).toBe(401); // đầu bảng + 400 dòng
  });

  it("tên nhân viên 120 ký tự cắt bằng dấu ba chấm (class nm), đủ tên ở title", async () => {
    const f = fakeReports({ drill: { placed: [orderRow(5, { careByName: LONG_NAME })] } });
    mount(f, { metric: "placed" });
    const nm = await screen.findByTitle(LONG_NAME);
    expect(nm.className).toContain("nm");
    expect(nm.textContent).toBe(LONG_NAME);
  });

  it("đơn chưa có người phụ trách → 'Chưa phân công'; trạng thái hiện chữ tiếng Việt", async () => {
    const f = fakeReports({
      drill: { placed: [orderRow(6, { careByName: undefined, statusLabel: "awaiting_pickup" })] },
    });
    mount(f, { metric: "placed" });
    expect(await screen.findByText("Chưa phân công")).toBeTruthy();
    expect(screen.getByText("Chờ thu gom")).toBeTruthy();
  });

  it("0 đơn → trạng thái trống; chỉ mã, không cột tên, SĐT, địa chỉ khách", async () => {
    const f = fakeReports({ drill: {} });
    mount(f, { metric: "cancelled" });
    expect(await screen.findByRole("heading", { name: "Đơn hủy · 0 đơn" })).toBeTruthy();
    expect(screen.getByText("Không có đơn nào trong ô này.")).toBeTruthy();
    const head = within(document.body)
      .queryAllByRole("columnheader")
      .map((h) => h.textContent);
    for (const bad of ["Tên khách", "SĐT", "Điện thoại", "Địa chỉ"]) expect(head).not.toContain(bad);
  });

  it("lỗi tải → câu tiếng Việt và nút Thử lại; nút ✕ gọi onClose", async () => {
    const f = fakeReports();
    stubReports(f);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(Response.json({ error: "forbidden", request_id: "x" }, { status: 403 }))),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const onClose = vi.fn();
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <DrilldownPanel
            cell={{ metric: "placed" }}
            period={{ from: "2026-11-27", to: "2026-11-27" }}
            onClose={onClose}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Bạn chưa được cấp quyền làm việc này")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Đóng" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
