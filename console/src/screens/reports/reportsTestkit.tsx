// API giả (fetch) cho test vitest của màn Báo cáo (Task 239, M6-16). Chỉ test nhập tệp này. Số của kịch bản chín đơn
// (docs/m6/tieu-chi.md, mockup tong-quan.html trạng thái A): 8 đơn lên, 7 chưa hủy, doanh số 1.910.000, TB 272.857.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { vi } from "vitest";
import type { Grant, Me, ReportRun, SalesDashboardReport, SalesOrdersDrilldownItem } from "../../api";
import { onRealtimeChanged } from "../../app/queryClient";
import { NOT_FOUND, ORDER_PAGE, bodyOf, page, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { screens } from "./route";

export { ORDER_PAGE };

export const amounts = (orders: number, goodsAmount: number, shippingFeeCharged = 0) => ({
  orders,
  goodsAmount,
  shippingFeeCharged,
});

export const DASH: SalesDashboardReport = {
  status: "ready",
  period: { from: "2026-11-27", to: "2026-11-27" },
  generatedAt: "2026-11-27T07:32:05Z",
  placed: amounts(8, 2160000, 150000),
  active: amounts(7, 1910000, 150000),
  cancelled: amounts(1, 250000),
  delivered: amounts(2, 750000, 60000),
  returned: amounts(1, 250000, 30000),
  avgGoodsAmount: 272857,
  customerReturns: { items: 1, refundAmount: 150000 },
  pending: { confirm: 1, ship: 1, cancelRequested: 1 },
  byChannel: [
    { channel: "facebook", placed: amounts(6, 1550000, 120000), active: amounts(5, 1300000, 120000) },
    { channel: "web", placed: amounts(1, 360000, 30000), active: amounts(1, 360000, 30000) },
    { channel: "manual", placed: amounts(1, 250000), active: amounts(1, 250000) },
  ],
  byDay: [],
};

export const EMPTY_DASH: SalesDashboardReport = {
  ...DASH,
  placed: amounts(0, 0),
  active: amounts(0, 0),
  cancelled: amounts(0, 0),
  delivered: amounts(0, 0),
  returned: amounts(0, 0),
  avgGoodsAmount: null,
  customerReturns: { items: 0, refundAmount: 0 },
  pending: { confirm: 0, ship: 0, cancelRequested: 0 },
  byChannel: [],
};

export const LONG_NAME =
  "Trần Nguyễn Hoàng Bảo Bình — trưởng ca live tối thứ Bảy và Chủ nhật, phụ trách đơn đặt trước của khách Hà Nội và các tỉnh";

const hex = (n: number) => n.toString(16).padStart(12, "0");
export const orderId = (n: number) => `0195f3b2-0000-7000-8000-${hex(0xd000 + n)}`;

export function orderRow(n: number, p: Partial<SalesOrdersDrilldownItem> = {}): SalesOrdersDrilldownItem {
  return {
    orderId: orderId(n),
    code: `DH000480${String(n)}`,
    customerCode: `KH001204${String(n)}`,
    placedAt: "2026-11-27T06:58:00Z",
    statusLabel: "new",
    channel: "facebook",
    careByName: "Nguyễn Thị An",
    goodsAmount: 450000,
    shippingFeeCharged: 30000,
    ...p,
  };
}

export const OWNER: Grant[] = [
  { permission: "report.sales.read", scope: { type: "all", id: "" } },
  { permission: "report.export", scope: { type: "all", id: "" } },
];
export const ONE_PAGE: Grant[] = [{ permission: "report.sales.read", scope: { type: "page", id: "p1" } }];

/** Một bước trả lời của GET tổng quan; retryAfter (giây) đặt header Retry-After. */
export interface Step {
  status: number;
  body: unknown;
  retryAfter?: string;
}

/** GET tổng quan trả "chạy nền" (hết chỗ hay quá hạn giờ). */
export const BACKGROUND: Step = { status: 200, body: { status: "run_in_background", reason: "timeout" } };

/** Năm lần liền 503 retry_later (Retry-After 60 giây: không tự thử lại trong lúc test). */
export const busySteps = (): Step[] =>
  Array.from({ length: 5 }, () => ({
    status: 503,
    body: { error: "retry_later", request_id: "r" },
    retryAfter: "60",
  }));

/** QueryClient của test (không tự thử lại) và vỏ bọc cho renderHook. */
export const newClient = () => new QueryClient({ defaultOptions: { queries: { retry: false } } });
export const clientWrapper =
  (qc: QueryClient) =>
  ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;

/** Báo hiệu "lần chạy nền id vừa xong" như máy chủ gửi (chủ đề report.run_finished). */
export const finishRun = (qc: QueryClient, id: string) => {
  act(() => {
    onRealtimeChanged(qc, { topic: "report.run_finished", entity: { type: "report_run", id } });
  });
};

/** Chờ máy chủ giả nhận lần chạy nền đầu (run-1), đặt nó thành `patch` rồi báo hiệu xong như máy chủ. */
export async function settleRun(f: ReportsFake, qc: QueryClient, patch: Partial<ReportRun>) {
  await waitFor(() => {
    if (f.runs.size !== 1) throw new Error("chưa có lần chạy nền");
  });
  f.runs.set("run-1", makeRun("run-1", patch));
  finishRun(qc, "run-1");
}

export interface ReportsFake {
  writes: Write[];
  /** Số yêu cầu ghi tới đây còn bị mất mạng (không có câu trả lời); mỗi lần mất giảm một. */
  failPosts: number;
  replies: { status: number; body: unknown }[];
  hold: Promise<void> | null;
  grants: Grant[];
  /** Mọi GET báo cáo đã nhận: "đường?truy vấn". */
  gets: string[];
  /** Câu trả lời xếp sẵn của GET tổng quan (lấy lần lượt); hết thì trả dashboard. */
  steps: Step[];
  dashboard: unknown;
  /** Mọi đơn của danh sách chi tiết theo metric (mặc định rỗng). */
  drill: Record<string, SalesOrdersDrilldownItem[]>;
  runs: Map<string, ReportRun>;
  /** Có thì trả đường tải của getSalesReportRunFile. */
  fileUrl: string;
}

export function fakeReports(p: Partial<ReportsFake> = {}): ReportsFake {
  return {
    writes: [],
    replies: [],
    hold: null,
    grants: OWNER,
    failPosts: 0,
    gets: [],
    steps: [],
    dashboard: DASH,
    drill: {
      pending_confirm: [orderRow(4, { statusLabel: "new", customerCode: "KH0012044", goodsAmount: 360000 })],
    },
    runs: new Map(),
    fileUrl: "https://files.example.test/reports/run-1.xlsx",
    ...p,
  };
}

export function makeRun(id: string, p: Partial<ReportRun> = {}): ReportRun {
  return {
    id,
    family: "sales",
    report: "dashboard",
    format: "json",
    status: "queued",
    params: { from: "2026-11-27", to: "2026-11-27" },
    truncated: false,
    createdAt: "2026-11-27T07:00:00Z",
    expiresAt: "2026-12-04T07:00:00Z",
    requestedByName: "Nguyễn Thị Lan",
    ...p,
  };
}

const RUN_PATH = /^\/api\/v1\/reports\/sales\/runs\/([^/]+)(\/file)?$/;

function readRoute(f: ReportsFake, path: string, q: URLSearchParams): Response {
  if (path === "/api/v1/me") return reply({ userId: "me", email: "a@elimaz.vn", grants: f.grants } satisfies Me);
  if (path.startsWith("/api/v1/reports/")) f.gets.push(`${path}?${q.toString()}`);
  if (path === "/api/v1/reports/sales/dashboard") {
    const step = f.steps.shift();
    if (step === undefined) return reply(f.dashboard);
    const headers = new Headers();
    if (step.retryAfter !== undefined) headers.set("Retry-After", step.retryAfter);
    return Response.json(step.body, { status: step.status, headers });
  }
  if (path === "/api/v1/reports/sales/orders/drilldown") {
    const all = f.drill[q.get("metric") ?? ""] ?? [];
    return reply({ status: "ready", total: all.length, ...page(all, q) });
  }
  const m = RUN_PATH.exec(path);
  if (m !== null) {
    const run = f.runs.get(m[1] ?? "");
    if (run === undefined) return reply(NOT_FOUND, 404);
    return m[2] === undefined ? reply(run) : reply({ url: f.fileUrl, expiresAt: "2026-11-28T07:00:00Z" });
  }
  return reply(NOT_FOUND, 404);
}

/** POST …/runs: tạo lần chạy xếp hàng với số thứ tự (run-1, run-2…) và ghi vào f.runs. */
function writeRoute(f: ReportsFake, path: string, body: Record<string, unknown>): Response {
  if (path !== "/api/v1/reports/sales/runs") return reply(NOT_FOUND, 404);
  const id = `run-${String(f.runs.size + 1)}`;
  const params = typeof body.params === "object" && body.params !== null ? { ...body.params } : {};
  const run = makeRun(id, {
    format: body.format === "xlsx" ? "xlsx" : "json",
    params,
  });
  f.runs.set(id, run);
  return reply(run, 202);
}

/** Thay fetch bằng API giả (đọc: readRoute; ghi: ghi lại kèm Idempotency-Key, trả lần chạy mới). */
export function stubReports(f: ReportsFake): ReportsFake {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (req.method === "GET") return readRoute(f, url.pathname, url.searchParams);
      const body = await bodyOf(req);
      f.writes.push({ method: req.method, path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      if (f.failPosts > 0) {
        f.failPosts--;
        throw new TypeError("network");
      }
      if (f.hold !== null) await f.hold;
      const queued = f.replies.shift();
      return queued === undefined ? writeRoute(f, url.pathname, body) : reply(queued.body, queued.status);
    }),
  );
  return f;
}

/** Dựng màn Tổng quan ở đường path với API giả f. */
export function renderDashboard(f: ReportsFake, path = "/bao-cao/tong-quan") {
  stubReports(f);
  return renderScreens(screens, path, ORDER_PAGE);
}
