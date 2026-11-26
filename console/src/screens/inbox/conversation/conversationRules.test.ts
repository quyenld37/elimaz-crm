// Quy luật thuần của màn hội thoại (Task 175): đồng hồ còn lại, chính sách gửi → ô gõ, trạng thái gửi → nút (HV14 sửa),
// khóa nhắn riêng, gộp trang tin.
import { describe, expect, it } from "vitest";
import { composerPolicy, mergeMessages, privateReplyLock, remaining, statusView } from "./conversationRules";
import { detail, msg, out } from "./conversationTestkit";

const NOW = Date.parse("2026-11-26T03:00:00Z");
const MIN = 60_000;

describe("conversationRules", () => {
  it("remaining: phút, giờ, ngày; hết → null", () => {
    expect(remaining(NOW + 30 * MIN + 20_000, NOW)).toBe("30 phút");
    expect(remaining(NOW + (23 * 60 + 10) * MIN, NOW)).toBe("23 giờ 10 phút");
    expect(remaining(NOW + 20 * 60 * MIN, NOW)).toBe("20 giờ");
    expect(remaining(NOW + (5 * 1440 + 120) * MIN, NOW)).toBe("5 ngày 2 giờ");
    expect(remaining(NOW + 30_000, NOW)).toBeNull();
    expect(remaining(NOW - MIN, NOW)).toBeNull();
  });

  it("composerPolicy: không quyền, chỉ xem, cửa sổ đóng khóa; Human Agent mở kèm đồng hồ", () => {
    expect(composerPolicy(detail(), false, NOW)).toMatchObject({ locked: true });
    expect(
      composerPolicy(detail({ sendPolicy: { allowed: false, reason: "window_closed" } }), true, NOW),
    ).toMatchObject({ locked: true, title: "Đã quá 24 giờ từ tin cuối của khách, Facebook không cho gửi tin thường" });
    const ha = composerPolicy(
      detail({ sendPolicy: { allowed: true, mode: "human_agent", until: new Date(NOW + 1440 * MIN).toISOString() } }),
      true,
      NOW,
    );
    expect(ha).toEqual({
      locked: false,
      humanAgent: true,
      clock: "1 ngày",
      until: new Date(NOW + 1440 * MIN).toISOString(),
    });
  });

  it("statusView: nút gửi lại chỉ ở in_doubt (hỏi lại), expired, failed lý do tạm", () => {
    const fix = (s: ReturnType<typeof statusView>) =>
      s?.kind === "fix" ? { resend: s.resend, confirm: s.confirm } : null;
    expect(fix(statusView(out(1, "in_doubt")))).toEqual({ resend: true, confirm: true });
    expect(fix(statusView(out(1, "expired")))).toEqual({ resend: true, confirm: false });
    expect(fix(statusView(out(1, "failed", { failReason: "not_sent" })))).toEqual({ resend: true, confirm: false });
    expect(fix(statusView(out(1, "failed", { failReason: "window_closed" })))).toEqual({
      resend: false,
      confirm: false,
    });
    expect(fix(statusView(out(1, "failed")))).toEqual({ resend: false, confirm: false });
    expect(statusView(out(1, "discarded"))?.kind).toBe("line");
    expect(statusView(msg(1))).toBeNull();
  });

  it("privateReplyLock: tin riêng đang sống → used; tin riêng lỗi (none) còn nhắn; quá 7 ngày → expired", () => {
    const at = new Date(NOW - 3_600_000).toISOString();
    expect(privateReplyLock(msg(1, { sentAt: at, privateReply: { state: "in_doubt" } }), NOW)).toBe("used");
    expect(privateReplyLock(msg(1, { sentAt: at, privateReply: { state: "none" } }), NOW)).toBeNull();
    expect(privateReplyLock(msg(1, { sentAt: new Date(NOW - 8 * 86_400_000).toISOString() }), NOW)).toBe("expired");
  });

  it("mergeMessages: một tin một dòng, bản sau thắng, theo thời gian", () => {
    const a = msg(2, { body: "cũ" });
    const merged = mergeMessages([
      [msg(1), a],
      [{ ...a, body: "mới" }, msg(3)],
    ]);
    expect(merged.map((m) => m.body)).toEqual(["Tin 1", "mới", "Tin 3"]);
  });
});
