// @vitest-environment jsdom
// Kênh kết nối (kế hoạch M4 Task 178 Step 1; mockup ket-noi-page.html trạng thái A, C; HV32, HV34–HV38; HT15).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { TEXT_BAD_AUTHORIZE_URL } from "./ChannelsPage";
import { P2, account, newFake, stubDialog, stubKenh, type KenhFake } from "./kenhTestkit";
import { screens } from "./route";

let fake: KenhFake;
let assign: ReturnType<typeof vi.fn>;
const realLocation = window.location;

beforeEach(() => {
  stubDialog();
  fake = stubKenh(newFake());
  assign = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      href: realLocation.href,
      origin: realLocation.origin,
      pathname: realLocation.pathname,
      search: "",
      hash: "",
      assign,
    },
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  Object.defineProperty(window, "location", { configurable: true, value: realLocation });
});

const renderPage = () => renderScreens(screens, "/settings/channels");
const row = async (name: string) => {
  const b = await screen.findByText(name, { selector: "b" });
  const r = b.closest(".chrow");
  if (!(r instanceof HTMLElement)) throw new Error("không thấy dòng");
  return r;
};

describe("danh sách tài khoản kênh", () => {
  it("dòng Page: biểu tượng, trạng thái, chế độ, Page ID, tính năng kênh, lần kiểm, webhook", async () => {
    renderPage();
    const r = await row("ELIMAZ");
    expect(within(r).getByText("FB")).toBeTruthy();
    expect(within(r).getByText("Đang hoạt động")).toBeTruthy();
    expect(within(r).getByText("Chỉ xem")).toBeTruthy();
    expect(within(r).getByText("Facebook Page · Page ID 256660660862565")).toBeTruthy();
    for (const f of ["Tin nhắn", "Comment", "Live", "Nhắn riêng comment", "Ẩn comment", "Nút trả lời nhanh"]) {
      expect(within(r).getByText(`✓ ${f}`)).toBeTruthy();
    }
    expect(within(r).getByText("Đã đăng ký 11/11 trường")).toBeTruthy();
    expect(within(r).getByText(/mã truy cập còn hiệu lực · đủ quyền/)).toBeTruthy();
  });

  it("Page mất quyền (HT15): chip đỏ, hộp đỏ nói tin vẫn được nhận, nút chính Kết nối lại mở Facebook cùng tab", async () => {
    fake.channels = [account({ status: "degraded", statusReason: "token_invalid", name: "ELIMAZ Sale", id: P2 })];
    fake.replies.push({ status: 200, body: { authorizeUrl: "https://www.facebook.com/v23.0/dialog/oauth?state=s" } });
    renderPage();
    const r = await row("ELIMAZ Sale");
    expect(within(r).getByText("Mất quyền")).toBeTruthy();
    const box = within(r).getByRole("alert");
    expect(box.textContent).toContain("Mã truy cập không còn hiệu lực");
    expect(box.textContent).toContain("CRM vẫn nhận và lưu tin, comment mới nhưng không gửi được");
    fireEvent.click(within(r).getByRole("button", { name: "Kết nối lại" }));
    await waitFor(() => {
      expect(assign).toHaveBeenCalledWith("https://www.facebook.com/v23.0/dialog/oauth?state=s");
    });
    expect(fake.writes[0]?.path).toBe("/api/v1/channels/facebook/oauth/start");
  });

  it("Page đã ngắt: chip Đã ngắt, chỉ có Kết nối lại (không công tắc)", async () => {
    fake.channels = [account({ status: "disconnected" })];
    renderPage();
    const r = await row("ELIMAZ");
    expect(within(r).getByText("Đã ngắt")).toBeTruthy();
    expect(within(r).getByRole("button", { name: "Kết nối lại" })).toBeTruthy();
    expect(within(r).queryByRole("button", { name: /Chỉ xem/ })).toBeNull();
  });

  it("chưa có tài khoản → câu hướng dẫn", async () => {
    fake.channels = [];
    renderPage();
    expect(await screen.findByText(/Chưa có tài khoản kênh nào/)).toBeTruthy();
  });
});

describe("Kết nối Page Facebook", () => {
  it("startFacebookOAuth rồi window.location.assign(authorizeUrl) (cùng tab)", async () => {
    fake.replies.push({ status: 200, body: { authorizeUrl: "https://www.facebook.com/dialog/oauth?x=1" } });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Kết nối Page Facebook" }));
    await waitFor(() => {
      expect(assign).toHaveBeenCalledWith("https://www.facebook.com/dialog/oauth?x=1");
    });
    expect(fake.writes[0]?.key).not.toBeNull();
  });

  it("địa chỉ không phải facebook.com → không chuyển trang, báo lỗi", async () => {
    fake.replies.push({ status: 200, body: { authorizeUrl: "https://evil.example/oauth" } });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Kết nối Page Facebook" }));
    expect(await screen.findByText(TEXT_BAD_AUTHORIZE_URL)).toBeTruthy();
    expect(assign).not.toHaveBeenCalled();
  });
});

describe("chế độ chỉ xem (MO03, HV34)", () => {
  it("tắt chỉ xem → hộp xác nhận đúng câu; Hủy → không gọi API", async () => {
    renderPage();
    const r = await row("ELIMAZ");
    fireEvent.click(within(r).getByRole("button", { name: "Chỉ xem: bật" }));
    const dlg = screen.getByRole("dialog", { name: "Bật gửi thật cho Page ELIMAZ?" });
    expect(within(dlg).getByText("Khách sẽ nhận tin từ CRM.")).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fake.writes).toHaveLength(0);
  });

  it("xác nhận → setChannelSendMode enabled kèm version; dòng thành Đang gửi thật", async () => {
    renderPage();
    const r = await row("ELIMAZ");
    fireEvent.click(within(r).getByRole("button", { name: "Chỉ xem: bật" }));
    fake.channels = [account({ sendMode: "enabled", version: 4 })];
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Bật gửi thật" }));
    await waitFor(() => {
      expect(within(r).getByText("Đang gửi thật")).toBeTruthy();
    });
    expect(fake.writes[0]).toMatchObject({
      method: "PUT",
      path: "/api/v1/channels/0195f3b2-0000-7000-8000-0000000c0001/send-mode",
      body: { mode: "enabled", expectedVersion: 3 },
    });
  });

  it("bật lại chỉ xem không cần xác nhận", async () => {
    fake.channels = [account({ sendMode: "enabled" })];
    renderPage();
    const r = await row("ELIMAZ");
    fireEvent.click(within(r).getByRole("button", { name: "Chỉ xem: tắt" }));
    await waitFor(() => {
      expect(fake.writes[0]?.body).toEqual({ mode: "read_only", expectedVersion: 3 });
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("409 version_conflict → báo có người vừa đổi, nút Tải lại", async () => {
    fake.channels = [account({ sendMode: "enabled" })];
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderPage();
    const r = await row("ELIMAZ");
    fireEvent.click(within(r).getByRole("button", { name: "Chỉ xem: tắt" }));
    expect(await within(r).findByText(/Có người vừa đổi cài đặt Page này, tải lại/)).toBeTruthy();
    expect(within(r).getByRole("button", { name: "Tải lại" })).toBeTruthy();
  });
});

describe("Human Agent, kiểm tra, ngắt kết nối", () => {
  it("Human Agent là công tắc riêng kèm câu 'Chỉ bật khi Meta đã cấp'; bấm gửi enabled true", async () => {
    renderPage();
    const r = await row("ELIMAZ");
    expect(within(r).getByText(/Chỉ bật khi Meta đã cấp/)).toBeTruthy();
    fireEvent.click(within(r).getByRole("button", { name: "Human Agent: tắt" }));
    await waitFor(() => {
      expect(fake.writes[0]?.body).toEqual({ enabled: true, expectedVersion: 3 });
    });
    expect(fake.writes[0]?.path).toMatch(/\/human-agent$/);
  });

  it("kênh không có tính năng Human Agent → không có công tắc", async () => {
    const f = account().features;
    if (f === undefined) throw new Error("thiếu features");
    fake.channels = [account({ features: { ...f, humanAgent: false } })];
    renderPage();
    const r = await row("ELIMAZ");
    expect(within(r).queryByRole("button", { name: /Human Agent/ })).toBeNull();
  });

  it("Kiểm tra kết nối: khóa 'Đang kiểm tra…' tới khi có lần kiểm mới, rồi hiện kết quả", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      renderPage();
      const r = await row("ELIMAZ");
      fake.channels = [account({ tokenCheckedAt: "2099-01-01T00:00:00Z" })];
      fireEvent.click(within(r).getByRole("button", { name: "Kiểm tra kết nối" }));
      await waitFor(() => {
        expect(within(r).getByText(/ELIMAZ ổn/)).toBeTruthy();
      });
      expect(fake.writes[0]?.path).toMatch(/\/health-check$/);
      expect(within(r).getByRole("button", { name: "Kiểm tra kết nối" })).toHaveProperty("disabled", false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("Kiểm tra kết nối không so giờ máy nhân viên: lần kiểm mới ghi giờ máy chủ cũ hơn giờ máy vẫn là xong", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      vi.setSystemTime(new Date("2026-11-26T15:00:00Z"));
      renderPage();
      const r = await row("ELIMAZ");
      // Máy chủ chậm hơn máy nhân viên 5 phút: lần kiểm mới mang giờ 14:55 (vẫn khác lần trước 13:00).
      fake.channels = [account({ tokenCheckedAt: "2026-11-26T14:55:00Z" })];
      fireEvent.click(within(r).getByRole("button", { name: "Kiểm tra kết nối" }));
      expect(await within(r).findByText(/ELIMAZ ổn/)).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("Kiểm tra kết nối: giờ máy chủ chưa đổi (dù mới hơn giờ máy nhân viên) thì chưa báo ổn", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      vi.setSystemTime(new Date("2026-11-26T12:00:00Z"));
      renderPage();
      const r = await row("ELIMAZ");
      fireEvent.click(within(r).getByRole("button", { name: "Kiểm tra kết nối" }));
      expect(await within(r).findByRole("button", { name: "Đang kiểm tra…" })).toBeTruthy();
      expect(within(r).queryByText(/ELIMAZ ổn/)).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("Kiểm tra kết nối chưa có kết quả: nút khóa, quá 60 giây thì báo và mở lại nút", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      renderPage();
      const r = await row("ELIMAZ");
      fireEvent.click(within(r).getByRole("button", { name: "Kiểm tra kết nối" }));
      const busy = await within(r).findByRole("button", { name: "Đang kiểm tra…" });
      expect(busy).toHaveProperty("disabled", true);
      await vi.advanceTimersByTimeAsync(61_000);
      expect(await within(r).findByText(/Chưa có kết quả kiểm tra sau 60 giây/)).toBeTruthy();
      expect(within(r).getByRole("button", { name: "Kiểm tra kết nối" })).toHaveProperty("disabled", false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("Ngắt kết nối hỏi lại; Hủy → không gọi; xác nhận → disconnectChannel", async () => {
    renderPage();
    const r = await row("ELIMAZ");
    fireEvent.click(within(r).getByRole("button", { name: "Ngắt kết nối" }));
    let dlg = screen.getByRole("dialog", { name: "Ngắt kết nối Page ELIMAZ?" });
    expect(within(dlg).getByText(/Hội thoại, tin và comment đã nhận vẫn giữ/)).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy" }));
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(r).getByRole("button", { name: "Ngắt kết nối" }));
    dlg = screen.getByRole("dialog", { name: "Ngắt kết nối Page ELIMAZ?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ngắt kết nối" }));
    await waitFor(() => {
      expect(fake.writes[0]?.path).toMatch(/\/disconnect$/);
    });
  });
});
