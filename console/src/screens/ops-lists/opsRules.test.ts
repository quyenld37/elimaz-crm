import { describe, expect, it } from "vitest";
import {
  agedState,
  clock,
  dueText,
  kindCounts,
  needsCallTime,
  overdueLabel,
  phoneText,
  reviewKindLabel,
  riskChip,
  statsText,
  statusText,
  telHref,
} from "./opsRules";

const NOW = Date.parse("2026-11-22T06:00:00Z"); // 13:00 giờ VN

describe("opsRules", () => {
  it("giờ và hạn theo giờ Việt Nam", () => {
    expect(clock("2026-11-22T03:00:00Z")).toBe("10:00");
    expect(dueText("2026-11-22T03:00:00Z")).toBe("hạn 10:00 · 22/11");
    expect(dueText("2026-11-22T20:30:00Z")).toBe("hạn 03:30 · 23/11");
  });
  it("nhãn quá hạn: giờ, ngày, ít nhất 1 giờ, chưa quá hạn thì không có", () => {
    expect(overdueLabel({ dueAt: "2026-11-22T03:00:00Z", overdue: true }, NOW)).toBe("Quá hạn 3 giờ");
    expect(overdueLabel({ dueAt: "2026-11-22T05:50:00Z", overdue: true }, NOW)).toBe("Quá hạn 1 giờ");
    expect(overdueLabel({ dueAt: "2026-11-19T06:00:00Z", overdue: true }, NOW)).toBe("Quá hạn 3 ngày");
    expect(overdueLabel({ dueAt: "2026-11-22T03:00:00Z", overdue: false }, NOW)).toBeNull();
  });
  it("trạng thái đang xử lý kèm giờ gọi lại", () => {
    expect(statusText({ status: "in_progress", nextCallAt: "2026-11-22T08:00:00Z" })).toBe(
      "Đang xử lý · gọi lại 15:00",
    );
    expect(statusText({ status: "in_progress" })).toBe("Đang xử lý");
    expect(statusText({ status: "open" })).toBe("");
  });
  it("kết quả cần giờ gọi lại; thống kê; nhãn rủi ro", () => {
    expect(needsCallTime("no_answer")).toBe(true);
    expect(needsCallTime("reschedule")).toBe(true);
    expect(needsCallTime("redeliver")).toBe(false);
    expect(statsText({ delivered: 3, returned: 2, returnRateBp: 4000 })).toBe("Nhận 3 · Hoàn 2 (40%)");
    expect(riskChip("high")).toBe("Hay hoàn");
    expect(riskChip("watch")).toBe("Có hoàn");
    expect(riskChip("none")).toBeNull();
  });
  it("liên kết gọi: chỉ chữ số, giữ + đầu, số rỗng không có", () => {
    expect(telHref("0912 888 000")).toBe("tel:0912888000");
    expect(telHref("+84 912-888-000")).toBe("tel:+84912888000");
    expect(telHref("  ")).toBeNull();
    expect(phoneText("0912888000")).toBe("0912 888 000");
    expect(phoneText("+84912888000")).toBe("+84912888000");
  });
  it("loại Cần xem: tên, loại lạ giữ mã, đếm theo loại", () => {
    expect(reviewKindLabel("in_doubt_create")).toBe("Không rõ đã tạo");
    expect(reviewKindLabel("loai_la")).toBe("loai_la");
    expect(kindCounts([{ kind: "a" }, { kind: "b" }, { kind: "a" }])).toEqual([
      { kind: "a", n: 2 },
      { kind: "b", n: 1 },
    ]);
  });
  it("hàng giữ quá hạn", () => {
    expect(agedState({ state: "held", ageDays: 23 })).toBe("Đang giữ · 23 ngày");
    expect(agedState({ state: "in_transit", ageDays: 184 })).toBe("Đang giao · 184 ngày");
  });
});
