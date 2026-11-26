// @vitest-environment jsdom
// Băng trạng thái tài khoản kênh trên đầu danh sách (Task 174; mockup inbox.html trạng thái C; HT15 phía màn hình,
// HV08): Page chỉ xem, Page mất kết nối; Page bình thường, đã ngắt không có băng; nút sang Kênh kết nối chỉ khi có quyền.
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it } from "vitest";
import { ChannelBanners } from "./ChannelBanners";
import { P1, P2, account } from "./inboxTestkit";

afterEach(cleanup);

const P3 = "0195f3b2-0000-7000-8000-00000000a003";
const P4 = "0195f3b2-0000-7000-8000-00000000a004";

it("Page read_only và Page degraded → đúng hai câu băng; Page bình thường, đã ngắt không có băng", () => {
  render(
    <MemoryRouter>
      <ChannelBanners
        canManage
        accounts={[
          account(P1, "ELIMAZ Official", { status: "degraded", statusReason: "token_invalid" }),
          account(P2, "ELIMAZ Sale", { sendMode: "read_only" }),
          account(P3, "ELIMAZ Kids"),
          account(P4, "ELIMAZ Cũ", { status: "disconnected", sendMode: "read_only" }),
        ]}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("Page ELIMAZ Sale đang ở chế độ chỉ xem.")).toBeTruthy();
  expect(screen.getByText("Page ELIMAZ Official mất kết nối, cần quản trị kết nối lại.")).toBeTruthy();
  expect(screen.getByText(/đổi mật khẩu hoặc gỡ quyền/)).toBeTruthy();
  expect(screen.queryByText(/ELIMAZ Kids/)).toBeNull();
  expect(screen.queryByText(/ELIMAZ Cũ/)).toBeNull();
  expect(screen.getByRole("link", { name: "Mở Kênh kết nối" }).getAttribute("href")).toBe("/settings/channels");
});

it("Page mất kết nối mà đang chỉ xem: chỉ băng mất kết nối; không có quyền quản trị kênh: không có nút", () => {
  render(
    <MemoryRouter>
      <ChannelBanners
        canManage={false}
        accounts={[
          account(P1, "ELIMAZ Official", {
            status: "degraded",
            sendMode: "read_only",
            statusReason: "meta_policy:block",
          }),
        ]}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("Page ELIMAZ Official mất kết nối, cần quản trị kết nối lại.")).toBeTruthy();
  expect(screen.getByText(/Meta đang hạn chế Page này/)).toBeTruthy();
  expect(screen.queryByText(/chế độ chỉ xem/)).toBeNull();
  expect(screen.queryByRole("link")).toBeNull();
});

it("không Page nào có vấn đề → không vẽ gì", () => {
  const { container } = render(
    <MemoryRouter>
      <ChannelBanners canManage accounts={[account(P1, "ELIMAZ Official")]} />
    </MemoryRouter>,
  );
  expect(container.innerHTML).toBe("");
});
