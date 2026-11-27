// @vitest-environment jsdom
// Thanh trạng thái chung của báo cáo (Task 240, M6-17 thêm): khi tạo lần chạy nền hỏng (mất mạng, bận), thanh chạy nền
// nói rõ lý do và cho "Thử lại" (gửi lại cùng khóa chống trùng, việc của useReport.retryRun).
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReportBanners } from "./ReportBanners";

afterEach(cleanup);

const view = (rep: Parameters<typeof ReportBanners>[0]["rep"]) =>
  render(
    <MemoryRouter>
      <ReportBanners rep={rep} failedHint="gợi ý" />
    </MemoryRouter>,
  );

describe("ReportBanners — chạy nền", () => {
  it("không lỗi: chỉ báo 'đang chạy nền' và nút Tệp xuất, không có Thử lại", () => {
    view({ phase: "background", message: null, reload: vi.fn(), retryRun: vi.fn() });
    expect(screen.getByText(/Báo cáo nặng, đang chạy nền/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Thử lại" })).toBeNull();
  });

  it("tạo lần chạy hỏng: hiện câu lỗi và Thử lại gọi retryRun", () => {
    const retryRun = vi.fn();
    view({ phase: "background", message: "Mất kết nối. Thử lại.", reload: vi.fn(), retryRun });
    expect(screen.getByText("Mất kết nối. Thử lại.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(retryRun).toHaveBeenCalledOnce();
  });
});
