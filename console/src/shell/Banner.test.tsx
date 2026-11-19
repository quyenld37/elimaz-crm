// @vitest-environment jsdom
// Băng báo của khung (spec 04 mục 6, ADR-34): đúng câu chữ cho từng trạng thái; khung vẫn tải khi API tạm ngừng.
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TEXT_EMPTY_HOME } from "../app/router";
import { App } from "./App";
import { Banner, TEXT_API_DOWN, TEXT_RECONNECTING, TEXT_SESSION_EXPIRED } from "./Banner";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Banner", () => {
  it("đúng câu chữ của spec 04 mục 6", () => {
    expect(TEXT_RECONNECTING).toBe("Mất kết nối, đang nối lại");
    expect(TEXT_SESSION_EXPIRED).toBe("Phiên đã hết hạn, tải lại trang");
    expect(TEXT_API_DOWN).toBe("Hệ thống tạm ngừng, đang thử lại");
  });

  it("reconnecting → Mất kết nối, đang nối lại", () => {
    render(<Banner realtime="reconnecting" apiDown={false} />);
    expect(screen.getByRole("status").textContent).toBe(TEXT_RECONNECTING);
  });

  it("session_expired → Phiên đã hết hạn, tải lại trang", () => {
    render(<Banner realtime="session_expired" apiDown={false} />);
    expect(screen.getByRole("alert").textContent).toBe(TEXT_SESSION_EXPIRED);
  });

  it("API không trả lời → băng báo API tạm ngừng", () => {
    render(<Banner realtime="connected" apiDown={true} />);
    expect(screen.getByRole("status").textContent).toBe(TEXT_API_DOWN);
  });

  it("API tạm ngừng và đang nối lại: hiện cả hai băng", () => {
    render(<Banner realtime="reconnecting" apiDown={true} />);
    const texts = screen.getAllByRole("status").map((e) => e.textContent);
    expect(texts).toEqual([TEXT_API_DOWN, TEXT_RECONNECTING]);
  });

  it("phiên hết hạn che các băng khác (tải lại trang là việc duy nhất cần làm)", () => {
    render(<Banner realtime="session_expired" apiDown={true} />);
    expect(screen.queryAllByRole("status")).toHaveLength(0);
    expect(screen.getByRole("alert").textContent).toBe(TEXT_SESSION_EXPIRED);
  });

  it("connected hoặc chưa biết, API sống → không hiện gì", () => {
    const { container, rerender } = render(<Banner realtime="connected" apiDown={false} />);
    expect(container.textContent).toBe("");
    rerender(<Banner realtime={null} apiDown={false} />);
    expect(container.textContent).toBe("");
  });
});

describe("App (khung)", () => {
  it("API không trả lời /health/live: khung vẫn tải và hiện băng API tạm ngừng", async () => {
    const urls: string[] = [];
    vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
      urls.push(input instanceof Request ? input.url : String(input));
      return Promise.reject(new TypeError("Failed to fetch"));
    });
    render(<App />);
    expect(screen.getAllByText("ELIMAZ").length).toBeGreaterThan(0); // cột trái và thanh trên (Task 48)
    // Chờ theo sự kiện (băng báo hiện, đã gọi đủ hai đường), không chờ một quãng giờ cố định: máy tải cao thì 50 ms
    // chưa đủ cho lần hỏi đầu (Task 421).
    await waitFor(() => {
      expect(screen.getAllByRole("status").map((e) => e.textContent)).toContain(TEXT_API_DOWN);
      expect(urls.some((u) => u.endsWith("/health/live"))).toBe(true);
      expect(urls.some((u) => u.endsWith("/api/v1/stream"))).toBe(true);
    });
    // Chưa đọc được /api/v1/me: không được báo nhầm "chưa được giao màn hình" (Task 48, reviewer).
    expect(screen.queryByText(TEXT_EMPTY_HOME)).toBeNull();
  });

  it("API trả lời: không có băng API tạm ngừng", async () => {
    vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
      const u = input instanceof Request ? input.url : String(input);
      if (u.endsWith("/health/live")) {
        return Promise.resolve(
          new Response(JSON.stringify({ status: "ok" }), {
            status: 200,
            headers: { "content-type": "application/json" },
          }),
        );
      }
      return new Promise<Response>(() => undefined);
    });
    render(<App />);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(screen.queryByText(TEXT_API_DOWN)).toBeNull();
  });
});

describe("App (khung) — /api/v1/me", () => {
  // Máy chủ giả: /health/live sống; /api/v1/me trả `me` (status, thân) hoặc treo khi null; đường khác treo.
  async function renderWithMe(me: [number, unknown] | null) {
    vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
      const u = input instanceof Request ? input.url : String(input);
      const reply = u.endsWith("/health/live")
        ? ([200, { status: "ok" }] as const)
        : u.endsWith("/api/v1/me")
          ? me
          : null;
      if (reply === null) return new Promise<Response>(() => undefined);
      const [status, body] = reply;
      return Promise.resolve(
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }),
      );
    });
    render(<App />);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
  }

  it("/me đang tải → không hiện câu trống hay không tìm thấy", async () => {
    await renderWithMe(null);
    expect(screen.queryByText(TEXT_EMPTY_HOME)).toBeNull();
    expect(screen.getByRole("main").textContent).toBe("");
  });

  it("/me trả 401 → không hiện câu trống (băng phiên hết hạn lo việc báo)", async () => {
    await renderWithMe([401, { error: "unauthenticated", request_id: "r" }]);
    expect(screen.queryByText(TEXT_EMPTY_HOME)).toBeNull();
  });

  it("/me tải xong, không có quyền màn nào → câu trống", async () => {
    await renderWithMe([200, { userId: "u", email: "lan@elimaz.vn", grants: [] }]);
    // findByText chờ /me trả và vẽ xong (máy tải cao: quá 50 ms của renderWithMe, Task 421).
    expect(await screen.findByText(TEXT_EMPTY_HOME)).toBeTruthy();
  });
});
