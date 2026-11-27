// Route của danh sách vận hành (kế hoạch M3 Task 135): đường dẫn, quyền, nhóm menu "Vận chuyển" và số đếm cạnh mục.
import { describe, expect, it } from "vitest";
import { screens } from "./route";

describe("route danh sách vận hành", () => {
  it("bốn đường của kế hoạch, nhóm Vận chuyển, quyền theo danh sách; ba mục đầu có số đếm, mục Giữ hàng quá hạn không", () => {
    expect(screens.map((s) => [s.path, s.group, s.permission, s.badge !== undefined])).toEqual([
      ["/ops/callbacks", "Vận chuyển", "fulfillment.callbacks.work", true],
      ["/ops/stuck", "Vận chuyển", "fulfillment.read", true],
      ["/ops/review", "Vận chuyển", "fulfillment.read", true],
      ["/ops/aged-holds", "Vận chuyển", "inventory.aged.read", false],
    ]);
  });
});
