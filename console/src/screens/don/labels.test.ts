// Chữ của nhãn đơn, lý do hủy, lý do chờ, phiếu trả (kế hoạch M2 Task M2-28): mọi giá trị enum của hợp đồng
// (openapi.yaml, nguồn của client sinh) có chữ; 17 nhãn đúng thứ tự orderview.Labels() (Task M2-08; sending của M3 Task M3-28).
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { OrderLabel } from "../../api";
import { COUPON_REASON_MESSAGES } from "../../app/errors";
import { QTY_TEXT } from "./receiveRules";
import {
  CANCEL_REASON_TEXT,
  LABEL_BAR,
  LABEL_ORDER,
  LABEL_TEXT,
  PENDING_REASON_TEXT,
  RETURN_KIND_TEXT,
  RETURN_STATUS_TEXT,
  STAFF_CANCEL_REASONS,
  STATUS_TEXT,
  historyReasonText,
  isAttention,
} from "./labels";

const yaml = readFileSync(join(import.meta.dirname, "../../../../openapi/openapi.yaml"), "utf8").split("\n");

/** Giá trị enum của schema `name` trong components (dạng [a, b] hoặc danh sách gạch đầu dòng). */
function schemaEnum(name: string): string[] {
  const start = yaml.findIndex((l) => l === `    ${name}:`);
  expect(start, `openapi.yaml thiếu schema ${name}`).toBeGreaterThan(0);
  for (let i = start + 1; i < yaml.length; i++) {
    const l = yaml[i] ?? "";
    const inline = /^\s+enum: \[(.*)\]\s*$/.exec(l);
    if (inline?.[1] !== undefined) return inline[1].split(",").map((s) => s.trim());
    if (/^\s+enum:\s*$/.test(l)) {
      const out: string[] = [];
      for (const x of yaml.slice(i + 1)) {
        const m = /^\s+- ([a-z_]+)\s*$/.exec(x);
        if (m?.[1] === undefined) break;
        out.push(m[1]);
      }
      return out;
    }
    if (/^ {4}\S/.test(l)) break;
  }
  throw new Error(`schema ${name} không có enum`);
}

// Thứ tự của orderview.Labels() (internal/queries/orderview/label.go).
const GO_LABELS: OrderLabel[] = [
  "draft",
  "new",
  "awaiting_discount_approval",
  "awaiting_customer",
  "out_of_stock",
  "confirmed",
  "packing",
  "packed",
  "sending",
  "awaiting_pickup",
  "shipping",
  "delivery_failed",
  "returning",
  "cancel_requested",
  "cancelled",
  "delivered",
  "returned",
];

const filled = (m: Readonly<Record<string, string>>, keys: string[]) => keys.filter((k) => (m[k] ?? "").trim() === "");

describe("labels", () => {
  it("17 nhãn, đúng thứ tự orderview.Labels() và đúng enum OrderLabel của hợp đồng; nhãn nào cũng có chữ", () => {
    expect(LABEL_ORDER).toEqual(GO_LABELS);
    expect(schemaEnum("OrderLabel")).toEqual(GO_LABELS);
    expect(filled(LABEL_TEXT, GO_LABELS)).toEqual([]);
    expect(Object.keys(LABEL_TEXT).sort()).toEqual([...GO_LABELS].sort());
  });

  it("chữ nhãn theo bảng Task M2-08", () => {
    expect(LABEL_TEXT.awaiting_discount_approval).toBe("Chờ duyệt giảm giá");
    expect(LABEL_TEXT.awaiting_customer).toBe("Chờ khách xác nhận");
    expect(LABEL_TEXT.delivery_failed).toBe("Thất bại");
    expect(LABEL_TEXT.returning).toBe("Đang chuyển hoàn");
    expect(LABEL_TEXT.delivered).toBe("Thành công");
    expect(LABEL_TEXT.returned).toBe("Đã hoàn");
    expect(LABEL_TEXT.sending).toBe("Đang gửi hãng");
  });

  it("thanh nhãn theo mockup: 10 nhãn lọc được ở M2 rồi Tất cả; nhãn cần người xử lý viền đỏ", () => {
    expect(LABEL_BAR).toEqual([
      "draft",
      "new",
      "awaiting_discount_approval",
      "awaiting_customer",
      "out_of_stock",
      "confirmed",
      "cancel_requested",
      "cancelled",
      "delivered",
      "returned",
    ]);
    expect(GO_LABELS.filter(isAttention)).toEqual(["awaiting_discount_approval", "out_of_stock", "cancel_requested"]);
  });

  it("mọi giá trị của OrderStatus, PendingReason, CancelReason, ReturnKind, ReturnStatus, tình trạng nhận có chữ", () => {
    expect(filled(STATUS_TEXT, schemaEnum("OrderStatus"))).toEqual([]);
    expect(filled(PENDING_REASON_TEXT, schemaEnum("PendingReason"))).toEqual([]);
    expect(filled(CANCEL_REASON_TEXT, schemaEnum("CancelReason"))).toEqual([]);
    expect(filled(RETURN_KIND_TEXT, schemaEnum("ReturnKind"))).toEqual([]);
    expect(filled(RETURN_STATUS_TEXT, schemaEnum("ReturnStatus"))).toEqual([]);
    // Task 136: ba ô Tốt / Hỏng / Thiếu thay ô chọn một tình trạng (CONDITION_TEXT đã xóa).
    expect(filled(QTY_TEXT, ["sellable", "damaged", "missing"])).toEqual([]);
  });

  it("mọi lý do từ chối mã (CouponRejectReason) có chữ", () => {
    expect(filled(COUPON_REASON_MESSAGES, schemaEnum("CouponRejectReason"))).toEqual([]);
  });

  it("5 lý do nhân viên chọn khi hủy theo đúng thứ tự mockup; draft_expired, duplicate chỉ hệ thống đặt", () => {
    expect(STAFF_CANCEL_REASONS).toEqual([
      "customer_changed_mind",
      "out_of_stock",
      "unreachable",
      "wrong_info",
      "other",
    ]);
    expect(STAFF_CANCEL_REASONS.map((r) => CANCEL_REASON_TEXT[r])).toEqual([
      "Khách đổi ý",
      "Hết hàng",
      "Không liên lạc được",
      "Sai thông tin",
      "Lý do khác",
    ]);
  });

  it("lý do trong lịch sử trạng thái: lý do hủy, lý do của máy chủ; lạ → không chữ", () => {
    expect(historyReasonText("manual_discount_over_threshold")).toBe("Giảm tay vượt mức duyệt");
    expect(historyReasonText("duplicate")).toBe("Đơn trùng");
    expect(historyReasonText("discount_approved")).toBe("Đã duyệt giảm giá");
    expect(historyReasonText("la_qua")).toBeNull();
    expect(historyReasonText(undefined)).toBeNull();
  });
});
