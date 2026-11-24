// Chữ của chủ đề, mức hài lòng, trạng thái góp ý (HV22 của docs/mockups/m5) và nút đổi trạng thái của dòng mở.
import { describe, expect, it } from "vitest";
import { moodText, nextStates } from "./feedbackRules";

describe("moodText", () => {
  it("1–5 bằng chữ; không có hay ngoài thang → —", () => {
    expect([1, 2, 3, 4, 5].map((m) => moodText(m))).toEqual([
      "1/5 · Rất không hài lòng",
      "2/5 · Không hài lòng",
      "3/5 · Bình thường",
      "4/5 · Hài lòng",
      "5/5 · Rất hài lòng",
    ]);
    expect(moodText(undefined)).toBe("—");
    expect(moodText(0)).toBe("—");
    expect(moodText(6)).toBe("—");
  });
});

describe("nextStates", () => {
  it("hai trạng thái khác trạng thái hiện tại, theo thứ tự mới → đã xem → đã phản hồi", () => {
    expect(nextStates("new")).toEqual(["seen", "answered"]);
    expect(nextStates("seen")).toEqual(["new", "answered"]);
    expect(nextStates("answered")).toEqual(["new", "seen"]);
  });
});
