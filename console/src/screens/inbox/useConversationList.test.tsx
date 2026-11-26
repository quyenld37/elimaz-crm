// @vitest-environment jsdom
// useConversationList (Task 174, CH01 phía màn hình): trang đầu; báo hiệu conversation.changed → đúng một lần tải phần
// đổi (updatedSince = lần tải trước − 30 s), gộp theo id, sắp lại; resync → tải lại từ đầu; hội thoại đổi mà không có
// tin mới → tải lại cả danh sách.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { onRealtimeChanged, onRealtimeResync } from "../../app/queryClient";
import { DEFAULT_FILTERS } from "./inboxRules";
import type { ConversationSummary } from "../../api";
import { conv, convId, inboxFake, stubInbox, type InboxFake } from "./inboxTestkit";
import { CONVERSATION_TOPIC, useConversationList } from "./useConversationList";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function setup(rows = Array.from({ length: 5 }, (_, i) => conv(i + 1))) {
  const fake = stubInbox(inboxFake({ rows }));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const onFresh = vi.fn();
  const hook = renderHook(() => useConversationList(DEFAULT_FILTERS, { onFresh }), { wrapper });
  await waitFor(() => {
    expect(hook.result.current.rows).toHaveLength(rows.length);
  });
  return { fake, qc, hook, onFresh };
}

/** Máy chủ đổi hội thoại id. */
function edit(fake: InboxFake, id: string, patch: Partial<ConversationSummary>) {
  fake.rows = fake.rows.map((r) => (r.id === id ? { ...r, ...patch } : r));
}

/** Hội thoại x có tin mới trên máy chủ, báo hiệu tới; chờ x lên dòng đầu. */
async function newMessage(
  env: Omit<Awaited<ReturnType<typeof setup>>, "onFresh">,
  x: string,
  patch: Partial<ConversationSummary> = {},
) {
  edit(env.fake, x, { lastMessageAt: new Date().toISOString(), ...patch });
  changed(env.qc, x);
  await waitFor(() => {
    expect(env.hook.result.current.rows[0]?.id).toBe(x);
  });
}

/** Chờ máy chủ nhận đủ n lần gọi listConversations. */
async function calls(fake: InboxFake, n: number) {
  await waitFor(() => {
    expect(fake.listCalls).toHaveLength(n);
  });
}

const changed = (qc: QueryClient, id: string) => {
  act(() => {
    onRealtimeChanged(qc, { topic: CONVERSATION_TOPIC, entity: { type: "conversation", id } });
  });
};

it("báo hiệu cho X có tin mới → sau đúng một lần tải X ở dòng đầu, số chưa đọc tăng, X sáng nền", async () => {
  const { fake, qc, hook, onFresh } = await setup();
  expect(fake.listCalls).toHaveLength(1);
  expect(fake.listCalls[0]?.get("updatedSince")).toBeNull();
  const firstLoad = Date.now();

  const x = convId(4);
  await newMessage({ fake, qc, hook }, x, { unreadCount: 1, version: 2 });
  expect(fake.listCalls).toHaveLength(2);
  const since = fake.listCalls[1]?.get("updatedSince") ?? "";
  expect(Date.parse(since)).toBeLessThanOrEqual(firstLoad - 30_000);
  expect(Date.parse(since)).toBeGreaterThan(firstLoad - 40_000);
  expect(fake.listCalls[1]?.get("status")).toBe("open");
  expect(hook.result.current.rows[0]?.unreadCount).toBe(1);
  expect(hook.result.current.rows).toHaveLength(5);
  expect(hook.result.current.flash.has(x)).toBe(true);
  expect(onFresh).toHaveBeenCalledTimes(1);
  expect(onFresh.mock.calls[0]?.[0]).toEqual([expect.objectContaining({ id: x, unreadCount: 1 })]);
});

it("cùng id trả về ở hai lần tải → một dòng", async () => {
  const { fake, qc, hook } = await setup();
  const x = convId(1);
  edit(fake, x, { lastMessageAt: new Date().toISOString() });
  changed(qc, x);
  await calls(fake, 2);
  changed(qc, x);
  await calls(fake, 3);
  expect(hook.result.current.rows.filter((r) => r.id === x)).toHaveLength(1);
  expect(new Set(hook.result.current.rows.map((r) => r.id)).size).toBe(hook.result.current.rows.length);
});

it("resync → tải lại từ đầu (không updatedSince, không con trỏ)", async () => {
  const { fake, qc } = await setup();
  act(() => {
    onRealtimeResync(qc);
  });
  await calls(fake, 2);
  expect(fake.listCalls[1]?.get("updatedSince")).toBeNull();
  expect(fake.listCalls[1]?.get("cursor")).toBeNull();
});

it("hội thoại đang hiện đổi mà không có tin mới (giao, nhãn, đã đọc) → tải lại cả danh sách một lần", async () => {
  const { fake, qc, hook } = await setup();
  const x = convId(3);
  edit(fake, x, { unreadCount: 0, assignee: { id: "u", name: "An" } });
  changed(qc, x);
  await calls(fake, 3);
  expect(fake.listCalls[1]?.get("updatedSince")).not.toBeNull();
  expect(fake.listCalls[2]?.get("updatedSince")).toBeNull();
  await waitFor(() => {
    expect(hook.result.current.rows.find((r) => r.id === x)?.assignee?.name).toBe("An");
  });
});

it("báo hiệu của chủ đề khác → không tải gì (chỉ báo hiệu hội thoại sau đó gây đúng một lần tải)", async () => {
  const { fake, qc, hook } = await setup();
  act(() => {
    onRealtimeChanged(qc, { topic: "order.changed", entity: { type: "order", id: "o" } });
  });
  const x = convId(5);
  await newMessage({ fake, qc, hook }, x);
  expect(fake.listCalls).toHaveLength(2);
});

it("phần đổi không giữ mãi: sau lần tải lại cả danh sách, chỉ còn lần tải phần đổi gửi sau đó", async () => {
  const { fake, qc, hook } = await setup();
  await newMessage({ fake, qc, hook }, convId(5));
  await newMessage({ fake, qc, hook }, convId(4));
  expect(hook.result.current.heldDeltas).toBe(2);
  act(() => {
    onRealtimeResync(qc);
  });
  await calls(fake, 4);
  await waitFor(() => {
    expect(hook.result.current.rows[0]?.id).toBe(convId(4));
  });
  await newMessage({ fake, qc, hook }, convId(3));
  expect(hook.result.current.heldDeltas).toBe(1);
  expect(hook.result.current.rows.map((r) => r.id).slice(0, 3)).toEqual([convId(3), convId(4), convId(5)]);
});
