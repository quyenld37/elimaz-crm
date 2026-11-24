// @vitest-environment jsdom
// Danh sách Yêu cầu về đơn hàng /don/yeu-cau (kế hoạch M5 Task 207; mockup docs/mockups/m5/order-requests.html trạng
// thái A, F): lọc trạng thái, loại, tìm; 200 yêu cầu tải theo con trỏ; báo hiệu order_request.changed tải lại; lỗi 403
// không vẽ như danh sách rỗng.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { LONG_NAME, RQ1, installFake, renderAt, summary, type Fake } from "./requestTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.rows = [
    summary(),
    summary({ id: "r2", ref: "YC0000147", kind: "cancel", reasonCode: "CHANGED_MIND", orderCode: "DH0001309" }),
    summary({ id: "r3", ref: "YC0000145", kind: "return", status: "in_review", reasonCode: "QUALITY" }),
    summary({ id: "r4", ref: "YC0000143", status: "needs_info" }),
    summary({ id: "r5", ref: "YC0000141", status: "confirmed" }),
    summary({ id: "r6", ref: "YC0000139", status: "items_received" }),
    summary({ id: "r7", ref: "YC0000131", kind: "cancel", status: "resolved", reasonCode: "DUPLICATE" }),
    summary({ id: "r8", ref: "YC0000128", status: "rejected" }),
    summary({ id: "r9", ref: "YC0000125", kind: "question", status: "cancelled", reasonCode: LONG_NAME }),
  ];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const row = (ref: string) => screen.findByRole("row", { name: new RegExp(ref) });

describe("Danh sách yêu cầu", () => {
  it("mỗi dòng: mã yêu cầu dẫn tới chi tiết, loại, đơn dẫn tới trang đơn, lý do bằng chữ, trạng thái; đóng thì mờ", async () => {
    renderAt("/don/yeu-cau");
    const r1 = await row("YC0000148");
    expect(within(r1).getByRole("link", { name: "YC0000148" }).getAttribute("href")).toBe(`/don/yeu-cau/${RQ1}`);
    expect(within(r1).getByText("Đổi hàng")).toBeTruthy();
    expect(within(r1).getByRole("link", { name: "DH0001312" }).getAttribute("href")).toBe(`/don/${fake.order.id}`);
    expect(within(r1).getByText("Không vừa size")).toBeTruthy();
    expect(within(r1).getByText("Đã tiếp nhận")).toBeTruthy();
    expect(within(await row("YC0000147")).getByText("Hủy đơn")).toBeTruthy();
    expect(within(await row("YC0000147")).getByText("Đổi ý")).toBeTruthy();
    expect(within(await row("YC0000145")).getByText("Trả hàng")).toBeTruthy();
    const states: [string, string][] = [
      ["YC0000145", "Đang kiểm tra"],
      ["YC0000143", "Cần bổ sung"],
      ["YC0000141", "Đã xác nhận đổi"],
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
    // Mã lý do lạ (không có trong bảng chữ) hiện nguyên, xuống dòng được.
    const odd = within(await row("YC0000125")).getByText(LONG_NAME);
    expect(odd.className).toContain("wrap-any");
  });

  it("lọc trạng thái bằng thanh nhãn, lọc loại, tìm gửi status, kind, q lên API", async () => {
    renderAt("/don/yeu-cau");
    await row("YC0000148");
    expect(fake.listCalls[0]?.get("status")).toBeNull();
    const all = screen.getByRole("button", { name: "Tất cả" });
    expect(all.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Cần bổ sung" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("status")).toBe("needs_info");
    });
    expect(screen.getByRole("button", { name: "Cần bổ sung" }).getAttribute("aria-pressed")).toBe("true");
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

  it("báo hiệu order_request.changed tải lại danh sách: yêu cầu mới tự hiện", async () => {
    const { qc } = renderAt("/don/yeu-cau");
    await row("YC0000148");
    fake.rows = [summary({ id: "new", ref: "YC0000149" }), ...fake.rows];
    act(() => {
      onRealtimeChanged(qc, { topic: "order_request.changed", entity: { type: "order_request", id: "new" } });
    });
    expect(await row("YC0000149")).toBeTruthy();
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

  it("rỗng: chưa lọc → chưa có yêu cầu; đang lọc → không có yêu cầu khớp, có nút xem tất cả", async () => {
    fake.rows = [];
    renderAt("/don/yeu-cau");
    expect(await screen.findByText("Chưa có yêu cầu nào")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Hoàn tất" }));
    expect(await screen.findByText("Không có yêu cầu nào khớp")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Xem tất cả yêu cầu" }));
    expect(await screen.findByText("Chưa có yêu cầu nào")).toBeTruthy();
  });
});
