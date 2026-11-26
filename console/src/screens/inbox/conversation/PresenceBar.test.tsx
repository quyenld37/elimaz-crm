// @vitest-environment jsdom
// Ai đang xem, đang trả lời (Task 175; mockup hoi-thoai.html trạng thái A, B; CH10; HV23).
import { cleanup, act, fireEvent, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PresenceState } from "../../../api";
import { ME, convFake, renderConversation, stubConversation, writesTo } from "./conversationTestkit";
import { usePresenceHeartbeat } from "./usePresenceHeartbeat";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("usePresenceHeartbeat", () => {
  it("gửi mỗi 10 s khi hội thoại mở: 3 lần trong 30 s; đóng thì dừng", () => {
    vi.useFakeTimers();
    const send = vi.fn<(id: string, s: PresenceState) => Promise<void>>(() => Promise.resolve());
    const h = renderHook(() => {
      usePresenceHeartbeat("c1", "viewing", { send });
    });
    expect(send).toHaveBeenCalledTimes(1);
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(send).toHaveBeenCalledTimes(4);
    h.unmount();
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(send).toHaveBeenCalledTimes(4);
  });

  it("đổi trạng thái (đang gõ) → gửi ngay trạng thái mới, nhịp sau vẫn 10 s", () => {
    vi.useFakeTimers();
    const send = vi.fn<(id: string, s: PresenceState) => Promise<void>>(() => Promise.resolve());
    const h = renderHook(
      ({ s }: { s: PresenceState }) => {
        usePresenceHeartbeat("c1", s, { send });
      },
      { initialProps: { s: "viewing" } },
    );
    h.rerender({ s: "typing" });
    expect(send).toHaveBeenLastCalledWith("c1", "typing");
    expect(send).toHaveBeenCalledTimes(2);
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(send).toHaveBeenCalledTimes(3);
  });
});

describe("PresenceBar", () => {
  it("người khác đang gõ → 'An đang trả lời…'; đang lên đơn → 'Bình đang lên đơn'; không hiện chính mình", async () => {
    stubConversation(
      convFake({
        presence: [
          { userId: "u2", displayName: "An", state: "typing" },
          { userId: "u3", displayName: "Bình", state: "drafting_order" },
          { userId: "u4", displayName: "Chi", state: "viewing" },
          { userId: ME, displayName: "Tôi", state: "viewing" },
        ],
      }),
    );
    renderConversation();
    expect(await screen.findByText("An đang trả lời…")).toBeTruthy();
    expect(screen.getByText("Bình đang lên đơn")).toBeTruthy();
    expect(screen.getByText("Chi đang xem")).toBeTruthy();
    expect(screen.queryByText("Tôi đang xem")).toBeNull();
  });

  it("mở hội thoại gửi nhịp tim viewing, gõ chữ gửi typing; mỗi nhịp một khóa mới", async () => {
    const f = stubConversation(convFake());
    renderConversation();
    await waitFor(() => {
      expect(writesTo(f, "/presence")).toHaveLength(1);
    });
    expect(writesTo(f, "/presence")[0]?.body).toEqual({ state: "viewing" });
    fireEvent.change(await screen.findByRole("textbox", { name: "Nội dung trả lời" }), { target: { value: "D" } });
    await waitFor(() => {
      expect(writesTo(f, "/presence")).toHaveLength(2);
    });
    const [a, b] = writesTo(f, "/presence");
    expect(b?.body).toEqual({ state: "typing" });
    expect(b?.key).not.toBe(a?.key);
  });
});
