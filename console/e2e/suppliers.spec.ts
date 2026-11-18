// Màn Nhà cung cấp (kế hoạch M1 Task 327; mockup docs/mockups/m1/kho-nha-cung-cap.html, trạng thái A–D) ở 1512 × 767
// và 390 × 844 (thêm 360 cho tên 200 ký tự), API giả (e2e/fixtures.ts). Luật thật ở API (Task 29: tên không trùng,
// version, quyền); ở đây kiểm màn gửi đúng yêu cầu, tên dài không tràn, các hộp báo theo mockup. Ảnh chụp lưu ở
// test-results/nha-cung-cap-*.png để so với docs/mockups/m1/png/kho-nha-cung-cap-{1512,390,360}.png. Ô chọn có thêm
// nhanh được kiểm ở test vitest SupplierPicker.test.tsx; ca trong phiếu nhập ở Task M1-51c.
import type { Route } from "@playwright/test";
import type { Supplier } from "../src/api";
import {
  OWNER_GRANTS,
  expect,
  forEachViewport,
  json,
  noHorizontalScroll,
  test,
  writeRecorder,
  type FakeApi,
  type WriteLog,
} from "./fixtures";

const NAME_200 =
  "Công ty TNHH Sản xuất Thương mại và Dịch vụ May mặc Xuất khẩu Thanh Bình Hưng Thịnh Phát, chi nhánh Khu công nghiệp Phố Nối A, tỉnh Hưng Yên, xưởng số 3 chuyên hàng lụa và voan cao cấp cho thương hiệu";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000d${String(n).padStart(4, "0")}`;

// Bốn dòng của trạng thái A trong mockup.
const ROWS: Supplier[] = [
  { id: id(1), name: NAME_200, phone: "0912345678", note: "Giao thứ 3 hằng tuần", status: "active", version: 1 },
  { id: id(2), name: "Xưởng Hoa", phone: "", note: "", status: "active", version: 1 },
  {
    id: id(3),
    name: "Xưởng may Hoa Sen",
    phone: "+842439361234",
    note: "Hàng voan, đặt trước 10 ngày",
    status: "active",
    version: 3,
  },
  {
    id: id(4),
    name: "Vải Ninh Hiệp cô Thủy",
    phone: "0987000111",
    note: "Ngừng lấy từ 09/2026",
    status: "archived",
    version: 2,
  },
];

const MANAGER = [
  ...OWNER_GRANTS,
  { permission: "inventory.suppliers.manage", scope: { type: "all" as const, id: "" } },
];

interface Fake extends WriteLog {
  list: Supplier[];
  reads: URLSearchParams[];
}

/** API giả: listSuppliers lọc q, includeArchived; createSupplier, updateSupplier theo version (409 version_conflict). */
async function supplierApi(api: FakeApi, opts: { manage?: boolean; list?: Supplier[] } = {}): Promise<Fake> {
  const fake: Fake = { list: [...(opts.list ?? ROWS)], reads: [], writes: [], failNext: null };
  const rec = writeRecorder(fake);
  await api.live({ grants: opts.manage === false ? OWNER_GRANTS : MANAGER });
  await api.on(/\/api\/v1\/inventory\/suppliers(\?|$)/, async (r: Route) => {
    const req = r.request();
    if (req.method() === "GET") {
      const p = new URL(req.url()).searchParams;
      fake.reads.push(p);
      const q = (p.get("q") ?? "").toLowerCase();
      const all = p.get("includeArchived") === "true";
      return json(
        r,
        200,
        fake.list.filter((s) => (all || s.status === "active") && s.name.toLowerCase().includes(q)),
      );
    }
    const body = rec.record(r) as { name: string; phone?: string; note?: string };
    if (await rec.fail(r)) return;
    const s: Supplier = {
      id: id(100 + fake.list.length),
      name: body.name,
      phone: (body.phone ?? "").replace(/[ .-]/g, ""),
      note: body.note ?? "",
      status: "active",
      version: 1,
    };
    fake.list.push(s);
    return json(r, 201, s);
  });
  await api.on(/\/api\/v1\/inventory\/suppliers\/[0-9a-f-]+$/, async (r: Route) => {
    const body = rec.record(r) as Partial<Supplier>;
    if (await rec.fail(r)) return;
    const sid = new URL(r.request().url()).pathname.split("/").at(-1);
    const old = fake.list.find((s) => s.id === sid);
    if (old === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    if (body.version !== old.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
    const next = { ...old, ...body, version: old.version + 1 };
    fake.list = fake.list.map((s) => (s.id === sid ? next : s));
    return json(r, 200, next);
  });
  return fake;
}

forEachViewport((vp) => {
  test("trạng thái A: tên 200 ký tự không tràn, hiện cả đã lưu trữ, số theo nhóm", async ({ page, api }) => {
    const fake = await supplierApi(api);
    await page.goto("/kho/nha-cung-cap");
    await expect(page.getByRole("heading", { name: "Nhà cung cấp", exact: true })).toBeVisible();
    if (vp.width >= 1000) {
      const link = page.locator("aside.side").getByRole("link", { name: "Nhà cung cấp" });
      await expect(link).toHaveAttribute("aria-current", "page");
    }
    await expect(page.getByText(NAME_200)).toBeVisible();
    await expect(page.getByText("Vải Ninh Hiệp cô Thủy")).toHaveCount(0);
    await page.getByRole("checkbox", { name: "Hiện cả đã lưu trữ" }).check();
    const old = page.getByRole("row").filter({ hasText: "Vải Ninh Hiệp cô Thủy" });
    await expect(old).toHaveClass(/is-muted/);
    await expect(old.getByText("Đã lưu trữ")).toBeVisible();
    await expect(old.getByRole("button", { name: "Dùng lại" })).toBeVisible();
    expect(fake.reads.at(-1)?.get("includeArchived")).toBe("true");
    const first = page.getByRole("row").filter({ hasText: NAME_200 });
    await expect(first.getByText("0912 345 678")).toBeVisible();
    await expect(page.getByRole("row").filter({ hasText: "Xưởng Hoa" }).first().getByText("—")).toHaveCount(2);
    // Tên dài xuống dòng trong ô, không đẩy bảng rộng ra.
    const name = first.locator(".s-name");
    const box = await name.boundingBox();
    expect(box?.width ?? 0).toBeLessThanOrEqual(vp.width >= 1000 ? 520 : vp.width);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/nha-cung-cap-${String(vp.width)}.png`, fullPage: true });
  });

  test("trạng thái B: thêm với số sai, tên trùng; sửa lại thì gửi một lần có khóa", async ({ page, api }) => {
    const fake = await supplierApi(api);
    await page.goto("/kho/nha-cung-cap");
    await page.getByRole("button", { name: "Thêm nhà cung cấp" }).click();
    const dlg = page.getByRole("dialog", { name: "Thêm nhà cung cấp" });
    await dlg.getByLabel("Tên *").fill("Xưởng Hoa");
    await dlg.getByLabel("Điện thoại").fill("09123");
    await dlg.getByRole("button", { name: "Lưu" }).click();
    await expect(dlg.getByText("Số điện thoại cần 8–15 chữ số, có thể bắt đầu bằng +.")).toBeVisible();
    expect(fake.writes).toHaveLength(0);
    await dlg.getByLabel("Điện thoại").fill("0911 222 333");
    fake.failNext = { status: 409, code: "supplier_name_taken" };
    await dlg.getByRole("button", { name: "Lưu" }).click();
    await expect(dlg.getByText("Tên này đã có. Chọn nhà cung cấp có sẵn hoặc đặt tên khác.")).toBeVisible();
    await dlg.getByLabel("Điện thoại").fill("09123");
    await dlg.getByRole("button", { name: "Lưu" }).click();
    await expect(dlg.getByText("Số điện thoại cần 8–15 chữ số, có thể bắt đầu bằng +.")).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/nha-cung-cap-them-${String(vp.width)}.png` });

    await dlg.getByLabel("Tên *").fill("Xưởng Hoa Mới");
    await dlg.getByLabel("Điện thoại").fill("0911 222 333");
    await dlg.getByRole("button", { name: "Lưu" }).click();
    await expect(dlg).toBeHidden();
    await expect(page.getByText("Xưởng Hoa Mới")).toBeVisible();
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[1]).toMatchObject({ method: "POST", body: { name: "Xưởng Hoa Mới", phone: "0911 222 333" } });
    expect(fake.writes[1]?.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  });

  test("trạng thái C: lưu trữ có xác nhận; người khác vừa sửa → hộp Tải lại", async ({ page, api }) => {
    const fake = await supplierApi(api);
    await page.goto("/kho/nha-cung-cap");
    const row = page.getByRole("row").filter({ hasText: "Xưởng may Hoa Sen" });
    await row.getByRole("button", { name: "Lưu trữ" }).click();
    const dlg = page.getByRole("dialog", { name: 'Lưu trữ "Xưởng may Hoa Sen"?' });
    await expect(dlg.getByText(/Phiếu cũ vẫn giữ tên/)).toBeVisible();
    await page.screenshot({ path: `test-results/nha-cung-cap-luu-tru-${String(vp.width)}.png` });
    // Người khác vừa sửa: version trên máy chủ thành 4.
    fake.list = fake.list.map((s) => (s.id === id(3) ? { ...s, version: 4 } : s));
    await dlg.getByRole("button", { name: "Lưu trữ" }).click();
    await expect(page.getByText("Nhà cung cấp đã thay đổi, tải lại.")).toBeVisible();
    await expect(page.getByText("Có người vừa sửa nhà cung cấp này. Thay đổi của bạn chưa được lưu.")).toBeVisible();
    expect(fake.writes.at(-1)).toMatchObject({ method: "PATCH", body: { version: 3, status: "archived" } });
    await page.getByRole("button", { name: "Tải lại" }).click();
    await expect(page.getByText("Nhà cung cấp đã thay đổi, tải lại.")).toBeHidden();
    await row.getByRole("button", { name: "Lưu trữ" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Lưu trữ" }).click();
    await expect(row).toHaveCount(0);
    expect(fake.writes.at(-1)).toMatchObject({ method: "PATCH", body: { version: 4, status: "archived" } });
    await noHorizontalScroll(page);
  });

  test("trạng thái D: chưa có nhà cung cấp nào", async ({ page, api }) => {
    await supplierApi(api, { list: [] });
    await page.goto("/kho/nha-cung-cap");
    await expect(page.getByRole("heading", { name: "Chưa có nhà cung cấp nào" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Thêm nhà cung cấp" })).toHaveCount(2);
    await noHorizontalScroll(page);
  });

  test("chỉ có quyền xem: không có nút ghi", async ({ page, api }) => {
    await supplierApi(api, { manage: false });
    await page.goto("/kho/nha-cung-cap");
    await expect(page.getByText(NAME_200)).toBeVisible();
    await expect(page.getByRole("button", { name: /Thêm nhà cung cấp|Sửa|Lưu trữ/ })).toHaveCount(0);
  });
});

test.describe("360 × 800", () => {
  test.use({ viewport: { width: 360, height: 800 } });
  test("tên 200 ký tự không tràn ở 360", async ({ page, api }) => {
    await supplierApi(api);
    await page.goto("/kho/nha-cung-cap");
    await expect(page.getByText(NAME_200)).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: "test-results/nha-cung-cap-360.png", fullPage: true });
  });
});
