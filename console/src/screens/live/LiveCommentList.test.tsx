// @vitest-environment jsdom
// Kế hoạch M4 Task 177 (LV02 "màn hình vẫn mượt"): 600 comment trong 6 lô → danh sách ảo dựng dưới 80 dòng; đang xem
// phía trên thì vị trí đứng yên, "↓ 600 comment mới" rồi giảm khi cuộn xuống. Dòng có trạng thái chốt, nhãn; lọc; băng
// hỏi định kỳ; phiên đã kết thúc có số trên Facebook; chưa có phiên; thêm phiên bằng link video.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { stubDialog } from "../don/donTestkit";
import {
  AN,
  ENDED,
  LONG_NAME,
  PAGE,
  S_ENDED,
  S_LIVE,
  comment,
  fakeLayout,
  installLiveFake,
  renderLive,
  session,
} from "./liveTestkit";

beforeEach(() => {
  fakeLayout();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const list = () => screen.getByTestId("lclist");
const rows = () => list().querySelectorAll(".lc");
const signal = (qc: QueryClient) => {
  act(() => {
    onRealtimeChanged(qc, { topic: "live.comments", entity: { type: "live_session", id: S_LIVE } });
  });
};
const scrollTo = (top: number) => {
  act(() => {
    list().scrollTop = top;
    fireEvent.scroll(list());
  });
};

describe("LiveCommentList", () => {
  it("600 comment tới trong 6 lô khi đang xem phía trên: dưới 80 dòng, vị trí giữ nguyên, '600 comment mới' giảm khi cuộn", async () => {
    const fake = installLiveFake({ comments: Array.from({ length: 20 }, (_, i) => comment(i)) });
    const { qc } = renderLive(`/live/${S_LIVE}`);
    await screen.findByText("Khách 0");
    scrollTo(0);
    for (let b = 0; b < 6; b++) {
      fake.comments.push(...Array.from({ length: 100 }, (_, i) => comment(20 + b * 100 + i)));
      signal(qc);
      await screen.findByText(`${String(120 + b * 100)} comment`);
    }
    expect(rows().length).toBeLessThan(80);
    expect(list().scrollTop).toBe(0);
    expect(within(list()).getByText("Khách 0")).toBeTruthy();
    expect(screen.getByRole("button", { name: "↓ 600 comment mới" })).toBeTruthy();
    // Cuộn xuống giữa: dòng cuối đang thấy là khoảng dòng 320 → còn khoảng 300 comment mới.
    scrollTo(310 * 64);
    const btn = await screen.findByRole("button", { name: /comment mới$/ });
    const n = Number(btn.textContent.replace(/\D/g, ""));
    expect(n).toBeGreaterThan(250);
    expect(n).toBeLessThan(600);
    expect(rows().length).toBeLessThan(80);
    // Bấm nút → xuống cuối, nút mất.
    fireEvent.click(btn);
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: /comment mới$/ })).toBeNull();
    });
  });

  it("đang ở cuối danh sách: comment mới tự hiện, không có nút 'comment mới'", async () => {
    const fake = installLiveFake({ comments: Array.from({ length: 20 }, (_, i) => comment(i)) });
    const { qc } = renderLive(`/live/${S_LIVE}`);
    await screen.findByText("20 comment");
    fake.comments.push(...Array.from({ length: 50 }, (_, i) => comment(20 + i)));
    signal(qc);
    await screen.findByText("70 comment");
    expect(screen.queryByRole("button", { name: /comment mới$/ })).toBeNull();
  });

  it("dòng có nhãn Có SĐT, Đã ẩn trên Page, Đã lên đơn · người, Khách đã có đơn nháp, Đã nhắn riêng; nội dung là chữ thuần", async () => {
    installLiveFake({
      comments: [
        comment(1, {
          hasPhone: true,
          isHidden: true,
          claim: { by: AN, orderId: "0195f3b2-0000-7000-8000-00000000d240", orderCode: "DH0001240" },
        }),
        comment(2, { customerHasOpenDraft: true, customer: { id: "c", name: LONG_NAME } }),
        comment(3, { privateReply: { state: "sent" }, text: "<img src=x onerror=alert(1)>" }),
      ],
    });
    renderLive(`/live/${S_LIVE}`);
    await screen.findByText("Đã lên đơn DH0001240 · Trần Minh An");
    expect(screen.getByText("Có SĐT")).toBeTruthy();
    expect(screen.getByText("Đã ẩn trên Page")).toBeTruthy();
    expect(screen.getByText("Khách đã có đơn nháp")).toBeTruthy();
    expect(screen.getByText("Đã nhắn riêng")).toBeTruthy();
    expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeTruthy();
    expect(list().querySelector("img")).toBeNull();
    // Tên 60 ký tự liền: một dòng cắt bằng ellip, không tràn ngang.
    expect(screen.getByText(LONG_NAME).className).toContain("ellip");
    const done = rows()[0];
    expect(done?.className).toContain("is-done");
  });

  it("lọc 'Chỉ comment có SĐT', 'Chưa xử lý' gửi tham số và bật nút", async () => {
    const fake = installLiveFake({ comments: [comment(1, { hasPhone: true }), comment(2)] });
    renderLive(`/live/${S_LIVE}`);
    await screen.findByText("Khách 2");
    fireEvent.click(screen.getByRole("button", { name: "Chỉ comment có SĐT" }));
    await waitFor(() => {
      expect(screen.queryByText("Khách 2")).toBeNull();
    });
    expect(fake.commentCalls.at(-1)?.get("phoneOnly")).toBe("true");
    expect(screen.getByRole("button", { name: "Chỉ comment có SĐT" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Chưa xử lý" }));
    await waitFor(() => {
      expect(fake.commentCalls.at(-1)?.get("unhandledOnly")).toBe("true");
    });
  });

  it("phiên đọc bằng hỏi định kỳ → băng; chọn phiên mặc định là phiên đang live; dải số của phiên", async () => {
    installLiveFake({ sessions: [ENDED, session({ activeSource: "poll" })] });
    renderLive("/live");
    await screen.findByText("Comment đang lấy bằng cách hỏi định kỳ.");
    expect(screen.getByRole("link", { name: /Đang live/ }).getAttribute("aria-current")).toBe("true");
    expect(screen.getByText("hỏi định kỳ")).toBeTruthy();
    expect(screen.getByText("600")).toBeTruthy();
  });

  it("phiên đã kết thúc: số nhận được và số trên Facebook; lệch → hộp đỏ 'Thiếu 5 comment.'", async () => {
    installLiveFake();
    renderLive(`/live/${S_ENDED}`);
    await screen.findByText("Thiếu 5 comment.");
    expect(screen.getByText("605 comment")).toBeTruthy();
    expect(screen.queryByText("Comment đang lấy bằng cách hỏi định kỳ.")).toBeNull();
  });

  it("chưa có phiên hôm nay → trạng thái rỗng; thêm phiên bằng link video gửi startLiveSessionManually có khóa", async () => {
    const fake = installLiveFake({ sessions: [] });
    renderLive("/live");
    await screen.findByText("Chưa có phiên live hôm nay");
    fireEvent.click(await screen.findByRole("button", { name: "+ Thêm phiên bằng link video" }));
    fireEvent.change(screen.getByLabelText("Link hoặc mã video *"), {
      target: { value: "https://www.facebook.com/elimaz/videos/123456789/" },
    });
    await waitFor(() => {
      expect(screen.getByRole<HTMLSelectElement>("combobox").value).toBe(PAGE);
    });
    fireEvent.click(screen.getByRole("button", { name: "Thêm phiên" }));
    await screen.findByRole("link", { name: "Mở phiên" });
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]?.path).toBe(`/api/v1/channels/${PAGE}/live-sessions`);
    expect(fake.writes[0]?.body).toEqual({ videoIdOrUrl: "https://www.facebook.com/elimaz/videos/123456789/" });
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("không có live.operate → không có nút thêm phiên", async () => {
    installLiveFake({ sessions: [], grants: [{ permission: "conversation.read", scope: { type: "all", id: "" } }] });
    renderLive("/live");
    await screen.findByText("Chưa có phiên live hôm nay");
    expect(screen.queryByRole("button", { name: "+ Thêm phiên bằng link video" })).toBeNull();
  });
});
