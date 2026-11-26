// @vitest-environment jsdom
// Inbox trạng thái D: khách chưa trả lời, gửi mẫu một chạm (CH16; HV09; mockup inbox.html trạng thái D). Task 175 làm
// thay Task 174 (dùng đường gửi tin theo mẫu của màn hội thoại).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { P1, all, conv, convId, inboxFake, stubInbox } from "./inboxTestkit";
import { screens } from "./route";

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const TPL = "0195f3b2-0000-7000-8000-0000000e0007";
const hoursAgo = (h: number) => new Date(Date.now() - h * 3_600_000).toISOString();
const hoursAhead = (h: number) => new Date(Date.now() + h * 3_600_000 + 60_000).toISOString();

function fake() {
  return stubInbox(
    inboxFake({
      rows: [conv(1)],
      followUps: [
        {
          conversationId: convId(5),
          pageId: P1,
          customer: { id: "c5", name: "Vũ Thị Lan Anh" },
          lastStaffMessageAt: hoursAgo(5),
          lastStaffSnippet: "Dạ mẫu này còn màu đen và be ạ",
          windowEndsAt: hoursAhead(19),
        },
      ],
      templates: [
        {
          id: TPL,
          shortcut: "nhac",
          title: "Nhắc lại",
          body: "Chị ơi mẫu chị hỏi còn ít",
          quickReplies: [],
          version: 1,
          createdAt: "2026-11-20T00:00:00Z",
        },
      ],
      rendered: { [TPL]: "Chị Lan Anh ơi mẫu chị hỏi còn ít" },
    }),
  );
}

it("CH16: bật 'Khách chưa trả lời' → danh sách listFollowUps (afterHours 3), URL có followup=1", async () => {
  const f = fake();
  f.followUps.push({
    conversationId: convId(6),
    pageId: P1,
    customer: { id: "c6", name: "Đỗ Minh Quân" },
    lastStaffMessageAt: hoursAgo(4),
    windowEndsAt: hoursAhead(20),
  });
  const { router } = renderScreens(screens, "/inbox");
  fireEvent.click(await screen.findByRole("button", { name: "Khách chưa trả lời" }));
  const list = await screen.findByRole("list", { name: "Khách chưa trả lời" });
  expect(within(list).getByText("Vũ Thị Lan Anh")).toBeTruthy();
  expect(within(list).getByText("Còn 19 giờ để nhắn")).toBeTruthy();
  // Task 501: đoạn tin cuối của shop ("Bạn: …"); tin cuối không chữ (ảnh) → không có dòng.
  expect(within(list).getByText("Bạn: Dạ mẫu này còn màu đen và be ạ")).toBeTruthy();
  expect(within(list).getAllByText(/^Bạn: /)).toHaveLength(1);
  expect(f.followUpCalls.at(-1)?.get("afterHours")).toBe("3");
  expect(router.state.location.search).toContain("followup=1");
  fireEvent.change(screen.getByRole("combobox", { name: "Im lặng quá" }), { target: { value: "6" } });
  await waitFor(() => {
    expect(f.followUpCalls.at(-1)?.get("afterHours")).toBe("6");
  });
});

it("CH16: 'Gửi mẫu /nhac' → render mẫu cho hội thoại rồi gửi tin chữ (hai khóa chống trùng), dòng thành 'Đã gửi'", async () => {
  const f = fake();
  renderScreens(screens, "/inbox?followup=1");
  const list = await screen.findByRole("list", { name: "Khách chưa trả lời" });
  fireEvent.click(await within(list).findByRole("button", { name: "Gửi mẫu /nhac" }));
  await waitFor(() => {
    expect(f.writes.filter((w) => w.path.endsWith("/messages"))).toHaveLength(1);
  });
  const [render, send] = f.writes;
  expect(render?.path).toBe(`/api/v1/reply-templates/${TPL}/render`);
  expect(render?.body).toEqual({ conversationId: convId(5) });
  expect(send?.path).toBe(`/api/v1/conversations/${convId(5)}/messages`);
  expect(send?.body).toEqual({ text: "Chị Lan Anh ơi mẫu chị hỏi còn ít" });
  expect(send?.key).not.toBe(render?.key);
  expect(await within(list).findByText("Đã gửi mẫu /nhac", { exact: false })).toBeTruthy();
  expect(within(list).queryByRole("button", { name: "Gửi mẫu /nhac" })).toBeNull();
});

it("chưa có mẫu trả lời → không có nút gửi, câu hướng dẫn tạo mẫu", async () => {
  stubInbox(inboxFake({ followUps: fake().followUps, templates: [] }));
  renderScreens(screens, "/inbox?followup=1");
  expect(await screen.findByText("Chưa có mẫu trả lời", { exact: false })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Gửi mẫu/ })).toBeNull();
});

it("chỉ có quyền xem (không có conversation.reply) → không có nút gửi, câu lý do quyền", async () => {
  const f = fake();
  f.grants = [all("conversation.read")];
  renderScreens(screens, "/inbox?followup=1");
  const list = await screen.findByRole("list", { name: "Khách chưa trả lời" });
  expect(await within(list).findByText("Bạn chưa được cấp quyền trả lời ở Page này")).toBeTruthy();
  expect(within(list).queryByRole("button", { name: /Gửi mẫu/ })).toBeNull();
});
