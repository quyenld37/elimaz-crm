// Màn Nhập Excel (kế hoạch M1 Task 52; mockup docs/mockups/m1/nhap-excel.html, trạng thái A–F) ở 1512 × 767 và
// 390 × 844, API giả (e2e/fixtures.ts). Luật kiểm tệp thật ở máy chủ (Task 47: lỗi từng dòng, cột của tệp mẫu —
// TestImportTemplate, TestTemplateRoundTrip; Task 323: áp dụng). Ở đây kiểm màn: tải mẫu, tải tệp lên đúng chuỗi
// createUpload → PUT → completeUpload → createCatalogImport, hiện đúng lỗi từng dòng, xem trước, áp dụng có khóa chống
// trùng và version, tệp đã nhập, tệp sai định dạng, áp dụng thất bại.
import type { Page } from "@playwright/test";
import type { CatalogImport, CatalogImportError, CatalogImportSummary } from "../src/api";
import {
  expect,
  forEachViewport,
  json,
  noHorizontalScroll,
  test,
  writeRecorder,
  type FakeApi,
  type WriteLog,
} from "./fixtures";

const LONG = "san-pham-thang-11-bo-suu-tap-thu-dong-dam-vay-ao-so-mi-cong-so-mau-200.xlsx";
const XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
const TEMPLATE_BYTES = Buffer.from("PK\u0003\u0004 tep mau gia");
/** 10:02 18/11 giờ Việt Nam. */
const CREATED = "2026-11-18T03:02:00Z";

// Bảy lỗi của trạng thái C trong mockup, chép nguyên văn đầu ra của máy chủ cho tệp mẫu lỗi loi-tung-dong.xlsx
// (importer.Validate, TestValidate_SevenSeededErrors): cột là tiêu đề cột của tệp mẫu (có * ở cột bắt buộc), mã là mã
// Go (E-08 review độc lập M1).
const SEVEN: CatalogImportError[] = [
  [5, "Tên mẫu*", "missing_required", "Thiếu Tên mẫu*"],
  [9, "Giá bán*", "invalid_price", 'Giá bán* "350,5" không hợp lệ: số nguyên đồng không âm, ví dụ 350000 hoặc 350.000'],
  [12, "SKU*", "duplicate_sku_in_file", "SKU M001-2 trùng dòng 3"],
  [20, "Danh mục", "unknown_category", 'Danh mục "Áo > Không có" không có'],
  [31, "Mã màu", "invalid_color_hex", 'Mã màu "red" phải có dạng #RRGGBB, ví dụ #FF0000'],
  [44, "Trạng thái", "invalid_status", 'Trạng thái "Bán" không có: chọn Đang nhập, Đang bán hoặc Ngừng bán'],
  [50, "Tên mẫu*", "inconsistent_product_fields", "Tên mẫu* khác dòng 47 cùng mã mẫu M010"],
].map(([row, column, code, message]) => ({
  row: Number(row),
  column: String(column),
  code: String(code),
  message: String(message),
}));

const FILE_ERR: CatalogImportError = {
  row: 0,
  column: "",
  code: "file_not_xlsx",
  message: "Tệp không phải Excel .xlsx đọc được: hãy mở bằng Excel và lưu lại dạng .xlsx",
};
// Lỗi lúc áp khi SKU vừa bị mẫu khác lấy (importer.Recheck).
const STALE: CatalogImportError = {
  row: 14,
  column: "SKU*",
  code: "sku_belongs_to_other_product",
  message: 'SKU "AO01-DEN-S" vừa được dùng cho mẫu "AO02" sau khi kiểm tệp',
};

/** Câu của màn khi thiếu files.upload (ImportPage.tsx TEXT_NO_UPLOAD_RIGHT). */
const TEXT_NO_UPLOAD_RIGHT =
  "Tài khoản của bạn chưa có quyền tải tệp lên nên chưa chọn tệp được. Nhờ Chủ shop cấp quyền.";

const EMPTY: CatalogImportSummary = { models: 0, create: 0, update: 0, unchanged: 0 };

interface Fake {
  log: WriteLog;
  puts: { url: string; headers: Record<string, string>; size: number }[];
}

/** API giả có trạng thái: kết quả kiểm theo tên tệp; tệp đã áp lần trước → duplicate. */
async function importApi(
  api: FakeApi,
  page: Page,
  perms: readonly string[] = ["catalog.read", "catalog.import", "files.upload", "realtime.subscribe"],
): Promise<Fake> {
  const fake: Fake = { log: { writes: [], failNext: null }, puts: [] };
  const rec = writeRecorder(fake.log);
  const names = new Map<string, string>(); // fileId → tên tệp
  const imports = new Map<string, CatalogImport>();
  const pending = new Map<string, CatalogImport>(); // kết quả trả ở lần đọc kế tiếp
  const applied = new Map<string, string>(); // tên tệp → id bản nhập đã áp
  let n = 0;
  const id = (p: string) => `0195f3b2-0000-7000-8000-${p}${String(++n).padStart(8, "0")}`;

  await api.live({
    grants: perms.map((permission) => ({
      permission,
      scope: { type: "all", id: "" },
    })),
  });
  await api.catalog();
  await api.on("**/api/v1/catalog/imports/template", (r) =>
    r.fulfill({
      status: 200,
      contentType: XLSX,
      headers: { "content-disposition": 'attachment; filename="mau-nhap-san-pham.xlsx"' },
      body: TEMPLATE_BYTES,
    }),
  );
  await api.on("**/api/v1/files/uploads", async (r) => {
    if (await rec.fail(r)) return;
    const body = rec.record(r);
    const fileId = id("0000");
    names.set(fileId, (body as { fileName: string }).fileName);
    await json(r, 201, {
      fileId,
      putUrl: `/kho-gia/${fileId}`,
      headers: { "x-amz-checksum-sha256": "abc" },
      expiresAt: "2026-11-18T03:20:00Z",
    });
  });
  await page.route("**/kho-gia/**", async (r) => {
    fake.puts.push({
      url: r.request().url(),
      headers: r.request().headers(),
      size: r.request().postDataBuffer()?.length ?? 0,
    });
    await r.fulfill({ status: 200 });
  });
  await api.on(/\/api\/v1\/files\/[^/]+\/complete$/, async (r) => {
    if (await rec.fail(r)) return;
    rec.record(r);
    const fid = new URL(r.request().url()).pathname.split("/")[4] ?? "";
    await json(r, 200, { id: fid, purpose: "catalog_import", status: "uploaded", variants: [] });
  });
  await api.on(/\/api\/v1\/catalog\/imports$/, async (r) => {
    if (await rec.fail(r)) return;
    const body = rec.record(r) as { fileId: string };
    const name = names.get(body.fileId) ?? "";
    const base: CatalogImport = {
      id: id("1000"),
      fileId: body.fileId,
      status: "validating",
      summary: EMPTY,
      errors: [],
      version: 1,
      createdAt: CREATED,
    };
    imports.set(base.id, base);
    let done: CatalogImport;
    const dup = applied.get(name);
    if (dup !== undefined) done = { ...base, status: "duplicate", duplicateOf: dup, version: 2 };
    else if (name.includes("loi-tung-dong"))
      done = { ...base, status: "invalid", errors: SEVEN, summary: { ...EMPTY, errors: 7 }, version: 2 };
    else if (name.includes("hong"))
      done = { ...base, status: "invalid", errors: [FILE_ERR], summary: { ...EMPTY, errors: 1 }, version: 2 };
    else
      done = { ...base, status: "ready", summary: { models: 200, create: 1000, update: 0, unchanged: 0 }, version: 2 };
    pending.set(base.id, done);
    names.set(base.id, name);
    await json(r, 202, base);
  });
  await api.on(/\/api\/v1\/catalog\/imports\/[^/]+$/, async (r) => {
    if (r.request().method() !== "GET" || r.request().url().endsWith("/template")) return r.fallback();
    const iid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    const next = pending.get(iid);
    if (next !== undefined) {
      imports.set(iid, next);
      pending.delete(iid);
    }
    const cur = imports.get(iid);
    if (cur === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    return json(r, 200, cur);
  });
  await api.on(/\/api\/v1\/catalog\/imports\/[^/]+\/apply$/, async (r) => {
    if (await rec.fail(r)) return;
    const body = rec.record(r) as { version: number };
    const iid = new URL(r.request().url()).pathname.split("/")[5] ?? "";
    const cur = imports.get(iid);
    if (cur === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    if (cur.version !== body.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
    const applying = { ...cur, status: "applying" as const, version: cur.version + 1 };
    imports.set(iid, applying);
    const name = names.get(iid) ?? "";
    if (name.includes("doi-sau-kiem"))
      pending.set(iid, { ...applying, status: "failed", errors: [STALE], version: applying.version + 1 });
    else {
      pending.set(iid, { ...applying, status: "applied", version: applying.version + 1 });
      applied.set(name, iid);
    }
    return json(r, 202, applying);
  });
  return fake;
}

const file = (name: string, size = 2048) => ({ name, mimeType: XLSX, buffer: Buffer.alloc(size, 1) });

async function pick(page: Page, name: string, size?: number) {
  await page.locator('input[type="file"]').setInputFiles(file(name, size));
}

test.use({ timezoneId: "Asia/Ho_Chi_Minh" });

forEachViewport(() => {
  test("tải tệp mẫu: tệp .xlsx đúng tên và đúng nội dung máy chủ trả", async ({ page, api }) => {
    await importApi(api, page);
    await page.goto("/nhap-excel");
    await expect(page.getByRole("heading", { name: "Nhập sản phẩm từ Excel" })).toBeVisible();
    const [dl] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: "Tải tệp mẫu (.xlsx)" }).click(),
    ]);
    expect(dl.suggestedFilename()).toBe("mau-nhap-san-pham.xlsx");
    const path = await dl.path();
    const { readFile } = await import("node:fs/promises");
    expect(Buffer.compare(await readFile(path), TEMPLATE_BYTES)).toBe(0);
    await expect(page.locator(".steps li.is-done")).toHaveText(["1Tải tệp mẫu"]);
    await noHorizontalScroll(page);
  });

  test("tệp có 7 lỗi: bảng lỗi đúng dòng, cột, câu tiếng Việt; không có nút áp dụng", async ({ page, api }) => {
    const fake = await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, "loi-tung-dong.xlsx");
    await expect(page.getByText("Chưa ghi gì: tệp có 7 lỗi.")).toBeVisible();
    const rows = page.locator(".etbl tbody tr");
    await expect(rows).toHaveCount(7);
    for (const [i, e] of SEVEN.entries()) {
      const cells = rows.nth(i).locator("td");
      await expect(cells.nth(0)).toHaveText(String(e.row));
      // Ô "Cột" bỏ dấu * của cột bắt buộc như mockup; câu lỗi giữ nguyên văn máy chủ.
      await expect(cells.nth(1)).toHaveText(e.column.replace(/\*$/, ""));
      await expect(cells.nth(2)).toHaveText(e.message);
    }
    await expect(page.getByRole("button", { name: /^Áp dụng/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Chọn tệp đã sửa" })).toBeVisible();
    // Chuỗi ghi: xin đường, PUT kèm header ký sẵn, báo xong, tạo bản nhập — mỗi lần một khóa riêng.
    expect(fake.log.writes.map((w) => w.path.replace(/[0-9a-f-]{36}/, ":id"))).toEqual([
      "/api/v1/files/uploads",
      "/api/v1/files/:id/complete",
      "/api/v1/catalog/imports",
    ]);
    expect(fake.log.writes[0]?.body).toEqual({
      purpose: "catalog_import",
      fileName: "loi-tung-dong.xlsx",
      contentType: XLSX,
      sizeBytes: 2048,
    });
    expect(new Set(fake.log.writes.map((w) => w.key)).size).toBe(3);
    expect(fake.log.writes.every((w) => w.key !== null)).toBe(true);
    expect(fake.puts).toHaveLength(1);
    expect(fake.puts[0]?.headers["x-amz-checksum-sha256"]).toBe("abc");
    expect(fake.puts[0]?.size).toBe(2048);
    await noHorizontalScroll(page);
  });

  test("tệp 200 mẫu hợp lệ: xem trước, áp dụng, đã nhập; tải lại cùng tệp → đã nhập lúc …", async ({ page, api }) => {
    const fake = await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, LONG);
    await expect(page.getByText("Đang kiểm")).toBeVisible();
    await expect(page.getByRole("heading", { name: `${LONG} sẵn sàng` })).toBeVisible();
    const kpi = (label: string) => page.locator(".kpi", { hasText: label }).locator(".v");
    await expect(kpi("Mẫu")).toHaveText("200");
    await expect(kpi("Biến thể tạo mới")).toHaveText("1.000");
    await expect(kpi("Biến thể cập nhật")).toHaveText("0");
    await expect(kpi("Giữ nguyên")).toHaveText("0");
    await expect(page.getByText("hoặc đủ 1.000 biến thể, hoặc không gì cả")).toBeVisible();
    await noHorizontalScroll(page);

    await page.getByRole("button", { name: "Áp dụng…" }).click();
    const dlg = page.getByRole("dialog", { name: "Áp dụng tệp nhập?" });
    await expect(dlg).toContainText("Tạo 1.000 biến thể, cập nhật 0 biến thể của 200 mẫu.");
    await dlg.getByRole("button", { name: "Áp dụng", exact: true }).click();
    await expect(page.getByText("Đã nhập xong.")).toBeVisible();
    await expect(page.getByText("200 mẫu: tạo 1.000 biến thể, cập nhật 0.")).toBeVisible();
    await expect(page.getByRole("link", { name: "Xem danh sách sản phẩm" })).toHaveAttribute("href", "/san-pham");
    const apply = fake.log.writes.find((w) => w.path.endsWith("/apply"));
    expect(apply?.body).toEqual({ version: 2 });
    expect(apply?.key).not.toBeNull();

    // Lần hai cùng tệp: máy chủ báo duplicate; giờ là giờ tạo bản nhập đã áp.
    await page.goto("/nhap-excel");
    await pick(page, LONG);
    await expect(page.getByText("Tệp này đã nhập lúc 10:02 18/11.")).toBeVisible();
    await expect(page.getByText("Nội dung giống hệt lần đó nên không có gì thay đổi.", { exact: false })).toBeVisible();
    await expect(page.getByRole("button", { name: /^Áp dụng/ })).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  test("rời trang rồi quay lại: kết quả kiểm vẫn ở đó", async ({ page, api }) => {
    await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, "mau-200.xlsx");
    await expect(page.getByRole("heading", { name: "mau-200.xlsx sẵn sàng" })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("heading", { name: "mau-200.xlsx sẵn sàng" })).toBeVisible();
  });

  test("tệp không đọc được: sai đuôi hay quá 5 MB chặn ngay, tệp hỏng do máy chủ báo", async ({ page, api }) => {
    const fake = await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, "san-pham.csv");
    await expect(page.getByText("Không đọc được tệp.")).toBeVisible();
    await pick(page, "to.xlsx", 5 * 1024 * 1024 + 1);
    await expect(page.getByText("Không đọc được tệp.")).toBeVisible();
    expect(fake.log.writes).toHaveLength(0);

    await pick(page, "hong.xlsx");
    await expect(page.getByText(FILE_ERR.message)).toBeVisible();
    await expect(page.getByText("Không đọc được tệp.")).toBeVisible();
    await expect(page.locator(".etbl")).toHaveCount(0);
    await noHorizontalScroll(page);
  });

  // E-02 (review độc lập M1): createUpload, completeUpload đòi files.upload, tách khỏi catalog.import (vai trò tự định
  // nghĩa có thể có cái này mà thiếu cái kia). Thiếu thì màn báo trước thay vì để chọn tệp rồi nhận 403.
  test("có catalog.import mà thiếu files.upload: tải mẫu được, không có ô chọn tệp, câu báo cần quyền", async ({
    page,
    api,
  }) => {
    const fake = await importApi(api, page, ["catalog.read", "catalog.import", "realtime.subscribe"]);
    await page.goto("/nhap-excel");
    await expect(page.getByText(TEXT_NO_UPLOAD_RIGHT)).toBeVisible();
    await expect(page.getByRole("button", { name: "Tải tệp mẫu (.xlsx)" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Chọn tệp" })).toHaveCount(0);
    await expect(page.getByText("Kéo tệp .xlsx vào đây")).toHaveCount(0);
    expect(fake.log.writes).toEqual([]);
    await noHorizontalScroll(page);
  });

  test("áp dụng thất bại vì dữ liệu vừa đổi: không ghi gì, nêu dòng", async ({ page, api }) => {
    await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, "doi-sau-kiem.xlsx");
    await page.getByRole("button", { name: "Áp dụng…" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Áp dụng", exact: true }).click();
    await expect(page.getByText("Không ghi được, không có gì thay đổi.")).toBeVisible();
    await expect(page.getByText(`Dòng 14: ${STALE.message}`)).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("tạo bản nhập lỗi: câu lỗi, chọn lại tệp được", async ({ page, api }) => {
    const fake = await importApi(api, page);
    await page.goto("/nhap-excel");
    fake.log.failNext = { status: 409, code: "file_purpose_not_allowed" };
    await pick(page, "mau-200.xlsx");
    await expect(page.getByText("Không tải được loại tệp này vào chỗ này")).toBeVisible();
    await pick(page, "mau-200.xlsx");
    await expect(page.getByRole("heading", { name: "mau-200.xlsx sẵn sàng" })).toBeVisible();
  });

  test("áp dụng bị từ chối vì người khác vừa đổi (version_conflict): câu lỗi, vẫn ở xem trước", async ({
    page,
    api,
  }) => {
    const fake = await importApi(api, page);
    await page.goto("/nhap-excel");
    await pick(page, "mau-200.xlsx");
    await page.getByRole("button", { name: "Áp dụng…" }).click();
    fake.log.failNext = { status: 409, code: "version_conflict" };
    await page.getByRole("dialog").getByRole("button", { name: "Áp dụng", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Người khác vừa sửa mục này. Tải lại để xem bản mới rồi sửa lại");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "mau-200.xlsx sẵn sàng" })).toBeVisible();
  });

  test("mở bản nhập không có: câu lỗi và khung bắt đầu", async ({ page, api }) => {
    await importApi(api, page);
    await page.goto("/nhap-excel?id=0195f3b2-0000-7000-8000-999999999999&tep=x.xlsx");
    await expect(page.getByText("Không mở được bản nhập.")).toBeVisible();
    await expect(page.getByText("Không tìm thấy, có thể đã bị xóa")).toBeVisible();
    await expect(page.getByRole("button", { name: "Chọn tệp", exact: true })).toBeVisible();
  });
});
