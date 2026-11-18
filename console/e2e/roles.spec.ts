// Màn Vai trò (kế hoạch M1 Task M1-56, Q35; mockup docs/mockups/m1/vai-tro.html) ở 1512 × 767 và 390 × 844, API giả có
// trạng thái (e2e/fixtures.ts). Luật thật nằm ở API (identity.CreateRole, UpdateRole, ArchiveRole); ở đây kiểm màn gửi
// đúng yêu cầu, làm mờ quyền người sửa không có kèm lý do, vai trò Chủ chỉ xem, câu báo khi máy chủ chặn, và vai trò
// mới hiện ở màn Nhân viên.
import type { Page } from "@playwright/test";
import type { Grant, PermissionInfo, Role, StaffMember } from "../src/api";
import { expect, forEachViewport, json, test, writeRecorder, type FakeApi, type WriteLog } from "./fixtures";

const ME = "0195f3b2-0000-7000-8000-000000000001";
const MINH = "0195f3b2-0000-7000-8000-000000000004";

const CATALOG: PermissionInfo[] = [
  { key: "catalog.read", scopeType: "none", group: "Sản phẩm", label: "Xem sản phẩm" },
  { key: "catalog.manage", scopeType: "none", group: "Sản phẩm", label: "Sửa sản phẩm, ảnh, phân loại" },
  { key: "inventory.read", scopeType: "warehouse", group: "Kho", label: "Xem tồn kho và phiếu" },
  { key: "inventory.stocktake", scopeType: "warehouse", group: "Kho", label: "Đếm kiểm kho" },
  { key: "conversation.read", scopeType: "page", group: "Hội thoại", label: "Xem hội thoại" },
  { key: "files.upload", scopeType: "none", group: "Tệp", label: "Tải tệp lên" },
  { key: "platform.files.manage", scopeType: "shop", group: "Tệp", label: "Kiểm tệp, tạo lại cỡ ảnh" },
  { key: "realtime.subscribe", scopeType: "none", group: "Hệ thống", label: "Nhận cập nhật tức thì" },
  { key: "identity.users.manage", scopeType: "shop", group: "Quản trị", label: "Quản lý nhân viên" },
  { key: "identity.roles.manage", scopeType: "shop", group: "Quản trị", label: "Quản lý vai trò" },
];
const ALL = CATALOG.map((p) => p.key);
const LONG_NAME = "Nhân viên kho thời vụ mùa Tết chi nhánh Thủ Đức ca tối cuối tuần";

const rid = (n: number) => `0195f3b2-0000-7000-8000-0000000c00${String(n).padStart(2, "0")}`;
const role = (n: number, key: string, name: string, permissions: string[], extra: Partial<Role> = {}): Role => ({
  id: rid(n),
  key,
  name,
  status: "active",
  customized: false,
  version: 1,
  holderCount: 0,
  permissions,
  ...extra,
});

function seedRoles(): Role[] {
  return [
    role(1, "owner", "Chủ", ALL, { holderCount: 2 }),
    role(2, "manager", "Quản lý", ["catalog.read", "catalog.manage", "inventory.read", "inventory.stocktake"], {
      holderCount: 1,
    }),
    role(3, "warehouse", "Kho", ["catalog.read", "inventory.read"], { customized: true, holderCount: 2 }),
    role(4, "custom_ab12cd34", "Kho phụ", ["catalog.read", "inventory.read", "inventory.stocktake"], {
      customized: true,
      holderCount: 1,
    }),
    role(5, "custom_tet00001", LONG_NAME, ["catalog.read"], { customized: true }),
    role(6, "custom_old00001", "Thời vụ Tết", ["catalog.read", "files.upload"], {
      customized: true,
      status: "archived",
    }),
  ];
}

const staffMember = (id: string, name: string, roleKey: string): StaffMember => ({
  id,
  email: `${id.slice(-4)}@elimaz.vn`,
  displayName: name,
  status: "active",
  version: 1,
  createdAt: "2026-11-18T08:00:00Z",
  grants: [{ roleKey, scopeType: "all", scopeId: "" }],
});

interface Fake extends WriteLog {
  roles: Role[];
}

// Người sửa mặc định: Quản lý được cấp thêm quyền quản lý vai trò và nhân viên (không có platform.files.manage).
const MANAGER_GRANTS: Grant[] = [
  "catalog.read",
  "catalog.manage",
  "inventory.read",
  "inventory.stocktake",
  "files.upload",
  "realtime.subscribe",
  "identity.roles.manage",
  "identity.users.manage",
].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

async function rolesApi(api: FakeApi, grants: Grant[] = MANAGER_GRANTS): Promise<Fake> {
  const fake: Fake = { roles: seedRoles(), writes: [], failNext: null };
  await api.live({ userId: ME, grants });
  const { record, fail } = writeRecorder(fake);
  await api.on("**/api/v1/identity/permissions", (r) => json(r, 200, CATALOG));
  await api.on("**/api/v1/identity/users", (r) =>
    json(r, 200, [staffMember(ME, "Phạm Thu Hà", "manager"), staffMember(MINH, "Lê Văn Minh", "custom_ab12cd34")]),
  );
  await api.on("**/api/v1/identity/roles", async (r) => {
    if (r.request().method() === "GET") return json(r, 200, fake.roles);
    const body = record(r) as { name: string; permissions: string[] };
    if (await fail(r)) return;
    const created = role(fake.roles.length + 10, "custom_new00001", body.name, body.permissions, { customized: true });
    fake.roles.push(created);
    return json(r, 201, created);
  });
  await api.on(/\/api\/v1\/identity\/roles\/[^/]+(\/archive)?$/, async (r) => {
    const body = record(r) as { version: number; name?: string; permissions?: string[] };
    if (await fail(r)) return;
    const parts = new URL(r.request().url()).pathname.split("/");
    const target = fake.roles.find((x) => x.id === parts[5]);
    if (target === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    if (body.version !== target.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
    if (parts[6] === "archive") target.status = "archived";
    else {
      target.name = body.name ?? target.name;
      target.permissions = body.permissions ?? target.permissions;
      target.customized = true;
    }
    target.version++;
    return json(r, 200, target);
  });
  return fake;
}

async function noHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
}

const permBox = (page: Page, key: string) =>
  page
    .locator("label.perm")
    .filter({ has: page.locator("code", { hasText: new RegExp(`^${key.replace(".", "\\.")}$`) }) });

forEachViewport(() => {
  test("danh sách: loại, số quyền, người giữ, lưu trữ chỉ xem; tên dài không tràn", async ({ page, api }) => {
    await rolesApi(api);
    await page.goto("/vai-tro");
    await expect(page.getByRole("heading", { name: "Vai trò", level: 1 })).toBeVisible();
    const row = (name: string) => page.locator("tbody tr").filter({ hasText: name });
    await expect(row("Chủ")).toContainText("Mẫu · luôn đủ mọi quyền");
    await expect(row("Chủ").getByRole("link", { name: "Xem Chủ" })).toBeVisible();
    await expect(row("Kho phụ")).toContainText("Tự tạo");
    await expect(row("Kho phụ").getByRole("link", { name: "Sửa Kho phụ" })).toBeVisible();
    await expect(page.locator("tbody tr").filter({ hasText: /^Kho/ }).first()).toContainText("Mẫu đã sửa");
    await expect(row("Thời vụ Tết")).toContainText("Đã lưu trữ");
    await expect(row("Thời vụ Tết").getByRole("link", { name: "Xem Thời vụ Tết" })).toBeVisible();
    await expect(row(LONG_NAME)).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("vai trò Chủ chỉ xem: mọi quyền đánh dấu, không sửa được", async ({ page, api }) => {
    const fake = await rolesApi(api);
    await page.goto(`/vai-tro/${rid(1)}`);
    await expect(page.getByText("Chỉ xem")).toBeVisible();
    await expect(page.getByText("Vai trò Chủ luôn có mọi quyền")).toBeVisible();
    const boxes = page.locator("label.perm input");
    await expect(boxes).toHaveCount(CATALOG.length);
    for (const b of await boxes.all()) {
      await expect(b).toBeChecked();
      await expect(b).toBeDisabled();
    }
    await expect(page.getByRole("button", { name: "Lưu" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Lưu trữ vai trò" })).toHaveCount(0);
    expect(fake.writes).toEqual([]);
    await noHorizontalScroll(page);
  });

  test("tạo vai trò: quyền người sửa không có hiện mờ kèm lý do; gửi đúng tên, quyền, khóa chống trùng", async ({
    page,
    api,
  }) => {
    const fake = await rolesApi(api);
    await page.goto("/vai-tro");
    await page.getByRole("link", { name: "Tạo vai trò" }).click();
    await expect(page.getByRole("heading", { name: "Tạo vai trò" })).toBeVisible();
    const files = permBox(page, "platform.files.manage");
    await expect(files.locator("input")).toBeDisabled();
    await expect(files).toContainText("Bạn không có quyền này nên không cấp được");
    await expect(permBox(page, "inventory.read")).toContainText("theo kho");
    await expect(permBox(page, "conversation.read").locator("input")).toBeDisabled();
    await page.getByLabel("Tên vai trò *").fill("Kho đêm");
    await permBox(page, "catalog.read").locator("input").check();
    await permBox(page, "inventory.read").locator("input").check();
    await expect(page.getByText("Quyền (2 đã chọn)")).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page).toHaveURL(/\/vai-tro$/);
    await expect(page.locator("tbody tr").filter({ hasText: "Kho đêm" })).toContainText("Tự tạo");
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]).toMatchObject({
      method: "POST",
      path: "/api/v1/identity/roles",
      body: { name: "Kho đêm", permissions: ["catalog.read", "inventory.read"] },
    });
    expect(fake.writes[0]?.key).toMatch(/^[A-Za-z0-9_-]{8,}$/);
  });

  test("sửa vai trò: gửi version; trùng tên và bị sửa cùng lúc hiện câu báo", async ({ page, api }) => {
    const fake = await rolesApi(api);
    await page.goto(`/vai-tro/${rid(4)}`);
    await expect(page.getByText("Tự tạo · 1 người đang giữ")).toBeVisible();
    await permBox(page, "inventory.stocktake").locator("input").uncheck();
    fake.failNext = { status: 409, code: "role_name_taken" };
    await page.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("Đã có vai trò cùng tên. Đặt tên khác.");
    fake.failNext = { status: 409, code: "version_conflict" };
    await page.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("Vai trò vừa được người khác sửa, tải lại.");
    await page.getByRole("button", { name: "Tải lại" }).click();
    await expect(permBox(page, "inventory.stocktake").locator("input")).toBeChecked();
    await permBox(page, "inventory.stocktake").locator("input").uncheck();
    await page.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page).toHaveURL(/\/vai-tro$/);
    const last = fake.writes.at(-1);
    expect(last).toMatchObject({
      method: "PUT",
      path: `/api/v1/identity/roles/${rid(4)}`,
      body: { version: 1, name: "Kho phụ", permissions: ["catalog.read", "inventory.read"] },
    });
  });

  test("lưu trữ: còn người giữ thì chặn kèm tên; không ai giữ thì hỏi lại rồi gửi", async ({ page, api }) => {
    const fake = await rolesApi(api);
    await page.goto(`/vai-tro/${rid(4)}`);
    await page.getByRole("button", { name: "Lưu trữ vai trò" }).click();
    const box = page.getByRole("alert");
    await expect(box).toContainText('Không lưu trữ được: còn 1 người đang giữ vai trò "Kho phụ".');
    await expect(box).toContainText("Lê Văn Minh");
    await expect(box.getByRole("link", { name: "Mở màn Nhân viên" })).toBeVisible();
    expect(fake.writes).toEqual([]);

    await page.goto(`/vai-tro/${rid(5)}`);
    await page.getByRole("button", { name: "Lưu trữ vai trò" }).click();
    const dlg = page.getByRole("dialog");
    await expect(dlg).toContainText(`Lưu trữ vai trò "${LONG_NAME}"?`);
    await noHorizontalScroll(page);
    await dlg.getByRole("button", { name: "Lưu trữ" }).click();
    await expect(page).toHaveURL(/\/vai-tro$/);
    expect(fake.writes).toEqual([
      expect.objectContaining({
        method: "POST",
        path: `/api/v1/identity/roles/${rid(5)}/archive`,
        body: { version: 1 },
      }),
    ]);
    await expect(page.locator("tbody tr").filter({ hasText: LONG_NAME })).toContainText("Đã lưu trữ");
  });

  test("vai trò tự tạo hiện ở màn Nhân viên; vai trò đã lưu trữ không có trong danh sách gán", async ({
    page,
    api,
  }) => {
    await rolesApi(api);
    await page.goto(`/nhan-vien/${MINH}/quyen`);
    const options = page.getByRole("radiogroup", { name: "Vai trò sẽ thêm" });
    await expect(options).toContainText("Kho phụ (tự tạo)");
    await expect(options).not.toContainText("Thời vụ Tết");
  });

  test("menu: có mục Vai trò khi có quyền quản lý vai trò", async ({ page, api }) => {
    await rolesApi(api);
    await page.goto("/vai-tro");
    await expect(page.getByRole("heading", { name: "Vai trò", level: 1 })).toBeVisible();
  });
});

test("không có quyền quản lý vai trò: không có màn Vai trò", async ({ page, api }) => {
  await rolesApi(
    api,
    MANAGER_GRANTS.filter((g) => g.permission !== "identity.roles.manage"),
  );
  await page.goto("/vai-tro");
  await expect(page.getByText("Không có trang này, hoặc bạn chưa được cấp quyền xem")).toBeVisible();
});
