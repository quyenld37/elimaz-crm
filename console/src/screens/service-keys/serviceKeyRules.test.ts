// Luật thuần của màn Khóa dịch vụ (Task 208).
import { describe, expect, it } from "vitest";
import { KEY_PERMISSIONS, parseCidrs, permChips, validateCreate } from "./serviceKeyRules";
import { ALL_WEB } from "./serviceKeysTestkit";

describe("quyền của khóa", () => {
  it("đủ chín quyền web.* của identity.ServiceKeyPermissions (docs/quyen.md), đúng thứ tự", () => {
    expect(KEY_PERMISSIONS.map((p) => p.key)).toEqual(ALL_WEB);
    expect(KEY_PERMISSIONS.filter((p) => !p.byDefault).map((p) => p.key)).toEqual(["web.flash.buy"]);
  });

  it("chip: ít quyền → tên từng quyền; nhiều → số quyền; quyền lạ hiện nguyên mã", () => {
    expect(permChips(["web.catalog.read", "web.orders.create"])).toEqual(["Đọc danh mục", "Tạo đơn web"]);
    expect(permChips(ALL_WEB)).toEqual(["9 quyền website"]);
    expect(permChips(["web.catalog.read", "web.x.y"])).toEqual(["Đọc danh mục", "web.x.y"]);
  });
});

describe("parseCidrs", () => {
  it("bỏ dòng trống, cắt khoảng trắng; rỗng → []", () => {
    expect(parseCidrs("")).toEqual({ cidrs: [] });
    expect(parseCidrs(" 13.140.64.56/32 \n\n10.0.0.0/8\n")).toEqual({ cidrs: ["13.140.64.56/32", "10.0.0.0/8"] });
    expect(parseCidrs("2001:db8::/32")).toEqual({ cidrs: ["2001:db8::/32"] });
  });

  it("thiếu /, octet > 255, độ dài dải quá → lỗi nêu dòng", () => {
    expect(parseCidrs("13.140.64.56")).toEqual({ error: "Dòng 1 chưa đúng dạng địa chỉ/dải, ví dụ 13.140.64.56/32" });
    expect(parseCidrs("10.0.0.0/8\n300.1.1.1/32")).toEqual({
      error: "Dòng 2 chưa đúng dạng địa chỉ/dải, ví dụ 13.140.64.56/32",
    });
    expect("error" in parseCidrs("10.0.0.0/33")).toBe(true);
    expect("error" in parseCidrs("2001:db8::/129")).toBe(true);
    expect("error" in parseCidrs("abc/24")).toBe(true);
  });
});

describe("validateCreate", () => {
  it("tên 1–80 sau khi cắt; ít nhất một quyền; IP đúng dạng", () => {
    expect(validateCreate({ name: "  ", perms: ["web.catalog.read"], ips: "" }).name).toBe("Tên 1–80 ký tự");
    expect(validateCreate({ name: "x".repeat(81), perms: ["web.catalog.read"], ips: "" }).name).toBe("Tên 1–80 ký tự");
    expect(validateCreate({ name: "x".repeat(80), perms: ["web.catalog.read"], ips: "" })).toEqual({});
    expect(validateCreate({ name: "a", perms: [], ips: "" }).perms).toBe("Chọn ít nhất một quyền");
    expect(validateCreate({ name: "a", perms: ["web.catalog.read"], ips: "x" }).ips).toContain("Dòng 1");
  });
});
