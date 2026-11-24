// Luật hiện của màn Nhật ký thao tác: nhãn, người làm, trường đổi, khoảng ngày VN → [from, to) (Task 462).
import { describe, expect, it } from "vitest";
import {
  ACTION_OPTIONS,
  ACTION_TEXT,
  actionText,
  actorOptionText,
  actorText,
  dayRange,
  entityText,
  fieldsText,
} from "./auditRules";

describe("nhãn", () => {
  it("mã có nhãn → nhãn; mã lạ → nguyên mã", () => {
    expect(actionText("order.web_intake.set")).toBe("Tạm dừng / mở lại nhận đơn web");
    expect(actionText("x.y.moi")).toBe("x.y.moi");
  });

  it("ô lọc có mọi mã có nhãn, không trùng", () => {
    expect(new Set(ACTION_OPTIONS).size).toBe(Object.keys(ACTION_TEXT).length);
  });

  it("thực thể toàn shop chỉ nhãn; thực thể thường nhãn + mã; loại lạ → nguyên loại", () => {
    expect(entityText({ entityType: "order.web_intake", entityId: "t1" })).toEqual({ label: "Nhận đơn web" });
    expect(entityText({ entityType: "order_request", entityId: "r1" })).toEqual({ label: "Yêu cầu", id: "r1" });
    expect(entityText({ entityType: "la", entityId: undefined })).toEqual({ label: "la" });
    // Task 488: có tên → tên thay mã; bộ phối là thực thể toàn shop.
    expect(entityText({ entityType: "order_request", entityId: "r1", entityName: "YC-1" })).toEqual({
      label: "Yêu cầu",
      id: "YC-1",
    });
    expect(entityText({ entityType: "content.looks", entityId: "shop" })).toEqual({ label: "Bộ phối" });
    expect(entityText({ entityType: "order.web_intake", entityId: "t1", entityName: "x" })).toEqual({
      label: "Nhận đơn web",
    });
  });

  it("ô lọc người làm: nhân viên theo tên, khóa kèm 'Khóa:', đã khóa / thu hồi ghi rõ", () => {
    expect(actorOptionText({ actorType: "staff", name: "Lan", active: true })).toBe("Lan");
    expect(actorOptionText({ actorType: "staff", name: "Lan", active: false })).toBe("Lan (đã khóa)");
    expect(actorOptionText({ actorType: "service", name: "Web", active: true })).toBe("Khóa: Web");
    expect(actorOptionText({ actorType: "service", name: "Web", active: false })).toBe("Khóa: Web (đã thu hồi)");
  });

  it("người làm: hệ thống luôn 'Hệ thống'; thiếu tên → mã; thiếu cả hai → 'Không rõ'", () => {
    expect(actorText({ actorType: "system" })).toEqual({ name: "Hệ thống", kind: "Hệ thống" });
    expect(actorText({ actorType: "staff", actorId: "u1", actorName: "Lan" })).toEqual({
      name: "Lan",
      kind: "Nhân viên",
    });
    expect(actorText({ actorType: "carrier", actorId: "ghn" })).toEqual({ name: "ghn", kind: "Hãng vận chuyển" });
    expect(actorText({ actorType: "service" })).toEqual({ name: "Không rõ", kind: "Khóa dịch vụ" });
  });

  it("trường đổi: nhãn ngắn hoặc nguyên tên; rỗng → —", () => {
    expect(fieldsText(["paused", "reason", "ship_address"])).toBe("tạm dừng, lý do, ship_address");
    expect(fieldsText([])).toBe("—");
  });
});

describe("dayRange", () => {
  it("trống cả hai → không gửi from, to", () => {
    expect(dayRange("", "")).toEqual({});
  });
  it("từ 00:00 ngày đầu giờ VN tới 00:00 ngày sau ngày cuối", () => {
    expect(dayRange("2026-11-20", "2026-11-21")).toEqual({
      from: "2026-11-19T17:00:00.000Z",
      to: "2026-11-21T17:00:00.000Z",
    });
  });
  it("cùng một ngày → trọn ngày đó", () => {
    expect(dayRange("2026-11-21", "2026-11-21")).toEqual({
      from: "2026-11-20T17:00:00.000Z",
      to: "2026-11-21T17:00:00.000Z",
    });
  });
  it("chỉ một đầu", () => {
    expect(dayRange("", "2026-11-21")).toEqual({ to: "2026-11-21T17:00:00.000Z" });
    expect(dayRange("2026-11-21", "")).toEqual({ from: "2026-11-20T17:00:00.000Z" });
  });
  it("ngày đầu sau ngày cuối, ngày sai → lỗi", () => {
    expect(dayRange("2026-11-22", "2026-11-21")).toHaveProperty("error");
    expect(dayRange("2026-13-40", "")).toHaveProperty("error");
  });
});
