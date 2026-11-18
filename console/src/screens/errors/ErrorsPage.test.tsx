// @vitest-environment jsdom
// Màn Sự kiện lỗi (kế hoạch M1 Task 330; mockup docs/mockups/m1/su-kien-loi.html): đủ 10 loại lỗi theo thứ tự máy
// chủ, nội dung đầy đủ trong khối mở được, trạng thái trống có giờ kiểm, 403 → "Không có quyền", lỗi khác → hộp lỗi,
// tự tải lại mỗi 20 s, nút Tải lại. Fetch giả trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ErrorSummary, PlatformHealth } from "../../api";
import { ErrorsPage, REFRESH_MS, TEXT_EMPTY, TEXT_NO_ACCESS } from "./ErrorsPage";

function err(i: number, s: Partial<ErrorSummary> = {}): ErrorSummary {
  return {
    fingerprint: `f${String(i).padStart(2, "0")}abcdef0123`,
    name: `lỗi số ${String(i)}`,
    role: i % 2 === 0 ? "api" : "worker",
    message: `nội dung lỗi ${String(i)}`,
    count: 100 - i,
    firstSeenAt: "2026-11-17T23:12:00Z",
    lastSeenAt: "2026-11-18T07:28:00Z",
    ...s,
  };
}

function health(errors: ErrorSummary[]): PlatformHealth {
  return {
    processes: [],
    queue: { oldestPendingAgeS: 0, pending: 0, dead: 0, inDoubt: 0 },
    ingest: { spoolFiles: 0 },
    backup: {},
    errors,
    alerts: [],
  };
}

let reply: () => Response;
let calls: number;

const ok = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  calls = 0;
  reply = () => ok(health([]));
  vi.stubGlobal(
    "fetch",
    vi.fn((input: Request) => {
      if (new URL(input.url).pathname !== "/api/v1/platform/health") throw new Error(`không khai: ${input.url}`);
      calls++;
      return Promise.resolve(reply());
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ErrorsPage />
    </QueryClientProvider>,
  );
}

describe("ErrorsPage", () => {
  it("hiện đủ 10 loại theo thứ tự máy chủ, có tiến trình, số lần, lần đầu, lần cuối, mã nhận diện", async () => {
    const list = Array.from({ length: 10 }, (_, i) => err(i + 1));
    list[0] = err(1, { name: "pgconn.ConnectError", fingerprint: "a3f9c1ffee", count: 1204, role: "worker" });
    reply = () => ok(health(list));
    renderPage();
    const rows = await screen.findAllByRole("row");
    // một dòng đầu bảng + 10 dòng lỗi
    expect(rows).toHaveLength(11);
    const first = rows[1];
    if (first === undefined) throw new Error("thiếu dòng");
    const w = within(first);
    expect(w.getByText("pgconn.ConnectError")).toBeTruthy();
    expect(w.getByText(/a3f9c1$/)).toBeTruthy();
    expect(w.getByText("worker")).toBeTruthy();
    expect(w.getByText("1.204")).toBeTruthy();
    expect(w.getByText("06:12 18/11")).toBeTruthy();
    expect(w.getByText("14:28 18/11")).toBeTruthy();
    expect(within(rows[10] ?? first).getByText("lỗi số 10")).toBeTruthy();
  });

  it("nội dung dài 300 ký tự có đủ trong khối mở được (details)", async () => {
    const long = "x".repeat(150) + " " + "y".repeat(149);
    reply = () => ok(health([err(1, { message: long })]));
    const { container } = renderPage();
    await screen.findByText("lỗi số 1");
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details?.querySelector("summary")?.textContent).toBe(long);
    expect(details?.querySelector("p")?.textContent).toBe(long);
  });

  it("không có lỗi → trạng thái trống có giờ kiểm", async () => {
    renderPage();
    expect(await screen.findByText(TEXT_EMPTY)).toBeTruthy();
    expect(screen.getByText(/^Lần kiểm \d{2}:\d{2}:\d{2}\.$/)).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("API trả 403 → khối Không có quyền", async () => {
    reply = () =>
      new Response(JSON.stringify({ error: "forbidden", request_id: "t" }), {
        status: 403,
        headers: { "Content-Type": "application/json" },
      });
    renderPage();
    expect(await screen.findByText(TEXT_NO_ACCESS)).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("bị thu quyền khi đang mở (lần tải lại trả 403) → ẩn bảng cũ, hiện Không có quyền", async () => {
    reply = () => ok(health([err(1)]));
    renderPage();
    await screen.findByText("lỗi số 1");
    reply = () =>
      new Response(JSON.stringify({ error: "forbidden", request_id: "t" }), {
        status: 403,
        headers: { "Content-Type": "application/json" },
      });
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    expect(await screen.findByText(TEXT_NO_ACCESS)).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByText("lỗi số 1")).toBeNull();
  });

  it("API lỗi 500 → hộp lỗi có câu tiếng Việt", async () => {
    reply = () =>
      new Response(JSON.stringify({ error: "internal", request_id: "t" }), {
        status: 500,
        headers: { "Content-Type": "application/json" },
      });
    renderPage();
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("nút Tải lại gọi API lần nữa", async () => {
    renderPage();
    await screen.findByText(TEXT_EMPTY);
    expect(calls).toBe(1);
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(calls).toBe(2);
    });
  });

  it(`tự tải lại mỗi ${String(REFRESH_MS / 1000)} giây`, async () => {
    expect(REFRESH_MS).toBe(20_000);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderPage();
    await screen.findByText(TEXT_EMPTY);
    expect(calls).toBe(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH_MS);
    });
    await waitFor(() => {
      expect(calls).toBe(2);
    });
  });
});
