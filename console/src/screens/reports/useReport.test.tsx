// @vitest-environment jsdom
// useReport (Task 239, M6-16; Interfaces và Step 1 của kế hoạch): GET báo cáo; trả run_in_background thì tự tạo ĐÚNG MỘT
// lần chạy nền (POST …/runs, có Idempotency-Key), nghe báo hiệu report.run_finished của đúng lần chạy đó rồi hiện kết
// quả như xem tại chỗ; mạng lỗi rồi thử lại dùng cùng khóa; 503 retry_later hiện câu bận.
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { getSalesDashboard, type SalesDashboardReport } from "../../api";
import { unwrap } from "../../app/errors";
import {
  BACKGROUND,
  DASH,
  busySteps,
  clientWrapper,
  fakeReports,
  finishRun,
  settleRun,
  newClient,
  stubReports,
  type ReportsFake,
} from "./reportsTestkit";
import { parseDashboard } from "./dashboardData";
import { useReport } from "./useReport";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const PARAMS = { from: "2026-11-27", to: "2026-11-27" };

function mount(f: ReportsFake, o: { refreshMs?: number; sleep?: () => Promise<void> } = {}) {
  stubReports(f);
  const qc = newClient();
  const hook = renderHook(
    () =>
      useReport<SalesDashboardReport>({
        report: "dashboard",
        params: PARAMS,
        load: () => unwrap(getSalesDashboard({ query: PARAMS })),
        parse: parseDashboard,
        ...o,
      }),
    { wrapper: clientWrapper(qc) },
  );
  return { qc, ...hook };
}

/** Dựng hook với GET trả "chạy nền" và chờ lần chạy nền được tạo (có runId). */
async function mountWithRun() {
  const f = fakeReports({ steps: [BACKGROUND] });
  const m = mount(f);
  await waitFor(() => {
    expect(m.result.current.runId).not.toBeNull();
  });
  return { f, ...m };
}

describe("useReport", () => {
  it("xem tại chỗ: loading rồi ready có dữ liệu và giờ cập nhật", async () => {
    const f = fakeReports();
    const { result } = mount(f);
    expect(result.current.phase).toBe("loading");
    await waitFor(() => {
      expect(result.current.phase).toBe("ready");
    });
    expect(result.current.data?.active.orders).toBe(7);
    expect(result.current.updatedAt).not.toBeNull();
    expect(f.writes).toEqual([]);
  });

  it("run_in_background → đúng MỘT POST …/runs có Idempotency-Key và đúng báo cáo, tham số", async () => {
    const f = fakeReports({ steps: [BACKGROUND] });
    const { result } = mount(f);
    await waitFor(() => {
      expect(result.current.phase).toBe("background");
    });
    await waitFor(() => {
      expect(f.writes.length).toBe(1);
    });
    const w = f.writes[0];
    expect(w?.path).toBe("/api/v1/reports/sales/runs");
    expect(w?.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(w?.body).toEqual({ report: "dashboard", format: "json", params: PARAMS });
    // Không tự tạo thêm lần chạy dù hook render lại.
    await new Promise((r) => setTimeout(r, 50));
    expect(f.writes.length).toBe(1);
  });

  it("báo hiệu report.run_finished của đúng lần chạy → tải GET …/runs/{id} → hiện số như xem tại chỗ", async () => {
    const f = fakeReports({ steps: [BACKGROUND] });
    const { result, qc } = mount(f);
    await waitFor(() => {
      expect(result.current.phase).toBe("background");
    });
    await settleRun(f, qc, { status: "ready", result: { ...DASH } });
    await waitFor(() => {
      expect(result.current.phase).toBe("ready");
    });
    expect(result.current.data?.placed.orders).toBe(8);
  });

  it("lần chạy nền failed too_heavy → phase failed với câu 'quá nặng, thu hẹp khoảng thời gian'", async () => {
    const f = fakeReports({ steps: [BACKGROUND] });
    const { result, qc } = mount(f);
    await settleRun(f, qc, { status: "failed", reason: "too_heavy" });
    await waitFor(() => {
      expect(result.current.phase).toBe("failed");
    });
    expect(result.current.message).toBe("Báo cáo quá nặng, thu hẹp khoảng thời gian.");
  });

  it("mạng lỗi khi tạo lần chạy rồi thử lại → cùng Idempotency-Key", async () => {
    // 1 lần gửi + 3 lần tự gửi lại của useWriteAction đều mất mạng; lần Thử lại sau đó tới nơi.
    const f = fakeReports({ steps: [BACKGROUND], failPosts: 4 });
    const { result } = mount(f, { sleep: () => Promise.resolve() });
    await waitFor(() => {
      expect(result.current.message).toBe("Mất kết nối, chưa rõ đã lưu chưa. Bấm lại để gửi lại, không bị trùng");
    });
    const keys = f.writes.map((w) => w.key);
    expect(keys.length).toBe(4);
    expect(new Set(keys).size).toBe(1);
    act(() => {
      result.current.retryRun();
    });
    await waitFor(() => {
      expect(f.runs.size).toBe(1);
    });
    expect(f.writes.at(-1)?.key).toBe(keys[0]);
  });

  it("Tải lại khi đang chờ lần chạy nền không tạo thêm lần chạy; xong rồi thì tải lại bình thường", async () => {
    const { f, result, qc } = await mountWithRun();
    act(() => {
      result.current.reload();
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(f.writes.length).toBe(1);
    expect(f.gets.filter((g) => g.includes("/dashboard")).length).toBe(1);
    expect(result.current.phase).toBe("background");
    await settleRun(f, qc, { status: "failed", reason: "error" });
    await waitFor(() => {
      expect(result.current.phase).toBe("failed");
    });
    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(result.current.phase).toBe("ready");
    });
    expect(f.gets.filter((g) => g.includes("/dashboard")).length).toBe(2);
  });

  it("không đọc được lần chạy nền (404) → phase error có câu tiếng Việt, không đứng 'đang chạy nền' mãi", async () => {
    const { f, result, qc } = await mountWithRun();
    f.runs.clear(); // máy chủ không còn lần chạy này: GET …/runs/{id} → 404
    finishRun(qc, "run-1");
    await waitFor(() => {
      expect(result.current.phase).toBe("error");
    });
    expect(result.current.message).toBe("Không tìm thấy, có thể đã bị xóa");
  });

  it("503 retry_later khi xem → câu 'Hệ thống đang bận, thử lại sau ít giây'", async () => {
    const f = fakeReports({
      steps: busySteps(),
    });
    const { result } = mount(f);
    await waitFor(() => {
      expect(result.current.phase).toBe("error");
    });
    expect(result.current.message).toBe("Hệ thống đang bận, thử lại sau ít giây");
  });

  it("lỗi khác (403) → phase error có câu tiếng Việt; reload() tải lại", async () => {
    const f = fakeReports({ steps: [{ status: 403, body: { error: "forbidden", request_id: "r" } }] });
    const { result } = mount(f);
    await waitFor(() => {
      expect(result.current.phase).toBe("error");
    });
    expect(result.current.message).toBe("Bạn chưa được cấp quyền làm việc này");
    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(result.current.phase).toBe("ready");
    });
  });

  it("đồng hồ giả: sau 60 s gọi lại API đúng một lần; tab ẩn thì không gọi", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = fakeReports();
    const { result } = mount(f, { refreshMs: 60_000 });
    await vi.waitFor(() => {
      expect(result.current.phase).toBe("ready");
    });
    expect(f.gets.length).toBe(1);
    await vi.advanceTimersByTimeAsync(59_000);
    expect(f.gets.length).toBe(1);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(f.gets.length).toBe(2);
    const vis = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(f.gets.length).toBe(2);
    vis.mockReturnValue("visible");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(f.gets.length).toBe(3);
  });
});
