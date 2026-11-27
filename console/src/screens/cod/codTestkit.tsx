// API giả (fetch) và cách dựng cho test vitest của màn Đối soát COD. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { vi } from "vitest";
import type {
  CarrierAccount,
  CodDiscrepancy,
  CodMatchTotal,
  CodOverdue,
  CodStatement,
  CodStatementLine,
  Grant,
  Me,
} from "../../api";
import { NOT_FOUND, bodyOf, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const ME = "0195f3b2-0000-7000-8000-0000000000e1";
export const ACC_VTP = "0195f3b2-0000-7000-8000-0000000ac001";
export const ACC_SPX = "0195f3b2-0000-7000-8000-0000000ac002";
export const S_PARSING = "0195f3b2-0000-7000-8000-00000000d001";
export const S_FAILED = "0195f3b2-0000-7000-8000-00000000d002";
export const S_MATCHED = "0195f3b2-0000-7000-8000-00000000d003";
export const S_CONFIRMED = "0195f3b2-0000-7000-8000-00000000d004";
export const FILE = "0195f3b2-0000-7000-8000-00000000f101";
export const ORDER = "0195f3b2-0000-7000-8000-00000000a101";
export const SHIP = "0195f3b2-0000-7000-8000-00000000b101";

export const KE = { id: ME, name: "Mai Thị Kế" };

export function statement(p: Partial<CodStatement> = {}): CodStatement {
  return {
    id: S_MATCHED,
    carrierKey: "vtp",
    carrierAccountId: ACC_VTP,
    carrierAccountName: "Viettel Post — Kho chính",
    fileId: FILE,
    format: "elimaz_v1",
    periodFrom: "2026-11-08",
    periodTo: "2026-11-14",
    declaredTotal: 127_896_789,
    status: "matched",
    lineCount: 206,
    importedBy: KE,
    createdAt: "2026-11-21T09:05:00Z",
    version: 4,
    ...p,
  };
}

/** Bốn bảng kê của trạng thái A: đang phân tích, lỗi cột, chờ chốt, đã chốt. */
export function listA(): CodStatement[] {
  return [
    statement({
      id: S_PARSING,
      status: "parsing",
      lineCount: 0,
      periodFrom: undefined,
      periodTo: undefined,
      declaredTotal: undefined,
      createdAt: "2026-11-22T02:40:00Z",
    }),
    statement({
      id: S_FAILED,
      carrierKey: "best",
      carrierAccountId: ACC_SPX,
      carrierAccountName: "Best Express — Kho chính",
      status: "parse_failed",
      lineCount: 0,
      periodFrom: undefined,
      periodTo: undefined,
      declaredTotal: undefined,
      parseError: { code: "missing_column", column: "tien_cod" },
      createdAt: "2026-11-22T02:12:00Z",
    }),
    statement(),
    statement({
      id: S_CONFIRMED,
      carrierKey: "spx",
      carrierAccountName: "SPX — Kho chính",
      status: "confirmed",
      lineCount: 41,
      declaredTotal: 18_230_000,
      periodFrom: "2026-11-01",
      periodTo: "2026-11-07",
      confirmedAt: "2026-11-18T03:30:00Z",
      confirmedBy: KE,
      createdAt: "2026-11-18T03:00:00Z",
    }),
  ];
}

/** Tổng năm nhóm của trạng thái C (206 dòng, tổng Khớp 9 chữ số). */
export const TOTALS_C: CodMatchTotal[] = [
  { matchStatus: "amount_mismatch", count: 3, codAmount: 2_140_000, feeAmount: 90_000, shortAmount: 100_000 },
  { matchStatus: "duplicate", count: 1, codAmount: 690_000, feeAmount: 30_000, shortAmount: 0 },
  { matchStatus: "external", count: 4, codAmount: 1_610_000, feeAmount: 120_000, shortAmount: 0 },
  { matchStatus: "fee_only", count: 2, codAmount: 0, feeAmount: 50_000, shortAmount: 0 },
  { matchStatus: "matched", count: 196, codAmount: 123_456_789, feeAmount: 6_100_000, shortAmount: 0 },
];

export function line(p: Partial<CodStatementLine> = {}): CodStatementLine {
  return {
    id: `l-${String(p.lineNo ?? 12)}`,
    lineNo: 12,
    trackingCode: "1234567890001",
    shipmentId: SHIP,
    orderId: ORDER,
    orderCode: "DH0001101",
    codAmount: 1_200_000,
    feeAmount: 32_000,
    otherFee: 0,
    netAmount: 1_168_000,
    matchStatus: "amount_mismatch",
    expectedAmount: 1_290_000,
    diffAmount: -90_000,
    ...p,
  };
}

export const MISMATCH_LINES: CodStatementLine[] = [
  line(),
  line({
    lineNo: 57,
    trackingCode: "1234567890045",
    orderCode: "DH0001120",
    codAmount: 450_000,
    feeAmount: 28_000,
    expectedAmount: 460_000,
    diffAmount: -10_000,
  }),
  line({
    lineNo: 140,
    trackingCode: "1234567890102",
    orderCode: "DH0001133",
    codAmount: 490_000,
    feeAmount: 30_000,
    expectedAmount: 390_000,
    diffAmount: 100_000,
  }),
];

export const EXTERNAL_LINE = line({
  lineNo: 88,
  trackingCode: "0987654321777",
  shipmentId: undefined,
  orderId: undefined,
  orderCode: undefined,
  codAmount: 520_000,
  matchStatus: "external",
  expectedAmount: undefined,
  diffAmount: undefined,
});

export function account(p: Partial<CarrierAccount> = {}): CarrierAccount {
  return {
    id: ACC_VTP,
    carrierKey: "vtp",
    mode: "api",
    environment: "production",
    name: "Viettel Post — Kho chính",
    status: "active",
    warehouseId: "w1",
    isDefaultForWarehouse: true,
    showOnWeb: true,
    webSort: 1,
    hasCredentials: true,
    needsRewrap: false,
    version: 1,
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["cash.cod.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export const ACCOUNTANT = ["cash.statements.import", "cash.statements.confirm", "files.upload", "cash.adjust"];

export interface Detail {
  statement: CodStatement;
  totals: CodMatchTotal[];
  lines: CodStatementLine[];
  nextCursor?: string;
}

export interface Fake {
  grants: Grant[];
  statements: CodStatement[];
  details: Map<string, Detail>;
  detailCalls: URLSearchParams[];
  accounts: CarrierAccount[] | "forbidden";
  accountCalls: URLSearchParams[];
  discrepancies: CodDiscrepancy[];
  overdue: CodOverdue[];
  writes: Write[];
  /** Trả lần lượt cho mỗi yêu cầu ghi không phải tải tệp: mã HTTP và thân. */
  replies: { status: number; body: unknown }[];
  failReads: RegExp[];
}

type Handler = (m: RegExpExecArray, q: URLSearchParams) => Response;

function readRoutes(fake: Fake): [RegExp, Handler][] {
  return [
    [/^\/api\/v1\/me$/, () => reply({ userId: ME, email: "ke@elimaz.vn", grants: fake.grants } satisfies Me)],
    [/^\/api\/v1\/cod-statements$/, () => reply({ items: fake.statements })],
    [
      /^\/api\/v1\/cod-statements\/([^/]+)$/,
      (m, q) => {
        fake.detailCalls.push(q);
        const d = fake.details.get(m[1] ?? "");
        if (d === undefined) {
          const st = fake.statements.find((s) => s.id === m[1]);
          return st === undefined ? reply(NOT_FOUND, 404) : reply({ statement: st, totals: [], lines: [] });
        }
        const ms = q.get("matchStatus");
        const lines = ms === null ? d.lines : d.lines.filter((l) => l.matchStatus === ms);
        return reply({ ...d, lines, ...(d.nextCursor === undefined ? {} : { nextCursor: d.nextCursor }) });
      },
    ],
    [/^\/api\/v1\/cod\/discrepancies$/, () => reply({ items: fake.discrepancies })],
    [/^\/api\/v1\/cod\/overdue$/, () => reply({ items: fake.overdue })],
    [
      /^\/api\/v1\/carrier-accounts$/,
      (_m, q) =>
        fake.accountCalls.push(q) > 0 && fake.accounts === "forbidden"
          ? reply({ error: "forbidden", request_id: "t" }, 403)
          : reply({ items: fake.accounts }),
    ],
  ];
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf(...ACCOUNTANT),
    statements: listA(),
    details: new Map(),
    detailCalls: [],
    accounts: [account(), account({ id: ACC_SPX, carrierKey: "spx", name: "SPX — Kho chính", status: "revoked" })],
    accountCalls: [],
    discrepancies: [],
    overdue: [],
    writes: [],
    replies: [],
    failReads: [],
  };
  const routes = readRoutes(fake);
  const read = (url: URL): Response => {
    if (fake.failReads.some((re) => re.test(url.pathname))) return reply({ error: "internal", request_id: "t" }, 500);
    const hit = routes.map(([re, h]) => [re.exec(url.pathname), h] as const).find(([m]) => m !== null);
    return hit?.[0] == null ? reply(NOT_FOUND, 404) : hit[1](hit[0], url.searchParams);
  };
  const write = async (req: Request, path: string): Promise<Response> => {
    fake.writes.push({ method: req.method, path, key: req.headers.get("Idempotency-Key"), body: await bodyOf(req) });
    if (path === "/api/v1/files/uploads") {
      return reply(
        { fileId: FILE, putUrl: "https://kho.test/put", headers: {}, expiresAt: "2026-11-22T02:00:00Z" },
        201,
      );
    }
    if (path.startsWith("/api/v1/files/")) return reply({ id: FILE, status: "ready" });
    const next = fake.replies.shift();
    return next === undefined ? reply({}) : reply(next.body, next.status);
  };
  vi.stubGlobal(
    "fetch",
    vi.fn((req: Request) => {
      const url = new URL(req.url);
      return req.method === "GET" ? Promise.resolve(read(url)) : write(req, url.pathname);
    }),
  );
  return fake;
}

/** Dựng các màn của route.tsx ở đường `path` (cùng trang đơn giả cho liên kết sang màn Đơn). */
export function renderAt(path: string) {
  return renderScreens(screens, path, [{ path: "/don/:id", element: <p>Trang đơn</p> }]);
}

/** PUT giả lên kho tệp (không chạm mạng). */
export const okPut = () => Promise.resolve();
