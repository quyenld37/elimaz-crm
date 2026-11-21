// Màn Đối soát COD (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html) ở 1512 × 767, 390 × 844 và 360,
// API giả. Hành vi (nhập, tệp trùng, chốt 409, lọc nhóm) kiểm ở test vitest của src/screens/cod; ở đây kiểm bố cục
// không tràn (cả ô tiền 9 chữ số nằm trọn trong ô) và chụp test-results/cod-*.png để so với
// docs/mockups/m3/png/doi-soat-cod-*.png.
import type { Page } from "@playwright/test";
import type {
  CarrierAccount,
  CodDiscrepancy,
  CodMatchTotal,
  CodOverdue,
  CodStatement,
  CodStatementLine,
} from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000c${String(n).padStart(4, "0")}`;
const KE = { id: id(99), name: "Mai Thị Kế" };
const GRANTS = [
  ...OWNER_GRANTS,
  ...["cash.cod.read", "cash.statements.import", "cash.statements.confirm", "files.upload", "cash.adjust"].map(
    (permission) => ({ permission, scope: { type: "all" as const, id: "" } }),
  ),
];

const st = (p: Partial<CodStatement>): CodStatement => ({
  id: id(3),
  carrierKey: "vtp",
  carrierAccountId: id(50),
  carrierAccountName: "Viettel Post — Kho chính",
  fileId: id(60),
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
});
const LIST: CodStatement[] = [
  st({
    id: id(1),
    status: "parsing",
    lineCount: 0,
    periodFrom: undefined,
    periodTo: undefined,
    declaredTotal: undefined,
    createdAt: "2026-11-22T02:40:00Z",
  }),
  st({
    id: id(2),
    carrierKey: "best",
    carrierAccountName: "Best Express — Kho chính",
    status: "parse_failed",
    lineCount: 0,
    periodFrom: undefined,
    periodTo: undefined,
    declaredTotal: undefined,
    parseError: { code: "missing_column", column: "tien_cod" },
    createdAt: "2026-11-22T02:12:00Z",
  }),
  st({}),
  st({
    id: id(4),
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
const TOTALS: CodMatchTotal[] = [
  { matchStatus: "amount_mismatch", count: 3, codAmount: 2_140_000, feeAmount: 90_000, shortAmount: 100_000 },
  { matchStatus: "duplicate", count: 1, codAmount: 690_000, feeAmount: 30_000, shortAmount: 0 },
  { matchStatus: "external", count: 4, codAmount: 1_610_000, feeAmount: 120_000, shortAmount: 0 },
  { matchStatus: "fee_only", count: 2, codAmount: 0, feeAmount: 50_000, shortAmount: 0 },
  { matchStatus: "matched", count: 196, codAmount: 123_456_789, feeAmount: 6_100_000, shortAmount: 0 },
];
const ln = (
  no: number,
  track: string,
  order: string,
  cod: number,
  fee: number,
  exp: number,
  diff: number,
): CodStatementLine => ({
  id: id(200 + no),
  lineNo: no,
  trackingCode: track,
  shipmentId: id(300 + no),
  orderId: id(400 + no),
  orderCode: order,
  codAmount: cod,
  feeAmount: fee,
  otherFee: 0,
  netAmount: cod - fee,
  matchStatus: "amount_mismatch",
  expectedAmount: exp,
  diffAmount: diff,
});
const LINES = [
  ln(12, "1234567890001", "DH0001101", 1_200_000, 32_000, 1_290_000, -90_000),
  ln(57, "1234567890045", "DH0001120", 450_000, 28_000, 460_000, -10_000),
  ln(140, "1234567890102", "DH0001133", 490_000, 30_000, 390_000, 100_000),
];
const ACCOUNTS: CarrierAccount[] = [
  {
    id: id(50),
    carrierKey: "vtp",
    mode: "api",
    environment: "production",
    name: "Viettel Post — Kho chính",
    status: "active",
    warehouseId: "w",
    isDefaultForWarehouse: true,
    showOnWeb: true,
    webSort: 1,
    hasCredentials: true,
    needsRewrap: false,
    version: 1,
  },
];
const DISC: CodDiscrepancy[] = [
  {
    kind: "statement_line",
    statementId: id(3),
    lineNo: 12,
    shipmentId: id(312),
    orderId: id(412),
    orderCode: "DH0001101",
    carrierKey: "vtp",
    trackingCode: "1234567890001",
    codAmount: 1_200_000,
    expectedAmount: 1_290_000,
    diff: -90_000,
    matchStatus: "amount_mismatch",
  },
  {
    kind: "statement_line",
    statementId: id(3),
    lineNo: 140,
    shipmentId: id(340),
    orderId: id(440),
    orderCode: "DH0001133",
    carrierKey: "vtp",
    trackingCode: "1234567890102",
    codAmount: 490_000,
    expectedAmount: 390_000,
    diff: 100_000,
    matchStatus: "amount_mismatch",
  },
  {
    kind: "remitted_then_returned",
    shipmentId: id(350),
    orderId: id(450),
    orderCode: "DH0000987",
    carrierKey: "best",
    trackingCode: "BE0098800000VN",
    codAmount: 1_290_000,
    expectedAmount: 0,
    diff: -1_290_000,
  },
];
const OVERDUE: CodOverdue[] = [
  {
    shipmentId: id(501),
    orderId: id(601),
    orderCode: "DH0001001",
    carrierKey: "vtp",
    trackingCode: "1234567812345",
    deliveredAt: "2026-11-03T05:00:00Z",
    carrierHolding: 112_450_000,
    days: 19,
  },
  {
    shipmentId: id(502),
    orderId: id(602),
    orderCode: "DH0001010",
    carrierKey: "spx",
    trackingCode: "SPXVN0611111111",
    deliveredAt: "2026-11-06T05:00:00Z",
    carrierHolding: 650_000,
    days: 16,
  },
];

async function codApi(api: FakeApi, overdue: CodOverdue[] = OVERDUE) {
  await api.live({ grants: GRANTS });
  await api.on(/\/api\/v1\/cod-statements(\?|$)/, (r) =>
    r.request().method() === "POST"
      ? json(r, 409, { error: "statement_duplicate", request_id: "e2e", statementId: id(4) })
      : json(r, 200, { items: LIST }),
  );
  await api.on(/\/api\/v1\/cod-statements\/[^/?]+(\?|$)/, (r) => {
    const u = new URL(r.request().url());
    const sid = u.pathname.split("/").at(-1) ?? "";
    const head = LIST.find((s) => s.id === sid) ?? st({});
    const ms = u.searchParams.get("matchStatus");
    return json(r, 200, { statement: head, totals: TOTALS, lines: ms === "amount_mismatch" ? LINES : [] });
  });
  await api.on(/\/api\/v1\/carrier-accounts/, (r) => json(r, 200, { items: ACCOUNTS }));
  await api.on(/\/api\/v1\/files\/uploads/, (r) =>
    json(r, 201, { fileId: id(60), putUrl: "/kho-gia/put", headers: {}, expiresAt: "2026-11-22T02:00:00Z" }),
  );
  await api.on(/\/api\/v1\/files\/[^/]+\/complete/, (r) => json(r, 200, { id: id(60), status: "ready" }));
  await api.on(/\/api\/v1\/cod\/discrepancies/, (r) => json(r, 200, { items: DISC }));
  await api.on(/\/api\/v1\/cod\/overdue/, (r) => json(r, 200, { items: overdue }));
}

const shot = (page: Page, name: string, w: number) =>
  page.screenshot({ path: `test-results/cod-${name}-${String(w)}.png`, fullPage: true });

/** Mọi ô tiền nằm trọn trong ô bảng hay ô nhóm chứa nó (kể cả 9 chữ số). */
async function moneyInside(page: Page) {
  const bad = await page.evaluate(() =>
    [...document.querySelectorAll(".money")].flatMap((m) => {
      const box = m.closest("td, .grp, p, .box");
      if (box === null) return [];
      const a = m.getBoundingClientRect();
      const b = box.getBoundingClientRect();
      return a.left < b.left - 0.5 || a.right > b.right + 0.5 ? [m.textContent] : [];
    }),
  );
  expect(bad).toEqual([]);
}

forEachViewport((vp) => {
  test("danh sách bảng kê: bốn trạng thái, không tràn", async ({ page, api }) => {
    await codApi(api);
    await page.goto("/cod/statements");
    await expect(page.getByText('Thiếu cột "Tiền thu hộ"')).toBeVisible();
    await expect(page.getByRole("progressbar", { name: "Đang phân tích" })).toBeVisible();
    await noHorizontalScroll(page);
    await moneyInside(page);
    await shot(page, "danh-sach", vp.width);
  });

  test("hộp nhập: tệp trùng có liên kết bảng kê cũ, không tràn", async ({ page, api }) => {
    await codApi(api);
    await page.route("**/kho-gia/put", (r) => r.fulfill({ status: 200 }));
    await page.goto("/cod/statements");
    await page.getByRole("button", { name: "Nhập bảng kê" }).click();
    const dlg = page.getByRole("dialog", { name: "Nhập bảng kê COD" });
    await expect(dlg.getByRole("option", { name: "Viettel Post — Kho chính" })).toBeAttached();
    await dlg
      .getByLabel("Chọn tệp")
      .setInputFiles({ name: "BK_VTP_08-14_11.csv", mimeType: "text/csv", buffer: Buffer.alloc(86_016) });
    await expect(dlg.getByText(/đã tải lên/)).toBeVisible();
    await dlg.getByRole("button", { name: "Nhập", exact: true }).click();
    await expect(dlg.getByText("Tệp này đã nhập ngày 18/11")).toBeVisible();
    await noHorizontalScroll(page);
    await shot(page, "nhap", vp.width);
  });

  test("chi tiết bảng kê: năm nhóm tiền 9 chữ số, nhóm Lệch, thanh Chốt, không tràn", async ({ page, api }) => {
    await codApi(api);
    await page.goto(`/cod/statements/${id(3)}`);
    await expect(page.getByRole("link", { name: "DH0001101" })).toBeVisible();
    await expect(page.getByText("Không tính vào số liệu")).toBeVisible();
    await noHorizontalScroll(page);
    await moneyInside(page);
    await shot(page, "chi-tiet", vp.width);
  });

  test("lệch đối soát và quá hạn chưa có tiền (có dòng, 0 dòng), không tràn", async ({ page, api }) => {
    await codApi(api);
    await page.goto("/cod/discrepancies");
    await expect(page.getByText("Hãng chuyển tiền rồi báo hoàn", { exact: true })).toBeVisible();
    await noHorizontalScroll(page);
    await moneyInside(page);
    await shot(page, "lech", vp.width);
    await page.goto("/cod/overdue");
    await expect(page.getByText("112.450.000 ₫")).toBeVisible();
    await noHorizontalScroll(page);
    await moneyInside(page);
    await shot(page, "qua-han", vp.width);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("chi tiết và quá hạn rỗng không tràn ngang", async ({ page, api }) => {
    await codApi(api, []);
    await page.goto(`/cod/statements/${id(3)}`);
    await expect(page.getByRole("link", { name: "DH0001101" })).toBeVisible();
    await noHorizontalScroll(page);
    await moneyInside(page);
    await shot(page, "chi-tiet", 360);
    await page.goto("/cod/overdue");
    await expect(page.getByText("Không có đơn quá hạn chưa có tiền")).toBeVisible();
    await noHorizontalScroll(page);
  });
});
