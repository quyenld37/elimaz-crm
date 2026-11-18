// Luật hiển thị của màn Nhân viên (kế hoạch M1 Task 54): nhãn vai trò · phạm vi, lựa chọn phạm vi, vai trò người sửa
// gán được ở phạm vi nào. API vẫn là nơi quyết (Task 26, 315); các hàm này chỉ làm mờ lựa chọn chắc chắn bị từ chối.
import { describe, expect, it } from "vitest";
import type { Grant, Role, StaffMember } from "../../api";
import {
  ALL_SCOPE,
  canGrant,
  canGrantSomewhere,
  grantLabel,
  roleHint,
  sameGrant,
  scopeKey,
  scopeLabel,
  scopeOptions,
  warningText,
} from "./staffRules";

const role = (key: string, name: string, permissions: string[], extra: Partial<Role> = {}): Role => ({
  key,
  name,
  status: "active",
  customized: false,
  permissions,
  ...extra,
});
const g = (permission: string, type: Grant["scope"]["type"] = "all", id = ""): Grant => ({
  permission,
  scope: { type, id },
});
const W1 = { type: "warehouse" as const, id: "w1" };
const W2 = { type: "warehouse" as const, id: "w2" };

const member = (grants: StaffMember["grants"]): StaffMember => ({
  id: "u1",
  email: "a@b.vn",
  displayName: "A",
  status: "active",
  version: 1,
  createdAt: "2026-11-18T00:00:00Z",
  grants,
});

describe("scopeLabel, grantLabel", () => {
  it("toàn shop, kho, Page", () => {
    expect(scopeLabel(ALL_SCOPE)).toBe("Toàn shop");
    expect(scopeLabel(W1)).toBe("Kho w1");
    expect(scopeLabel({ type: "page", id: "p9" })).toBe("Page p9");
  });
  it("tên vai trò theo danh sách vai trò; vai trò lạ hiện khóa", () => {
    const roles = [role("warehouse", "Kho", [])];
    expect(grantLabel({ roleKey: "warehouse", scopeType: "warehouse", scopeId: "w1" }, roles)).toBe("Kho · Kho w1");
    expect(grantLabel({ roleKey: "gone", scopeType: "all", scopeId: "" }, roles)).toBe("gone · Toàn shop");
  });
  it("vai trò tự tạo có chữ (tự tạo)", () => {
    const roles = [role("custom_ab12cd34", "Kho phụ", ["catalog.read"], { customized: true })];
    expect(grantLabel({ roleKey: "custom_ab12cd34", scopeType: "all", scopeId: "" }, roles)).toBe(
      "Kho phụ (tự tạo) · Toàn shop",
    );
  });
});

describe("scopeOptions", () => {
  it("Toàn shop đứng đầu, sau là mọi kho, Page đã có trong grant của bất kỳ ai, không trùng", () => {
    const staff = [
      member([{ roleKey: "warehouse", scopeType: "warehouse", scopeId: "w2" }]),
      member([
        { roleKey: "warehouse", scopeType: "warehouse", scopeId: "w1" },
        { roleKey: "sale", scopeType: "warehouse", scopeId: "w2" },
        { roleKey: "sale", scopeType: "all", scopeId: "" },
      ]),
    ];
    expect(scopeOptions(staff).map(scopeKey)).toEqual(["all:", "warehouse:w1", "warehouse:w2"]);
  });
});

describe("canGrant: mọi quyền của vai trò người sửa phải có ở toàn shop hoặc đúng phạm vi đó", () => {
  const warehouse = role("warehouse", "Kho", ["catalog.read", "inventory.read"]);
  const owner = role("owner", "Chủ", [
    "catalog.read",
    "inventory.read",
    "identity.users.manage",
    "platform.jobs.manage",
  ]);
  const manager = [g("identity.users.manage"), g("catalog.read"), g("inventory.read")];

  it("có đủ ở toàn shop → gán được ở mọi phạm vi", () => {
    expect(canGrant(warehouse, ALL_SCOPE, manager)).toBe(true);
    expect(canGrant(warehouse, W1, manager)).toBe(true);
  });
  it("thiếu một quyền → không gán được ở đâu", () => {
    expect(canGrant(owner, ALL_SCOPE, manager)).toBe(false);
    expect(canGrantSomewhere(owner, [ALL_SCOPE, W1], manager)).toBe(false);
  });
  it("có quyền ở kho w1 → chỉ gán được ở w1", () => {
    const atW1 = [g("catalog.read", "warehouse", "w1"), g("inventory.read", "warehouse", "w1")];
    expect(canGrant(warehouse, W1, atW1)).toBe(true);
    expect(canGrant(warehouse, W2, atW1)).toBe(false);
    expect(canGrant(warehouse, ALL_SCOPE, atW1)).toBe(false);
    expect(canGrantSomewhere(warehouse, [ALL_SCOPE, W1, W2], atW1)).toBe(true);
  });
  it("vai trò không có quyền nào luôn gán được", () => {
    expect(canGrant(role("empty", "Trống", []), ALL_SCOPE, [])).toBe(true);
  });
});

describe("sameGrant", () => {
  it("so vai trò và phạm vi", () => {
    const a = { roleKey: "sale", scopeType: "all" as const, scopeId: "" };
    expect(sameGrant(a, { ...a })).toBe(true);
    expect(sameGrant(a, { ...a, scopeType: "warehouse", scopeId: "w1" })).toBe(false);
  });
});

describe("roleHint", () => {
  it("vai trò mẫu có câu mô tả; vai trò tự tạo ghi số quyền", () => {
    expect(roleHint(role("manager", "Quản lý", ["a"]))).toBe(
      "Mọi quyền trừ quản lý nhân viên, vai trò và quyền hệ thống",
    );
    expect(roleHint(role("custom_x", "Kho phụ", ["a", "b", "c"], { customized: true }))).toBe("Tự tạo · 3 quyền");
  });
});

describe("warningText: cảnh báo Task 315", () => {
  it("câu cố định theo mã kèm vai trò · phạm vi; mã lạ dùng câu chung", () => {
    const roles = [role("manager", "Quản lý", [])];
    expect(
      warningText(
        {
          code: "admin_needs_shop_scope",
          message: "bỏ qua",
          roleKey: "manager",
          scopeType: "warehouse",
          scopeId: "w1",
          permissions: ["inventory.suppliers.manage"],
        },
        roles,
      ),
    ).toBe("Quản lý · Kho w1: quyền quản trị chỉ hiệu lực khi gán toàn shop, các quyền đó đang không dùng được");
  });
});
