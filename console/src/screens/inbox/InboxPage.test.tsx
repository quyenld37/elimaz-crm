// @vitest-environment jsdom
// Màn Inbox (Task 174; mockup docs/mockups/m4/inbox.html trạng thái A–C; HV03, HV04, HV06, HV07 đã sửa): bộ lọc trên
// URL và nhớ trên máy, "Của tôi" (CH06), lọc nhãn (CH07), số chưa đọc theo tài khoản kênh tăng khi có tin (CH01), băng
// mất kết nối có "Tải lại ngay", âm báo tắt mặc định, trạng thái trống, mở hội thoại.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { renderScreens } from "../../app/screenTestkit";
import { RealtimeStateContext } from "../../shell/realtimeState";
import { FILTERS_STORAGE_KEY, SOUND_STORAGE_KEY } from "./inboxRules";
import { ME, P1, P2, account, all, conv, convId, inboxFake, stubInbox, tag, type InboxFake } from "./inboxTestkit";
import { screens } from "./route";
import { CONVERSATION_TOPIC } from "./useConversationList";

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const lastList = (f: InboxFake) => f.listCalls.at(-1) ?? new URLSearchParams();

function fakeWithRows(extra: Partial<InboxFake> = {}) {
  return stubInbox(
    inboxFake({
      rows: [
        conv(1, { customer: { id: "c1", name: "Phạm Thu Trang" }, unreadCount: 1, pageId: P2 }),
        conv(2, { customer: { id: "c2", name: "Lê Hà My" }, assignee: { id: ME, name: "Trần Minh An" } }),
      ],
      counts: {
        byPage: [
          { pageId: P1, unread: 120, open: 150 },
          { pageId: P2, unread: 9, open: 50 },
        ],
        mine: 1,
        unassigned: 1,
      },
      tags: [tag(1, { name: "Hỏi size" }), tag(2, { name: "Đã lên đơn" })],
      ...extra,
    }),
  );
}

it("danh sách tài khoản kênh: Tất cả tài khoản 99+, mỗi Page số chưa đọc (99+, 9); bấm Page → lọc pageId, URL có page", async () => {
  const f = fakeWithRows();
  const { router } = renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  const nav = within(screen.getByRole("navigation", { name: "Tài khoản kênh" }));
  const allBtn = nav.getByRole("button", { name: /Tất cả tài khoản/ });
  expect(allBtn.getAttribute("aria-pressed")).toBe("true");
  expect(within(allBtn).getByText("99+").getAttribute("aria-label")).toBe("129 chưa đọc");
  const sale = nav.getByRole("button", { name: /ELIMAZ Sale/ });
  expect(within(sale).getByText("9")).toBeTruthy();
  expect(within(sale).getByText("FB")).toBeTruthy();
  fireEvent.click(sale);
  await waitFor(() => {
    expect(lastList(f).get("pageId")).toBe(P2);
  });
  expect(router.state.location.search).toContain(`page=${P2}`);
});

it("chọn Của tôi → yêu cầu có assignee=me, URL có tham số, Bỏ lọc hiện; chọn nhãn → tagId", async () => {
  const f = fakeWithRows();
  const { router } = renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  expect(screen.queryByRole("button", { name: "Bỏ lọc" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Người phụ trách"), { target: { value: "me" } });
  await waitFor(() => {
    expect(lastList(f).get("assignee")).toBe("me");
  });
  expect(router.state.location.search).toContain("assignee=me");
  await waitFor(() => {
    expect(screen.queryByText("Phạm Thu Trang")).toBeNull();
  });
  expect(screen.getByText("Lê Hà My")).toBeTruthy();
  expect(screen.getAllByRole("button", { name: "Bỏ lọc" }).length).toBeGreaterThan(0);

  fireEvent.change(screen.getByLabelText("Nhãn"), { target: { value: tag(1).id } });
  await waitFor(() => {
    expect(lastList(f).get("tagId")).toBe(tag(1).id);
  });
  expect(lastList(f).get("assignee")).toBe("me");
});

it("HV06: bộ lọc nhớ trên máy; mở /inbox không tham số → dùng bộ lọc đã lưu; Bỏ lọc về mặc định", async () => {
  const f = fakeWithRows();
  const first = renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  fireEvent.click(screen.getByRole("button", { name: "Chỉ chưa đọc" }));
  await waitFor(() => {
    expect(lastList(f).get("unread")).toBe("true");
  });
  expect(localStorage.getItem(FILTERS_STORAGE_KEY)).toContain("unread=1");
  first.unmount();

  f.listCalls.length = 0;
  const second = renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  expect(f.listCalls.every((q) => q.get("unread") === "true")).toBe(true);
  expect(second.router.state.location.search).toContain("unread=1");
  expect(screen.getByRole("button", { name: "Chỉ chưa đọc" }).getAttribute("aria-pressed")).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "Bỏ lọc" }));
  await waitFor(() => {
    expect(lastList(f).get("unread")).toBeNull();
  });
  expect(second.router.state.location.search).toBe("");
  expect(lastList(f).get("status")).toBe("open");
});

it("CH01: báo hiệu tin mới của X → X lên đầu, số chưa đọc của X và của Page tăng", async () => {
  const f = fakeWithRows();
  const { qc, container } = renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  const names = () => [...container.querySelectorAll(".cv-n")].map((n) => n.textContent);
  expect(names()).toEqual(["Phạm Thu Trang", "Lê Hà My"]);

  const x = convId(2);
  f.rows = f.rows.map((r) => (r.id === x ? { ...r, lastMessageAt: new Date().toISOString(), unreadCount: 1 } : r));
  f.counts = {
    ...f.counts,
    byPage: [
      { pageId: P1, unread: 121, open: 150 },
      { pageId: P2, unread: 9, open: 50 },
    ],
  };
  act(() => {
    onRealtimeChanged(qc, { topic: CONVERSATION_TOPIC, entity: { type: "conversation", id: x } });
  });
  await waitFor(() => {
    expect(names()).toEqual(["Lê Hà My", "Phạm Thu Trang"]);
  });
  const row = within(screen.getByText("Lê Hà My").closest("a") ?? document.body);
  expect(row.getByText("1").getAttribute("aria-label")).toBe("1 chưa đọc");
  await waitFor(() => {
    expect(
      screen
        .getByRole("button", { name: /Tất cả tài khoản/ })
        .querySelector(".unr")
        ?.getAttribute("aria-label"),
    ).toBe("130 chưa đọc");
  });
  expect(f.listCalls.filter((q) => q.get("updatedSince") !== null)).toHaveLength(1);
});

it("HV04: âm báo tắt mặc định; bấm → bật, nhớ trên máy", async () => {
  fakeWithRows();
  renderScreens(screens, "/inbox");
  await screen.findByText("Phạm Thu Trang");
  const btn = screen.getByRole("button", { name: "Âm báo tin mới" });
  expect(btn.getAttribute("aria-pressed")).toBe("false");
  fireEvent.click(btn);
  expect(btn.getAttribute("aria-pressed")).toBe("true");
  expect(localStorage.getItem(SOUND_STORAGE_KEY)).toBe("1");
});

it("HV07: lớp realtime đang nối lại → băng Mất kết nối có Tải lại ngay; bấm → tải lại danh sách", async () => {
  const f = fakeWithRows();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    screens.map((s) => ({ path: s.path, element: s.element })),
    { initialEntries: ["/inbox"] },
  );
  render(
    <QueryClientProvider client={qc}>
      <RealtimeStateContext value="reconnecting">
        <RouterProvider router={router} />
      </RealtimeStateContext>
    </QueryClientProvider>,
  );
  await screen.findByText("Phạm Thu Trang");
  expect(screen.getByText("Mất kết nối, đang nối lại.")).toBeTruthy();
  const n = f.listCalls.length;
  fireEvent.click(screen.getByRole("button", { name: "Tải lại ngay" }));
  await waitFor(() => {
    expect(f.listCalls.length).toBe(n + 1);
  });
});

it("không có hội thoại nào khi đang lọc chưa đọc → trạng thái trống có Bỏ lọc; Tất cả tài khoản ghi đã đọc hết", async () => {
  stubInbox(inboxFake({ counts: { byPage: [], mine: 0, unassigned: 0 } }));
  renderScreens(screens, "/inbox?unread=1");
  await screen.findByText("Không có hội thoại nào");
  expect(screen.getByText("Không còn tin chưa đọc. Bỏ lọc để xem cả hội thoại đã đọc.")).toBeTruthy();
  expect(screen.getByText("đã đọc hết")).toBeTruthy();
});

it("Page chỉ xem, Page mất kết nối: băng trên đầu danh sách và chip cạnh tên Page (HV08)", async () => {
  stubInbox(
    inboxFake({
      grants: [all("conversation.read"), all("channel.manage")],
      accounts: [
        account(P1, "ELIMAZ Official", { status: "degraded", statusReason: "token_invalid" }),
        account(P2, "ELIMAZ Sale", { sendMode: "read_only" }),
      ],
    }),
  );
  renderScreens(screens, "/inbox");
  await screen.findByText("Page ELIMAZ Official mất kết nối, cần quản trị kết nối lại.");
  expect(screen.getByText("Page ELIMAZ Sale đang ở chế độ chỉ xem.")).toBeTruthy();
  const nav = within(screen.getByRole("navigation", { name: "Tài khoản kênh" }));
  expect(within(nav.getByRole("button", { name: /ELIMAZ Official/ })).getByText("Mất kết nối")).toBeTruthy();
  expect(within(nav.getByRole("button", { name: /ELIMAZ Sale/ })).getByText("Chỉ xem")).toBeTruthy();
});

it("/inbox/:id: dòng đang mở sáng, có khung hội thoại và nút quay lại danh sách giữ bộ lọc", async () => {
  fakeWithRows();
  const { container } = renderScreens(screens, `/inbox/${convId(2)}?assignee=me`);
  await screen.findByText("Lê Hà My");
  expect(container.querySelector(".chat")?.className).toContain("has-conv");
  expect(screen.getByText("Lê Hà My").closest("a")?.getAttribute("aria-current")).toBe("true");
  const back = await screen.findByRole("link", { name: "Về danh sách hội thoại" });
  expect(back.getAttribute("href")).toBe("/inbox?assignee=me");
});

it("lỗi API danh sách → câu lỗi tiếng Việt, không vẽ như danh sách rỗng", async () => {
  fakeWithRows({ listError: { status: 403, body: { error: "forbidden", request_id: "t" } } });
  renderScreens(screens, "/inbox");
  await screen.findByRole("alert");
  expect(screen.queryByText("Không có hội thoại nào")).toBeNull();
  expect(screen.queryByText("Đang tải hội thoại…")).toBeNull();
});
