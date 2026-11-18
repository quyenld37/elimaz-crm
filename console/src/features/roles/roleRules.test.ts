// Luật hiển thị của màn Vai trò (kế hoạch M1 Task M1-56): loại vai trò, quyền người sửa thêm/gỡ được, nhóm quyền,
// danh sách gửi lên. API vẫn là nơi quyết (identity.CreateRole, UpdateRole); các hàm này chỉ làm mờ.
import { describe, expect, it } from "vitest";
import type { Grant, PermissionInfo, Role } from "../../api";
import { blockedChanges, canChange, groupPermissions, isReadOnly, roleKind, scopeChip, toSubmit } from "./roleRules";

const role = (key: string, extra: Partial<Role> = {}): Role => ({
  id: "0195f3b2-0000-7000-8000-000000000001",
  key,
  name: key,
  status: "active",
  customized: false,
  version: 1,
  holderCount: 0,
  permissions: [],
  ...extra,
});
const perm = (key: string, scopeType: PermissionInfo["scopeType"], group = "Kho"): PermissionInfo => ({
  key,
  scopeType,
  group,
  label: key,
});
const g = (permission: string, type: Grant["scope"]["type"] = "all", id = ""): Grant => ({
  permission,
  scope: { type, id },
});

describe("roleKind, isReadOnly", () => {
  it("phân loại vai trò như mockup trạng thái A", () => {
    expect(roleKind(role("owner"))).toBe("Mẫu · luôn đủ mọi quyền");
    expect(roleKind(role("sale"))).toBe("Mẫu");
    expect(roleKind(role("warehouse", { customized: true }))).toBe("Mẫu đã sửa");
    expect(roleKind(role("custom_ab12cd34", { customized: true }))).toBe("Tự tạo");
  });
  it("Chủ và vai trò đã lưu trữ chỉ xem", () => {
    expect(isReadOnly(role("owner"))).toBe(true);
    expect(isReadOnly(role("custom_x", { status: "archived" }))).toBe(true);
    expect(isReadOnly(role("sale"))).toBe(false);
  });
});

describe("canChange", () => {
  const files = perm("platform.files.manage", "shop", "Tệp");
  const read = perm("catalog.read", "none", "Sản phẩm");
  const post = perm("inventory.documents.post", "warehouse");
  it("quyền quản trị và quyền theo kho cần có ở toàn shop", () => {
    expect(canChange(files, [g("platform.files.manage", "warehouse", "w1")])).toBe(false);
    expect(canChange(files, [g("platform.files.manage")])).toBe(true);
    expect(canChange(post, [g("inventory.documents.post", "warehouse", "w1")])).toBe(false);
    expect(canChange(post, [g("inventory.documents.post")])).toBe(true);
  });
  it("quyền loại none: gán nào cũng được", () => {
    expect(canChange(read, [g("catalog.read", "warehouse", "w1")])).toBe(true);
    expect(canChange(read, [])).toBe(false);
  });
});

describe("nhóm, chip, danh sách gửi", () => {
  const cat = [
    perm("catalog.read", "none", "Sản phẩm"),
    perm("catalog.manage", "none", "Sản phẩm"),
    perm("inventory.read", "warehouse", "Kho"),
    perm("conversation.read", "page", "Hội thoại"),
  ];
  it("nhóm liền theo thứ tự API", () => {
    expect(groupPermissions(cat).map((x) => [x.group, x.items.length])).toEqual([
      ["Sản phẩm", 2],
      ["Kho", 1],
      ["Hội thoại", 1],
    ]);
  });
  it("chip phạm vi", () => {
    expect(cat.map(scopeChip)).toEqual([null, null, "theo kho", "theo Page"]);
  });
  it("bỏ quyền lạ, giữ thứ tự danh mục", () => {
    expect(toSubmit(new Set(["inventory.read", "foo.bar", "catalog.read"]), cat)).toEqual([
      "catalog.read",
      "inventory.read",
    ]);
  });
  it("quyền người sửa không có mà bị thêm hay gỡ", () => {
    const actor = [g("catalog.read")];
    const blocked = blockedChanges(["catalog.manage"], new Set(["catalog.read", "inventory.read"]), cat, actor);
    expect(blocked.map((p) => p.key)).toEqual(["catalog.manage", "inventory.read"]);
  });
});
