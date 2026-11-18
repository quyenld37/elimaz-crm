// Màn Nhân viên và quyền (kế hoạch M1 Task 54, PQ07; mockup docs/mockups/m1/nhan-vien.html) ở 1512 × 767 và 390 × 844,
// API giả có trạng thái (e2e/fixtures.ts). Luật thật nằm ở API (Task 26, 315); ở đây kiểm màn gửi đúng yêu cầu, hiện
// đúng câu khi máy chủ chặn, ẩn nút trên dòng của chính mình và làm mờ vai trò vượt quyền người sửa.
import type { Grant, ReplaceGrantsRequest, Role, ScopeWarning, StaffMember } from "../src/api";
import { DESKTOP, expect, forEachViewport, json, test, writeRecorder, type FakeApi, type WriteLog } from "./fixtures";

const LAN = "0195f3b2-0000-7000-8000-000000000001"; // người đang đăng nhập (Chủ)
const HUNG = "0195f3b2-0000-7000-8000-000000000002"; // Chủ thứ hai
const HA = "0195f3b2-0000-7000-8000-000000000003"; // Quản lý, có thêm quyền quản lý nhân viên
const MINH = "0195f3b2-0000-7000-8000-000000000004"; // nhiều vai trò
const MAI = "0195f3b2-0000-7000-8000-000000000005"; // đã khóa
const KHO_CHINH = "0195f3b2-0000-7000-8000-00000000a001";

const ALL_PERMS = [
  "catalog.read",
  "inventory.read",
  "inventory.documents.create",
  "inventory.stocktake",
  "inventory.suppliers.manage",
  "order.read",
  "fulfillment.read",
  "fulfillment.ship",
  "realtime.subscribe",
  "identity.users.manage",
  "identity.roles.manage",
  "platform.jobs.manage",
];
const MANAGER_PERMS = ALL_PERMS.filter((p) => !p.startsWith("identity.") && !p.startsWith("platform."));

const ROLES: Role[] = [
  {
    id: "0195f3b2-0000-7000-8000-0000000b0001",
    version: 1,
    holderCount: 0,
    key: "owner",
    name: "Chủ",
    status: "active",
    customized: false,
    permissions: ALL_PERMS,
  },
  {
    id: "0195f3b2-0000-7000-8000-0000000b0002",
    version: 1,
    holderCount: 0,
    key: "manager",
    name: "Quản lý",
    status: "active",
    customized: false,
    permissions: MANAGER_PERMS,
  },
  {
    id: "0195f3b2-0000-7000-8000-0000000b0003",
    version: 1,
    holderCount: 0,
    key: "sale",
    name: "Bán hàng",
    status: "active",
    customized: false,
    permissions: ["catalog.read", "order.read", "inventory.read", "fulfillment.read", "realtime.subscribe"],
  },
  {
    id: "0195f3b2-0000-7000-8000-0000000b0004",
    version: 1,
    holderCount: 0,
    key: "warehouse",
    name: "Kho",
    status: "active",
    customized: false,
    permissions: ["catalog.read", "inventory.read", "inventory.documents.create", "realtime.subscribe"],
  },
  {
    id: "0195f3b2-0000-7000-8000-0000000b0005",
    version: 1,
    holderCount: 0,
    key: "accountant",
    name: "Kế toán",
    status: "active",
    customized: false,
    permissions: ["catalog.read", "inventory.read", "order.read", "fulfillment.read", "realtime.subscribe"],
  },
];

const all = (permission: string): Grant => ({ permission, scope: { type: "all", id: "" } });

function person(
  id: string,
  name: string,
  email: string,
  grants: StaffMember["grants"],
  extra: Partial<StaffMember> = {},
) {
  const m: StaffMember = {
    id,
    email,
    displayName: name,
    status: "active",
    version: 3,
    createdAt: "2026-11-18T08:00:00Z",
    grants,
    ...extra,
  };
  return m;
}

function seed(): StaffMember[] {
  return [
    person(LAN, "Nguyễn Thị Lan", "lan.nguyen@elimaz.com.vn", [{ roleKey: "owner", scopeType: "all", scopeId: "" }]),
    person(HUNG, "Trần Văn Hùng", "hung.tran@elimaz.com.vn", [{ roleKey: "owner", scopeType: "all", scopeId: "" }]),
    person(HA, "Phạm Thu Hà", "ha.pham@elimaz.com.vn", [{ roleKey: "manager", scopeType: "all", scopeId: "" }]),
    person(MINH, "Lê Văn Minh", "minh.le@elimaz.com.vn", [
      { roleKey: "warehouse", scopeType: "warehouse", scopeId: KHO_CHINH },
      { roleKey: "sale", scopeType: "all", scopeId: "" },
    ]),
    person(MAI, "Đỗ Thị Mai", "mai.do@elimaz.com.vn", [{ roleKey: "accountant", scopeType: "all", scopeId: "" }], {
      status: "locked",
    }),
  ];
}

interface Fake extends WriteLog {
  staff: StaffMember[];
}

async function staffApi(api: FakeApi, meId: string, meGrants: Grant[]): Promise<Fake> {
  const fake: Fake = { staff: seed(), writes: [], failNext: null };
  await api.live({ userId: meId, email: "x@elimaz.com.vn", grants: meGrants });
  await api.on("**/api/v1/identity/roles", (r) => json(r, 200, ROLES));
  const { record, fail } = writeRecorder(fake);
  await api.on("**/api/v1/identity/users", async (r) => {
    if (r.request().method() === "GET") return json(r, 200, fake.staff);
    const body = record(r) as { email: string; displayName: string };
    if (await fail(r)) return;
    if (fake.staff.some((m) => m.email === body.email))
      return json(r, 409, { error: "email_taken", request_id: "e2e" });
    const m = person("0195f3b2-0000-7000-8000-0000000000aa", body.displayName, body.email, [], { version: 1 });
    fake.staff.push(m);
    return json(r, 201, m);
  });
  await api.on(/\/api\/v1\/identity\/users\/[^/]+\/(grants|lock|unlock)$/, async (r) => {
    const body = record(r);
    if (await fail(r)) return;
    const [, , , , , id, action] = new URL(r.request().url()).pathname.split("/");
    const m = fake.staff.find((s) => s.id === id);
    if (m === undefined) return json(r, 404, { error: "not_found", request_id: "e2e" });
    if (action === "grants") {
      const req = body as ReplaceGrantsRequest;
      if (req.version !== m.version) return json(r, 409, { error: "version_conflict", request_id: "e2e" });
      m.grants = req.grants;
      m.warnings = req.grants
        .filter((g) => g.roleKey === "manager" && g.scopeType !== "all")
        .map((g): ScopeWarning => ({
          code: "admin_needs_shop_scope",
          message: "Quyền quản trị chỉ hiệu lực khi gán toàn shop",
          roleKey: g.roleKey,
          scopeType: g.scopeType,
          scopeId: g.scopeId,
          permissions: ["inventory.suppliers.manage"],
        }));
    } else {
      m.status = action === "lock" ? "locked" : "active";
    }
    m.version++;
    return json(r, 200, m);
  });
  return fake;
}

const OWNER = ALL_PERMS.map(all);
const MANAGER_WITH_USERS = [...MANAGER_PERMS, "identity.users.manage"].map(all);

async function noHorizontalScroll(page: import("@playwright/test").Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
}

const rowOf = (page: import("@playwright/test").Page, name: string) =>
  page.getByRole("row").filter({ has: page.getByText(name, { exact: true }) });

forEachViewport(() => {
  test("danh sách: chính mình không có nút sửa quyền hay khóa; người khác có; người bị khóa có Mở khóa", async ({
    page,
    api,
  }) => {
    await staffApi(api, LAN, OWNER);
    await page.goto("/nhan-vien");
    await expect(page.getByRole("heading", { name: "Nhân viên và quyền" })).toBeVisible();
    const me = rowOf(page, "Nguyễn Thị Lan");
    await expect(me.getByText("Bạn", { exact: true })).toBeVisible();
    await expect(me.getByText("Không tự sửa quyền hay khóa chính mình")).toBeVisible();
    await expect(me.getByRole("button")).toHaveCount(0);
    await expect(me.getByRole("link")).toHaveCount(0);
    const minh = rowOf(page, "Lê Văn Minh");
    await expect(minh.getByText(`Kho · Kho ${KHO_CHINH}`)).toBeVisible();
    await expect(minh.getByText("Bán hàng · Toàn shop")).toBeVisible();
    await expect(minh.getByRole("link", { name: "Sửa quyền" })).toBeVisible();
    await expect(minh.getByRole("button", { name: "Khóa" })).toBeVisible();
    const mai = rowOf(page, "Đỗ Thị Mai");
    await expect(mai.getByText("Đã khóa")).toBeVisible();
    await expect(mai.getByRole("button", { name: "Mở khóa" })).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("thêm nhân viên rồi gán Kho phạm vi kho chính → danh sách cập nhật", async ({ page, api }) => {
    const fake = await staffApi(api, LAN, OWNER);
    await page.goto("/nhan-vien");
    await page.getByRole("button", { name: "Thêm nhân viên" }).click();
    const dlg = page.getByRole("dialog", { name: "Thêm nhân viên" });
    // Email đã có → câu dưới ô email, hộp thoại vẫn mở.
    await dlg.getByLabel("Email đăng nhập *").fill("minh.le@elimaz.com.vn");
    await dlg.getByLabel("Tên hiển thị *").fill("Lê Văn Minh");
    await dlg.getByRole("button", { name: "Thêm", exact: true }).click();
    await expect(dlg.getByText("Email này đã có trong hệ thống")).toBeVisible();
    await noHorizontalScroll(page);
    await dlg.getByLabel("Email đăng nhập *").fill("ngoc.vu@elimaz.com.vn");
    await dlg.getByLabel("Tên hiển thị *").fill("Vũ Bảo Ngọc");
    await dlg.getByRole("button", { name: "Thêm", exact: true }).click();
    await expect(dlg).toBeHidden();
    const ngoc = rowOf(page, "Vũ Bảo Ngọc");
    await expect(ngoc.getByText("Chưa có quyền nào")).toBeVisible();

    await ngoc.getByRole("link", { name: "Gán vai trò" }).click();
    await expect(page.getByRole("heading", { name: "Quyền của Vũ Bảo Ngọc" })).toBeVisible();
    await page.getByRole("radio", { name: /^Kho/ }).check();
    await page.getByLabel("Phạm vi của vai trò Kho").selectOption({ label: `Kho ${KHO_CHINH}` });
    await page.getByRole("button", { name: "Thêm", exact: true }).click();
    await expect(page.getByRole("button", { name: "Gỡ vai trò Kho" })).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Lưu quyền" }).click();

    await expect(page.getByRole("heading", { name: "Nhân viên và quyền" })).toBeVisible();
    await expect(rowOf(page, "Vũ Bảo Ngọc").getByText(`Kho · Kho ${KHO_CHINH}`)).toBeVisible();
    const put = fake.writes.find((w) => w.method === "PUT");
    expect(put?.body).toEqual({
      version: 1,
      grants: [{ roleKey: "warehouse", scopeType: "warehouse", scopeId: KHO_CHINH }],
    });
    expect(put?.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(fake.writes.filter((w) => w.method === "POST").every((w) => w.key !== null)).toBe(true);
  });

  test("Quản lý sửa quyền: vai trò Chủ mờ kèm lý do; máy chủ chặn → câu không cấp quyền mình không có", async ({
    page,
    api,
  }) => {
    const fake = await staffApi(api, HA, MANAGER_WITH_USERS);
    await page.goto(`/nhan-vien/${MINH}/quyen`);
    await expect(page.getByRole("heading", { name: "Quyền của Lê Văn Minh" })).toBeVisible();
    await expect(page.getByText("Người sửa: Phạm Thu Hà · Quản lý")).toBeVisible();
    const ownerOpt = page.getByRole("radio", { name: /^Chủ/ });
    await expect(ownerOpt).toBeDisabled();
    await expect(page.getByText("Bạn không thể cấp quyền mà chính bạn không có", { exact: false })).toBeVisible();
    await expect(page.getByRole("radio", { name: /^Kế toán/ })).toBeEnabled();
    await noHorizontalScroll(page);

    // Máy chủ vẫn là nơi quyết: nó chặn thì màn hiện câu của mockup.
    fake.failNext = { status: 403, code: "grant_exceeds_actor" };
    await page.getByRole("button", { name: "Gỡ vai trò Bán hàng" }).click();
    await page.getByRole("button", { name: "Lưu quyền" }).click();
    await expect(page.getByText("Bạn không thể cấp quyền mà chính bạn không có.")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Quyền của Lê Văn Minh" })).toBeVisible();
  });

  test("khóa Chủ cuối cùng bị chặn → Phải còn ít nhất một Chủ đang hoạt động", async ({ page, api }) => {
    const fake = await staffApi(api, LAN, OWNER);
    await page.goto("/nhan-vien");
    await rowOf(page, "Trần Văn Hùng").getByRole("button", { name: "Khóa" }).click();
    const dlg = page.getByRole("dialog", { name: "Khóa Trần Văn Hùng?" });
    await expect(dlg.getByText("không dùng được CRM từ thao tác kế tiếp")).toBeVisible();
    fake.failNext = { status: 409, code: "last_owner" };
    await dlg.getByRole("button", { name: "Khóa" }).click();
    await expect(page.getByText("Phải còn ít nhất một Chủ đang hoạt động.")).toBeVisible();
    await expect(rowOf(page, "Trần Văn Hùng").getByText("Đang hoạt động")).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("khóa rồi mở khóa: trạng thái đổi theo câu trả lời của máy chủ", async ({ page, api }) => {
    const fake = await staffApi(api, LAN, OWNER);
    await page.goto("/nhan-vien");
    await rowOf(page, "Phạm Thu Hà").getByRole("button", { name: "Khóa" }).click();
    await page.getByRole("dialog", { name: "Khóa Phạm Thu Hà?" }).getByRole("button", { name: "Khóa" }).click();
    await expect(rowOf(page, "Phạm Thu Hà").getByText("Đã khóa")).toBeVisible();
    await rowOf(page, "Đỗ Thị Mai").getByRole("button", { name: "Mở khóa" }).click();
    await expect(rowOf(page, "Đỗ Thị Mai").getByText("Đang hoạt động")).toBeVisible();
    expect(fake.writes.map((w) => `${w.method} ${w.path}`)).toEqual([
      `POST /api/v1/identity/users/${HA}/lock`,
      `POST /api/v1/identity/users/${MAI}/unlock`,
    ]);
  });

  test("gán Quản lý theo kho → cảnh báo quyền quản trị chỉ hiệu lực khi gán toàn shop (Task 315)", async ({
    page,
    api,
  }) => {
    await staffApi(api, LAN, OWNER);
    await page.goto(`/nhan-vien/${MINH}/quyen`);
    await page.getByRole("radio", { name: /^Quản lý/ }).check();
    await page.getByLabel("Phạm vi của vai trò Quản lý").selectOption({ label: `Kho ${KHO_CHINH}` });
    await page.getByRole("button", { name: "Thêm", exact: true }).click();
    await page.getByRole("button", { name: "Lưu quyền" }).click();
    const minh = rowOf(page, "Lê Văn Minh");
    await expect(
      minh.getByText(
        `Quản lý · Kho ${KHO_CHINH}: quyền quản trị chỉ hiệu lực khi gán toàn shop, các quyền đó đang không dùng được`,
      ),
    ).toBeVisible();
    await noHorizontalScroll(page);
    // Trang sửa quyền cũng hiện cảnh báo ngay dưới dòng vai trò đó.
    await minh.getByRole("link", { name: "Sửa quyền" }).click();
    await expect(page.getByText("quyền quản trị chỉ hiệu lực khi gán toàn shop", { exact: false })).toBeVisible();
  });

  test("người khác vừa sửa → câu tải lại; bấm Tải lại lấy bản mới", async ({ page, api }) => {
    const fake = await staffApi(api, LAN, OWNER);
    await page.goto(`/nhan-vien/${MINH}/quyen`);
    await expect(page.getByRole("heading", { name: "Quyền của Lê Văn Minh" })).toBeVisible();
    const minh = fake.staff.find((m) => m.id === MINH);
    if (minh === undefined) throw new Error("thiếu Minh");
    minh.version = 9;
    minh.grants = [{ roleKey: "accountant", scopeType: "all", scopeId: "" }];
    await page.getByRole("button", { name: "Gỡ vai trò Bán hàng" }).click();
    await page.getByRole("button", { name: "Lưu quyền" }).click();
    await expect(page.getByText("Quyền của người này vừa được người khác sửa, tải lại.")).toBeVisible();
    await page.getByRole("button", { name: "Tải lại" }).click();
    await expect(page.getByRole("button", { name: "Gỡ vai trò Kế toán" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Gỡ vai trò Bán hàng" })).toHaveCount(0);
  });

  test("mở trang sửa quyền của chính mình → câu không tự sửa, không có nút lưu", async ({ page, api }) => {
    await staffApi(api, LAN, OWNER);
    await page.goto(`/nhan-vien/${LAN}/quyen`);
    await expect(page.getByText("Bạn không thể sửa quyền hay khóa chính mình.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Lưu quyền" })).toHaveCount(0);
  });
});

test("menu: mục Nhân viên chỉ hiện với người có quyền quản lý nhân viên", async ({ page, api }) => {
  await page.setViewportSize(DESKTOP);
  await staffApi(api, MINH, ["catalog.read", "inventory.read"].map(all));
  // "/" mở màn được phép đầu tiên (Danh sách sản phẩm, Task 50): khai các đường của nó.
  await api.catalog();
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Nhân viên" })).toHaveCount(0);
  // Mở thẳng /nhan-vien: không có trang này với người thiếu quyền.
  await page.goto("/nhan-vien");
  await expect(page.getByText("Không có trang này, hoặc bạn chưa được cấp quyền xem")).toBeVisible();
  await expect(page.getByRole("link", { name: "Nhân viên" })).toHaveCount(0);
});
