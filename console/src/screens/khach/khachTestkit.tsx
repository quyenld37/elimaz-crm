// API giả (fetch) và cách dựng cho test vitest của màn Khách. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";
import type {
  Customer,
  CustomerImport,
  CustomerListItem,
  CustomerTag,
  CustomerTagRule,
  Grant,
  MergeCandidate,
  Me,
  TimelineItem,
} from "../../api";
import { screens } from "./route";

/** Hộp thoại trong jsdom: dùng chung bản của màn Đơn. */
export { stubDialog } from "../don/donTestkit";

export const C1 = "0195f3b2-0000-7000-8000-00000000c001";
export const C2 = "0195f3b2-0000-7000-8000-00000000c002";
export const C3 = "0195f3b2-0000-7000-8000-00000000c003";
export const T1 = "0195f3b2-0000-7000-8000-00000000a001";
export const T2 = "0195f3b2-0000-7000-8000-00000000a002";
export const T3 = "0195f3b2-0000-7000-8000-00000000a003";
export const M1 = "0195f3b2-0000-7000-8000-00000000b001";
export const M2 = "0195f3b2-0000-7000-8000-00000000b002";
export const ME = "0195f3b2-0000-7000-8000-0000000000e1";
export const IMP = "0195f3b2-0000-7000-8000-00000000f001";
export const FILE = "0195f3b2-0000-7000-8000-00000000f101";

export const LONG_COMPANY = "Công ty TNHH Thương mại Dịch vụ Thời trang Hoàng Gia Minh Châu";

export const STATS = {
  orderCount: 12,
  deliveredCount: 9,
  returnedCount: 2,
  totalSpent: 8_450_000,
  lastOrderAt: "2026-11-18T07:31:00Z",
  returnRateBp: 1818,
};

export function listItem(p: Partial<CustomerListItem> = {}): CustomerListItem {
  return {
    id: C1,
    code: "KH000231",
    fullName: "Trần Thị Thu Hà",
    phones: ["+84912345678"],
    stats: STATS,
    ...p,
  };
}

export const TAG_QUEN: CustomerTag = { id: T1, name: "Khách quen", color: "gray", version: 1 };
export const TAG_HOAN: CustomerTag = { id: T2, name: "Hay hoàn", color: "red", version: 2 };
export const TAG_SI: CustomerTag = { id: T3, name: "Khách sỉ", color: "blue", version: 1 };

export function customer(p: Partial<Customer> = {}): Customer {
  return {
    id: C1,
    rootId: C1,
    code: "KH000231",
    fullName: "Trần Thị Thu Hà",
    lifecycle: "customer",
    birthday: "1990-08-12",
    stats: STATS,
    memberIds: [C1, C3],
    identities: [
      {
        id: "i1",
        customerId: C1,
        kind: "phone",
        value: "+84912345678",
        source: "order",
        createdAt: "2026-03-01T00:00:00Z",
      },
      {
        id: "i2",
        customerId: C3,
        kind: "phone",
        value: "+84355999000",
        source: "staff",
        createdAt: "2026-04-01T00:00:00Z",
      },
      {
        id: "i3",
        customerId: C1,
        kind: "fb_psid",
        scope: "page-1",
        value: "psid-123",
        source: "chat",
        createdAt: "2026-04-01T00:00:00Z",
      },
    ],
    addresses: [
      {
        id: "a1",
        customerId: C1,
        isDefault: true,
        version: 1,
        address: {
          name: "Trần Thị Thu Hà",
          phone: "+84912345678",
          line: "Số 12 ngõ 68 Trần Thái Tông",
          provinceName: "Hà Nội",
          wardName: "Phường Cầu Giấy",
        },
      },
    ],
    tags: [
      { tag: TAG_QUEN, source: "staff" },
      { tag: TAG_HOAN, source: "rule" },
    ],
    version: 4,
    createdAt: "2026-03-01T00:00:00Z",
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["customer.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Write {
  method: string;
  path: string;
  key: string | null;
  body: Record<string, unknown>;
}

export interface Fake {
  grants: Grant[];
  list: CustomerListItem[];
  listCalls: URLSearchParams[];
  customers: Map<string, Customer>;
  candidates: MergeCandidate[];
  timeline: TimelineItem[];
  tags: CustomerTag[];
  rules: Map<string, CustomerTagRule>;
  imports: CustomerImport[];
  writes: Write[];
  /** Đường đọc trả 500 internal (ca lỗi API). */
  failReads: RegExp[];
  /** Trả lần lượt cho mỗi yêu cầu ghi: mã HTTP và thân. Hết thì trả đối tượng hiện tại. */
  replies: { status: number; body: unknown }[];
}

const NOT_FOUND = { error: "not_found", request_id: "t" };
const reply = (body: unknown, status = 200) => Response.json(body, { status });
const found = (v: unknown) => (v === undefined ? reply(NOT_FOUND, 404) : reply(v));

/** Đọc thân JSON của yêu cầu ghi thành đối tượng (thân rỗng hay không phải đối tượng → {}). */
async function bodyOf(req: Request): Promise<Record<string, unknown>> {
  const raw = await req.text();
  const v: unknown = raw === "" ? {} : JSON.parse(raw);
  return typeof v === "object" && v !== null && !Array.isArray(v) ? Object.fromEntries(Object.entries(v)) : {};
}

type Handler = (m: RegExpExecArray, q: URLSearchParams) => Response;

/** Đường đọc của API giả: mẫu đường dẫn → trả lời (đường đầu tiên khớp thắng). */
function readRoutes(fake: Fake): [RegExp, Handler][] {
  return [
    [/^\/api\/v1\/me$/, () => reply({ userId: ME, email: "sale@elimaz.vn", grants: fake.grants } satisfies Me)],
    [
      /^\/api\/v1\/customers$/,
      (_m, q) => {
        fake.listCalls.push(q);
        const term = q.get("q");
        const items = term === null ? fake.list : search(fake.list, term);
        return reply({ items: q.get("tagId") === null ? items : items.slice(0, 1) });
      },
    ],
    [/^\/api\/v1\/customers\/[^/]+\/timeline$/, () => reply({ items: fake.timeline })],
    [/^\/api\/v1\/customers\/[^/]+\/merge-candidates$/, () => reply({ items: fake.candidates })],
    [/^\/api\/v1\/customers\/([^/]+)$/, (m) => found(fake.customers.get(m[1] ?? ""))],
    [/^\/api\/v1\/customer-tags$/, () => reply({ items: fake.tags })],
    [/^\/api\/v1\/customer-tags\/([^/]+)\/rule$/, (m) => found(fake.rules.get(m[1] ?? ""))],
    [
      /^\/api\/v1\/customer-imports\/[^/]+$/,
      () => found(fake.imports.length > 1 ? fake.imports.shift() : fake.imports[0]),
    ],
  ];
}

/** Trả lời mặc định cho yêu cầu ghi (khi không có câu trả lời xếp sẵn trong fake.replies). */
function writeReply(fake: Fake, p: string): Response {
  if (p === "/api/v1/files/uploads") {
    return reply({ fileId: FILE, putUrl: "https://kho.test/put", headers: {}, expiresAt: "2026-11-19T02:00:00Z" }, 201);
  }
  if (p.startsWith("/api/v1/files/")) return reply({ id: FILE, status: "ready" });
  if (p === "/api/v1/customer-imports") return reply({ importId: IMP }, 202);
  if (p.endsWith("/notes")) {
    return reply({ id: "n1", body: "x", author: { id: ME, name: "Tôi" }, createdAt: "2026-11-19T01:00:00Z" }, 201);
  }
  const id = /^\/api\/v1\/customers\/([^/]+)/.exec(p)?.[1];
  return id === undefined ? reply({}) : found(fake.customers.get(id));
}

const digits = (s: string) => s.replace(/\D/g, "");

/** Tìm giả theo luật của customer.Search: số → đuôi SĐT; chữ → tên (không dấu đơn giản: chứa chuỗi). */
function search(list: CustomerListItem[], q: string): CustomerListItem[] {
  if (/^KH\d+$/i.test(q))
    return list.filter((c) => c.code === q.toUpperCase()).map((c) => ({ ...c, matchedBy: "code" }));
  if (/^[\d\s.+-]+$/.test(q)) {
    const d = digits(q);
    return list
      .filter((c) => c.phones.some((p) => digits(p).endsWith(d)))
      .map((c) => ({ ...c, matchedBy: "phone_tail" as const }));
  }
  return list
    .filter((c) => c.fullName.toLowerCase().includes(q.toLowerCase()))
    .map((c) => ({ ...c, matchedBy: "name" }));
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf(),
    list: [],
    listCalls: [],
    customers: new Map(),
    candidates: [],
    timeline: [],
    tags: [TAG_QUEN, TAG_HOAN, TAG_SI],
    rules: new Map(),
    imports: [],
    writes: [],
    replies: [],
    failReads: [],
  };
  const routes = readRoutes(fake);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      const p = url.pathname;
      if (req.method === "GET") {
        if (fake.failReads.some((re) => re.test(p))) return reply({ error: "internal", request_id: "t" }, 500);
        for (const [re, handle] of routes) {
          const m = re.exec(p);
          if (m !== null) return handle(m, url.searchParams);
        }
        return reply(NOT_FOUND, 404);
      }
      const body = await bodyOf(req);
      fake.writes.push({ method: req.method, path: p, key: req.headers.get("Idempotency-Key"), body });
      const queued = fake.replies.shift();
      return queued === undefined ? writeReply(fake, p) : reply(queued.body, queued.status);
    }),
  );
  return fake;
}

/** Dựng các màn của route.tsx ở đường `path` (cùng trang đơn giả cho liên kết sang màn Đơn). */
export function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [...screens.map((s) => ({ path: s.path, element: s.element })), { path: "/don/:id", element: <p>Trang đơn</p> }],
    { initialEntries: [path] },
  );
  const view = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { qc, ...view };
}

/** Phần tử thứ i; thiếu thì test hỏng với câu rõ (thay cho ép kiểu hay dấu !). */
export function nth<T>(xs: readonly T[], i: number): T {
  const x = xs[i];
  if (x === undefined) throw new Error(`thiếu phần tử thứ ${String(i)}`);
  return x;
}
