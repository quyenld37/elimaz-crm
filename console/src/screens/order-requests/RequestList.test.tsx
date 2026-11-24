// @vitest-environment jsdom
// Danh sách Yêu cầu về đơn hàng /don/yeu-cau (kế hoạch M5 Task 207, Task 486; mockup docs/mockups/m5/order-requests.html
// trạng thái A, F): mặc định "Đang mở", số trên nhãn, cột Khách và Ảnh; lọc trạng thái, loại, tìm; 200 yêu cầu tải theo
// con trỏ; báo hiệu order_request.changed tải lại; lỗi 403 không vẽ như danh sách rỗng.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { LONG_NAME, RQ1, installFake, renderAt, summary, type Fake } from "./requestTestkit";
import { screens } from "./route";

const OPEN = ["received", "in_review", "needs_info", "confirmed", "items_received"];

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.rows = [
    summary(),
    summary({
      id: "r2",
      ref: "YC0000147",
      kind: "cancel",
      reasonCode: "CHANGED_MIND",
      orderCode: "DH0001309",
      customerName: LONG_NAME,
      photoCount: 0,
    }),
    summary({ id: "r3", ref: "YC0000145", kind: "return", status: "in_review", reasonCode: "QUALITY" }),
    summary({ id: "r4", ref: "YC0000143", status: "needs_info", photoCount: 0 }),
    summary({ id: "r5", ref: "YC0000141", status: "confirmed", shipBackDeadline: "2026-11-25T16:59:59Z" }),
    summary({ id: "r6", ref: "YC0000139", status: "items_received" }),
    summary({ id: "r7", ref: "YC0000131", kind: "cancel", status: "resolved", reasonCode: "DUPLICATE" }),
    summary({ id: "r8", ref: "YC0000128", status: "rejected" }),
    summary({
      id: "r9",
      ref: "YC0000125",
      kind: "question",
      status: "cancelled",
      photoCount: 0,
      customerName: undefined,
    }),
  ];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const row = (ref: string) => screen.findByRole("row", { name: new RegExp(ref) });

describe("Danh sách yêu cầu", () => {
  it("mỗi dòng: mã yêu cầu dẫn tới chi tiết, loại, đơn dẫn tới trang đơn, khách, trạng thái, ảnh; đóng thì mờ", async () => {
    renderAt("/don/yeu-cau");
    fireEvent.click(await screen.findByRole("button", { name: /^Tất cả/ }));
    const r1 = await row("YC0000148");
    expect(within(r1).getByRole("link", { name: "YC0000148" }).getAttribute("href")).toBe(`/don/yeu-cau/${RQ1}`);
    expect(within(r1).getByText("Đổi hàng")).toBeTruthy();
    expect(within(r1).getByRole("link", { name: "DH0001312" }).getAttribute("href")).toBe(`/don/${fake.order.id}`);
    expect(within(r1).getByText("Trần Thị Thu Hà")).toBeTruthy();
    expect(within(r1).getByText("Đã tiếp nhận")).toBeTruthy();
    expect(within(r1).getByRole("cell", { name: "3" })).toBeTruthy();
    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Yêu cầu",
      "Đơn",
      "Khách",
      "Trạng thái",
      "Ảnh",
      "Cập nhật",
    ]);
    // Tên khách 60 ký tự không dấu cách: xuống dòng được. Hủy đơn, hỏi về đơn không có ảnh → "—"; đổi hàng 0 ảnh → 0.
    const r2 = await row("YC0000147");
    expect(within(r2).getByText(LONG_NAME).className).toContain("wrap-any");
    expect(within(r2).getByText("Hủy đơn")).toBeTruthy();
    expect(within(r2).getByRole("cell", { name: "—" })).toBeTruthy();
    expect(within(await row("YC0000143")).getByRole("cell", { name: "0" })).toBeTruthy();
    expect(within(await row("YC0000125")).getAllByRole("cell", { name: "—" })).toHaveLength(2);
    expect(within(await row("YC0000145")).getByText("Trả hàng")).toBeTruthy();
    // Đã xác nhận đổi kèm hạn gửi hàng về (giờ Việt Nam).
    expect(within(await row("YC0000141")).getByText("Đã xác nhận đổi · gửi trước 25/11")).toBeTruthy();
    const states: [string, string][] = [
      ["YC0000145", "Đang kiểm tra"],
      ["YC0000143", "Cần bổ sung"],
      ["YC0000139", "Đã nhận hàng gửi về"],
      ["YC0000131", "Hoàn tất"],
      ["YC0000128", "Từ chối"],
      ["YC0000125", "Khách đã hủy"],
    ];
    for (const [ref, text] of states) {
      expect(within(await row(ref)).getByText(text)).toBeTruthy();
    }
    expect((await row("YC0000131")).className).toContain("is-muted");
    expect((await row("YC0000148")).className).not.toContain("is-muted");
  });

  it("thanh nhãn có số theo trạng thái (đếm cả shop); mặc định Đang mở gửi statuses của 5 trạng thái chưa đóng", async () => {
    renderAt("/don/yeu-cau");
    await row("YC0000148");
    const open = screen.getByRole("button", { name: "Đang mở 6" });
    expect(open.getAttribute("aria-pressed")).toBe("true");
    expect(fake.listCalls[0]?.getAll("statuses")).toEqual(OPEN);
    expect(fake.listCalls[0]?.get("status")).toBeNull();
    expect(screen.queryByRole("row", { name: /YC0000131/ })).toBeNull();
    for (const name of ["Đã tiếp nhận 2", "Đang kiểm tra 1", "Hoàn tất 1", "Khách đã hủy 1", "Tất cả 9"]) {
      expect(screen.getByRole("button", { name })).toBeTruthy();
    }
  });

  it("không đọc được số: nhãn không có số, danh sách vẫn hiện", async () => {
    fake.countError = { status: 500, body: { error: "internal", request_id: "t" } };
    renderAt("/don/yeu-cau");
    await row("YC0000148");
    expect(screen.getByRole("button", { name: "Đang mở" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Tất cả" })).toBeTruthy();
  });

  it("lọc trạng thái bằng thanh nhãn, lọc loại, tìm gửi status, kind, q lên API", async () => {
    renderAt("/don/yeu-cau");
    await row("YC0000148");
    fireEvent.click(screen.getByRole("button", { name: /^Tất cả/ }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.getAll("statuses")).toEqual([]);
    });
    expect(fake.listCalls.at(-1)?.get("status")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Cần bổ sung/ }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("status")).toBe("needs_info");
    });
    expect(fake.listCalls.at(-1)?.getAll("statuses")).toEqual([]);
    expect(screen.getByRole("button", { name: /^Cần bổ sung/ }).getAttribute("aria-pressed")).toBe("true");
    expect(await row("YC0000143")).toBeTruthy();
    expect(screen.queryByRole("row", { name: /YC0000148/ })).toBeNull();
    fireEvent.change(screen.getByRole("combobox", { name: "Loại yêu cầu" }), { target: { value: "return" } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("kind")).toBe("return");
    });
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm yêu cầu" }), { target: { value: " nguyen " } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("q")).toBe("nguyen");
    });
    expect(fake.listCalls.at(-1)?.get("status")).toBe("needs_info");
  });

  it("200 yêu cầu: tải 50 dòng mỗi lần theo con trỏ; còn nextCursor thì còn nút Tải thêm", async () => {
    fake.rows = Array.from({ length: 200 }, (_, i) => summary({ id: `x${String(i)}`, ref: `YC${String(10000 + i)}` }));
    renderAt("/don/yeu-cau");
    await row("YC10000");
    expect(screen.getAllByRole("row")).toHaveLength(51);
    expect(screen.getByText("50 yêu cầu, còn nữa")).toBeTruthy();
    for (let n = 100; n <= 200; n += 50) {
      fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
      await waitFor(() => {
        expect(screen.getAllByRole("row")).toHaveLength(n + 1);
      });
    }
    expect(fake.listCalls.map((q) => q.get("cursor"))).toEqual([null, "50", "100", "150"]);
    expect(fake.listCalls.every((q) => q.get("limit") === "50")).toBe(true);
    expect(screen.getByText("200 yêu cầu")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Tải thêm" })).toBeNull();
  });

  it("báo hiệu order_request.changed tải lại danh sách và số: yêu cầu mới tự hiện", async () => {
    const { qc } = renderAt("/don/yeu-cau");
    await row("YC0000148");
    fake.rows = [summary({ id: "new", ref: "YC0000149" }), ...fake.rows];
    act(() => {
      onRealtimeChanged(qc, { topic: "order_request.changed", entity: { type: "order_request", id: "new" } });
    });
    expect(await row("YC0000149")).toBeTruthy();
    expect(await screen.findByRole("button", { name: "Đang mở 7" })).toBeTruthy();
  });

  it("lỗi 403 hiện câu lỗi, không vẽ như danh sách rỗng", async () => {
    fake.listError = { status: 403, body: { error: "forbidden", request_id: "t" } };
    renderAt("/don/yeu-cau");
    const box = await screen.findByText(/Chưa tải được danh sách yêu cầu/);
    expect(box).toBeTruthy();
    expect(screen.queryByText(/Chưa có yêu cầu/)).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });

  it("rỗng: Đang mở → không có yêu cầu đang mở; lọc khác → không khớp; Tất cả → chưa có yêu cầu", async () => {
    fake.rows = [summary({ id: "done", ref: "YC0000100", status: "resolved" })];
    renderAt("/don/yeu-cau");
    expect(await screen.findByText("Không có yêu cầu đang mở")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Xem tất cả yêu cầu" }));
    expect(await row("YC0000100")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Tất cả/ }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: /^Từ chối/ }));
    expect(await screen.findByText("Không có yêu cầu nào khớp")).toBeTruthy();
    fake.rows = [];
    fireEvent.click(screen.getByRole("button", { name: "Xem tất cả yêu cầu" }));
    expect(await screen.findByText("Chưa có yêu cầu nào")).toBeTruthy();
  });

  it("số cạnh mục menu (HV02) là số yêu cầu đang mở; báo hiệu tải lại; không còn yêu cầu mở → không hiện số", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={qc}>{screens[0]?.badge}</QueryClientProvider>);
    expect(await screen.findByText("6")).toBeTruthy();
    expect(screen.getByText(", 6 việc")).toBeTruthy();
    fake.rows = fake.rows.filter((r) => r.status === "resolved");
    act(() => {
      onRealtimeChanged(qc, { topic: "order_request.changed", entity: { type: "order_request", id: "r1" } });
    });
    await waitFor(() => {
      expect(screen.queryByText("6")).toBeNull();
    });
    expect(fake.countReads).toBe(2);
  });
});
