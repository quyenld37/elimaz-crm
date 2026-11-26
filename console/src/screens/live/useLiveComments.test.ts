// @vitest-environment jsdom
// Kế hoạch M4 Task 177: useLiveComments tải theo after khi nhận live.comments (máy chủ gộp 300 ms), lùi 30 s và bỏ trùng;
// quy tắc thuần của màn (gộp, đếm comment mới, đọc lỗi chốt, chip xin địa chỉ).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../../app/errors";
import { onRealtimeChanged, onRealtimeResync } from "../../app/queryClient";
import { OLD_DRAFT, S_ENDED, S_LIVE, comment, installLiveFake } from "./liveTestkit";
import { afterOf, askChip, claimProblem, mergeComments, unseenCount } from "./liveRules";
import { useLiveComments } from "./useLiveComments";

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup(sessionId = S_LIVE, filters = { phoneOnly: false, unhandledOnly: false }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
  const view = renderHook((p: { id: string; f: typeof filters }) => useLiveComments(p.id, p.f), {
    wrapper,
    initialProps: { id: sessionId, f: filters },
  });
  return { qc, ...view };
}

/** Dựng hook và chờ lần tải đầu có đủ n comment. */
async function loaded(n: number) {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.items).toHaveLength(n);
  });
  return view;
}

const signal = (qc: QueryClient, id: string) => {
  act(() => {
    onRealtimeChanged(qc, { topic: "live.comments", entity: { type: "live_session", id } });
  });
};

describe("useLiveComments", () => {
  it("tải trang đầu theo nextCursor tới hết (1.200 comment, 3 trang 500)", async () => {
    const fake = installLiveFake({ comments: Array.from({ length: 1200 }, (_, i) => comment(i)) });
    await loaded(1200);
    expect(fake.commentCalls.map((q) => q.get("after"))).toEqual([
      null,
      `p:${afterOf(comment(499))}`,
      `p:${afterOf(comment(999))}`,
    ]);
    expect(fake.commentCalls.every((q) => q.get("limit") === "500")).toBe(true);
  });

  it("báo hiệu của phiên → một lần tải sau mốc after của comment cuối; trùng messageId (lùi 30 s) chỉ một dòng", async () => {
    const fake = installLiveFake({ comments: Array.from({ length: 20 }, (_, i) => comment(i)) });
    const { qc, result } = await loaded(20);
    fake.comments.push(...Array.from({ length: 100 }, (_, i) => comment(20 + i)));
    signal(qc, S_LIVE);
    await waitFor(() => {
      expect(result.current.items).toHaveLength(120);
    });
    expect(fake.commentCalls).toHaveLength(2);
    expect(fake.commentCalls[1]?.get("after")).toBe(afterOf(comment(19)));
    expect(new Set(result.current.items.map((c) => c.messageId)).size).toBe(120);
    expect(result.current.items.map((c) => c.messageId)).toEqual(fake.comments.map((c) => c.messageId));
  });

  it("báo hiệu của phiên khác bị bỏ qua", async () => {
    const fake = installLiveFake({ comments: [comment(1)] });
    const { qc } = await loaded(1);
    signal(qc, S_ENDED);
    signal(qc, S_ENDED);
    await new Promise((r) => setTimeout(r, 20));
    expect(fake.commentCalls).toHaveLength(1);
  });

  it("nhiều báo hiệu khi đang tải → chỉ thêm một lần tải sau khi lần đang chạy xong (gom)", async () => {
    const fake = installLiveFake({ comments: [comment(1)] });
    const { qc, result } = await loaded(1);
    // Giữ lần tải đang chạy bằng fetch chậm (GET không đi qua hàng đợi ghi).
    let release: () => void = () => undefined;
    const slow = new Promise<void>((r) => {
      release = r;
    });
    const real = globalThis.fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        await slow;
        return real(req);
      }),
    );
    fake.comments.push(comment(2), comment(3));
    for (let i = 0; i < 6; i++) signal(qc, S_LIVE);
    release();
    await waitFor(() => {
      expect(result.current.items).toHaveLength(3);
    });
    await new Promise((r) => setTimeout(r, 20));
    // 1 lần đầu + 1 lần đang chạy + đúng 1 lần gom cho các báo hiệu tới trong lúc đó.
    expect(fake.commentCalls).toHaveLength(3);
  });

  it("resync tải lại cả danh sách (đúng trạng thái chốt của comment cũ)", async () => {
    const fake = installLiveFake({ comments: [comment(1), comment(2)] });
    const { qc, result } = await loaded(2);
    fake.comments[0] = comment(1, { claim: { by: { id: "x", name: "Lan" }, orderId: "o", orderCode: "DH1" } });
    act(() => {
      onRealtimeResync(qc);
    });
    await waitFor(() => {
      expect(result.current.items[0]?.claim?.orderCode).toBe("DH1");
    });
    expect(fake.commentCalls.at(-1)?.get("after")).toBeNull();
  });

  it("resync khi lọc 'Chưa xử lý': comment người khác vừa chốt rời danh sách, hết thì danh sách rỗng", async () => {
    const fake = installLiveFake({ comments: [comment(1), comment(2)] });
    const { qc, result, rerender } = setup();
    await waitFor(() => {
      expect(result.current.items).toHaveLength(2);
    });
    rerender({ id: S_LIVE, f: { phoneOnly: false, unhandledOnly: true } });
    await waitFor(() => {
      expect(fake.commentCalls.at(-1)?.get("unhandledOnly")).toBe("true");
    });
    await waitFor(() => {
      expect(result.current.items).toHaveLength(2);
    });
    const by = { id: "x", name: "Lan" };
    fake.comments[1] = comment(2, { claim: { by, orderId: "o2", orderCode: "DH2" } });
    act(() => {
      onRealtimeResync(qc);
    });
    await waitFor(() => {
      expect(result.current.items.map((c) => c.messageId)).toEqual([comment(1).messageId]);
    });
    fake.comments[0] = comment(1, { claim: { by, orderId: "o1", orderCode: "DH1" } });
    act(() => {
      onRealtimeResync(qc);
    });
    await waitFor(() => {
      expect(result.current.items).toEqual([]);
    });
  });

  it("lọc có SĐT, chưa xử lý gửi tham số; đổi lọc tải lại từ đầu", async () => {
    const fake = installLiveFake({ comments: [comment(1, { hasPhone: true }), comment(2)] });
    const { result, rerender } = await loaded(2);
    rerender({ id: S_LIVE, f: { phoneOnly: true, unhandledOnly: true } });
    await waitFor(() => {
      expect(result.current.items).toHaveLength(1);
    });
    const last = fake.commentCalls.at(-1);
    expect(last?.get("phoneOnly")).toBe("true");
    expect(last?.get("unhandledOnly")).toBe("true");
    expect(last?.get("after")).toBeNull();
  });

  it("patch sửa một comment tại chỗ (đã chốt xong ở máy này)", async () => {
    installLiveFake({ comments: [comment(1), comment(2)] });
    const { result } = await loaded(2);
    act(() => {
      result.current.patch(comment(2).messageId, (c) => ({
        ...c,
        claim: { by: { id: "me", name: "An" }, orderId: "o", orderCode: "DH9" },
      }));
    });
    await waitFor(() => {
      expect(result.current.items[1]?.claim?.orderCode).toBe("DH9");
    });
  });
});

describe("liveRules", () => {
  it("mergeComments: bản mới thắng, xếp theo (sentAt, messageId)", () => {
    const a = comment(3);
    const b = comment(1);
    const b2 = { ...comment(1), text: "đã sửa" };
    expect(mergeComments([a, b], [b2]).map((c) => c.text)).toEqual(["đã sửa", "comment số 3"]);
  });

  it("unseenCount: 600 mới khi đang xem phía trên, giảm khi cuộn xuống, không âm", () => {
    expect(unseenCount(620, 20, 9)).toBe(600);
    expect(unseenCount(620, 20, 319)).toBe(300);
    expect(unseenCount(620, 20, 619)).toBe(0);
    expect(unseenCount(10, 20, 3)).toBe(0);
  });

  it("claimProblem đọc đủ bốn lỗi của lệnh chốt", () => {
    const e = (code: string, status: number, details: Record<string, unknown> = {}) =>
      new ApiError({ status, code, details });
    expect(claimProblem(e("comment_claimed", 409, { claimedBy: "An", orderId: "o", orderCode: "DH1" }))).toEqual({
      kind: "claimed",
      by: "An",
      orderId: "o",
      orderCode: "DH1",
    });
    expect(claimProblem(e("out_of_stock", 409, { available: 0, variantIds: ["v"] }))).toEqual({
      kind: "out_of_stock",
      available: 0,
      variantId: "v",
    });
    expect(claimProblem(e("open_draft_exists", 409, { drafts: [OLD_DRAFT, { bad: 1 }] }))).toEqual({
      kind: "open_draft",
      drafts: [OLD_DRAFT],
    });
    expect(claimProblem(e("seller_mismatch", 422))).toEqual({ kind: "seller_mismatch" });
    expect(claimProblem(e("internal", 500))).toBeNull();
  });

  it("askChip: đã xin, chưa xin được kèm lý do, chưa xin", () => {
    expect(askChip({ status: "queued" })).toEqual({ cls: "chip chip-ok", label: "Đã xin địa chỉ", reason: null });
    expect(askChip({ status: "skipped_policy", reason: "Page đang ở chế độ chỉ xem" }).label).toBe("Chưa xin được");
    expect(askChip({ status: "queued", reason: "Tin gửi lỗi" }).reason).toBe("Tin gửi lỗi");
    expect(askChip({ status: "not_requested", reason: "Page chưa chọn mẫu xin địa chỉ" }).label).toBe("Chưa xin");
  });
});
