// @vitest-environment jsdom
// Quy tắc tự động và cài đặt Page (kế hoạch M4 Task 178 Step 1; mockup cai-dat.html trạng thái A, B; CH11, CH12, CH16;
// HV33, HV52 sửa, HV53): giới hạn báo tại ô trước khi gửi, 409 "Có người vừa sửa, tải lại trang", khối theo tính năng
// kênh, chỉ xem khi không có quyền sửa, chấm "Chưa lưu" và hỏi lại khi rời trang.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { P1, P2, T1, account, grant, setupKenh } from "./kenhTestkit";
import { screens } from "./route";

const t = setupKenh();

const renderPage = () => renderScreens(screens, `/settings/pages/${P1}`);
const block = async (title: string) => screen.findByRole("region", { name: new RegExp(`^${title}`) });
const ruleWrites = (kind: string) => t.fake.writes.filter((w) => w.path.endsWith(`/automation-rules/${kind}`));

describe("Tin chào: nút trả lời nhanh", () => {
  it("nút 21 ký tự → báo lỗi tại ô trước khi gửi, không thêm, không gửi", async () => {
    renderPage();
    const b = await block("Tin chào");
    const input = within(b).getByLabelText(/^Nút mới/);
    fireEvent.change(input, { target: { value: "Tư vấn size áo và váy" } });
    expect(within(b).getByText("Tối đa 20 ký tự (đang 21).")).toBeTruthy();
    expect(within(b).getByText("21/20").className).toContain("is-over");
    fireEvent.click(within(b).getByRole("button", { name: "+ Thêm nút" }));
    expect(within(b).queryByRole("button", { name: "Xóa nút Tư vấn size áo và váy" })).toBeNull();
    expect(t.fake.writes).toHaveLength(0);
  });

  it("nút 20 ký tự được thêm; Lưu gửi đủ nút kèm version của quy tắc", async () => {
    renderPage();
    const b = await block("Tin chào");
    fireEvent.change(within(b).getByLabelText(/^Nút mới/), { target: { value: "Địa chỉ shop ở đâu ạ" } });
    fireEvent.click(within(b).getByRole("button", { name: "+ Thêm nút" }));
    expect(within(b).getByText("3/13 nút · mỗi nút tối đa 20 ký tự")).toBeTruthy();
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(ruleWrites("greeting")).toHaveLength(1);
    });
    expect(ruleWrites("greeting")[0]?.body).toEqual({
      enabled: true,
      config: {
        kind: "greeting",
        text: "Chào bạn, ELIMAZ đây ạ!",
        quickReplies: [
          { title: "Tư vấn size", payload: "Tư vấn size" },
          { title: "Xem bảng giá", payload: "Xem bảng giá" },
          { title: "Địa chỉ shop ở đâu ạ", payload: "Địa chỉ shop ở đâu ạ" },
        ],
      },
      expectedVersion: 5,
    });
  });

  it('"Thêm nút" khóa ở nút thứ 13 kèm lý do', async () => {
    const g = t.fake.rules.find((r) => r.kind === "greeting");
    if (g?.config.kind !== "greeting") throw new Error("thiếu tin chào");
    g.config.quickReplies = Array.from({ length: 13 }, (_, i) => ({ title: `Nút ${String(i)}`, payload: "x" }));
    renderPage();
    const b = await block("Tin chào");
    expect(within(b).getByRole("button", { name: "+ Thêm nút" })).toHaveProperty("disabled", true);
    expect(within(b).getByText("Đã đủ 13 nút.")).toBeTruthy();
  });

  it("kênh không có nút trả lời nhanh → không có phần nút (HV33)", async () => {
    const f = account().features;
    if (f === undefined) throw new Error("thiếu features");
    t.fake.channels = [account({ features: { ...f, quickReplies: false } })];
    renderPage();
    const b = await block("Tin chào");
    expect(within(b).queryByText(/Nút trả lời nhanh/)).toBeNull();
  });
});

describe("Tự trả lời comment", () => {
  it('"Thêm câu" khóa ở câu thứ 5 kèm lý do', async () => {
    renderPage();
    const b = await block("Tự trả lời comment");
    const add = within(b).getByRole("button", { name: "+ Thêm câu" });
    fireEvent.click(add);
    fireEvent.click(add);
    fireEvent.click(add);
    expect(within(b).getByText("5/5 câu")).toBeTruthy();
    expect(add).toHaveProperty("disabled", true);
    expect(within(b).getByText("Tối đa 5 câu.")).toBeTruthy();
  });

  it("câu rỗng, số giờ ngoài 1–72 → báo tại ô, không gửi", async () => {
    renderPage();
    const b = await block("Tự trả lời comment");
    fireEvent.click(within(b).getByRole("button", { name: "+ Thêm câu" }));
    fireEvent.change(within(b).getByLabelText(/Mỗi người mỗi bài/), { target: { value: "100" } });
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    expect(within(b).getByText("Nhập câu trả lời hoặc xóa dòng này.")).toBeTruthy();
    expect(within(b).getByText("Nhập số giờ từ 1 đến 72.")).toBeTruthy();
    expect(t.fake.writes).toHaveLength(0);
  });

  it("chọn mẫu tin riêng → gửi privateMessage.templateId", async () => {
    renderPage();
    const b = await block("Tự trả lời comment");
    fireEvent.change(within(b).getByLabelText("Tin riêng kèm theo"), { target: { value: `t:${T1}` } });
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(ruleWrites("auto_reply_comment")).toHaveLength(1);
    });
    expect(ruleWrites("auto_reply_comment")[0]?.body).toMatchObject({
      config: { privateMessage: { templateId: T1 }, perUserPerPostHours: 24, includeLive: false },
      expectedVersion: 2,
    });
  });

  it("409 version_conflict → 'Có người vừa sửa, tải lại trang'; Tải lại đọc lại quy tắc", async () => {
    t.fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderPage();
    const b = await block("Tự trả lời comment");
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    expect(await within(b).findByText("Có người vừa sửa, tải lại trang.")).toBeTruthy();
    const before = t.fake.reads.filter((r) => r.endsWith("/automation-rules")).length;
    fireEvent.click(within(b).getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(t.fake.reads.filter((r) => r.endsWith("/automation-rules")).length).toBe(before + 1);
    });
    await waitFor(() => {
      expect(within(b).queryByText("Có người vừa sửa, tải lại trang.")).toBeNull();
    });
  });
});

describe("Tự ẩn, nhắn lại", () => {
  it("Tự ẩn: chọn Tất cả → Lưu gửi mode all, enabled true", async () => {
    renderPage();
    const b = await block("Tự ẩn comment");
    fireEvent.click(within(b).getByRole("radio", { name: "Tất cả" }));
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(ruleWrites("auto_hide")[0]?.body).toEqual({
        enabled: true,
        config: { kind: "auto_hide", mode: "all", exceptPostIds: [] },
        expectedVersion: 4,
      });
    });
  });

  it("kênh không ẩn được comment → không có khối Tự ẩn (ẩn hẳn, HV33)", async () => {
    const f = account().features;
    if (f === undefined) throw new Error("thiếu features");
    t.fake.channels = [account({ features: { ...f, hideComment: false } })];
    renderPage();
    await block("Tin chào");
    expect(screen.queryByRole("region", { name: /^Tự ẩn comment/ })).toBeNull();
  });

  it("Nhắn lại chưa cấu hình (uuid rỗng): bật mà chưa chọn mẫu → báo tại ô; chọn mẫu → gửi templateId", async () => {
    renderPage();
    const b = await block("Nhắn lại khách chưa trả lời");
    fireEvent.click(within(b).getByRole("button", { name: "Bật nhắn lại khách chưa trả lời" }));
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    expect(within(b).getByText("Chọn mẫu để bật nhắn lại.")).toBeTruthy();
    expect(t.fake.writes).toHaveLength(0);
    fireEvent.change(within(b).getByLabelText("Mẫu nhắn lại"), { target: { value: T1 } });
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(ruleWrites("follow_up")[0]?.body).toEqual({
        enabled: true,
        config: { kind: "follow_up", afterHours: 4, templateId: T1 },
        expectedVersion: 0,
      });
    });
  });
});

describe("cài đặt Page: xoay vòng, Live, nguồn đơn", () => {
  it("Live: khung giờ chồng nhau → báo tại khung, không gửi; hạn đơn nháp từ Cài đặt đơn", async () => {
    renderPage();
    const b = await block("Live");
    expect(await within(b).findByText("24 giờ · đặt ở Cài đặt đơn, dùng chung mọi đơn nháp")).toBeTruthy();
    fireEvent.click(within(b).getByRole("button", { name: "+ Thêm khung giờ" }));
    fireEvent.change(within(b).getByLabelText("Từ (khung 2)"), { target: { value: "22:00" } });
    fireEvent.change(within(b).getByLabelText("Đến (khung 2)"), { target: { value: "23:30" } });
    fireEvent.change(within(b).getByLabelText("Nguồn đơn (khung 2)"), { target: { value: t.fake.sources[1]?.id } });
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    expect(within(b).getAllByText(/Chồng với khung/)).toHaveLength(2);
    expect(t.fake.writes).toHaveLength(0);
  });

  it("Nguồn đơn và nâng cao: lưu gửi cả bản cài đặt Page với expectedVersion, chỉ đổi trường của khối", async () => {
    renderPage();
    const b = await block("Nguồn đơn và nâng cao");
    fireEvent.change(within(b).getByLabelText("Số phút"), { target: { value: "30" } });
    fireEvent.click(within(b).getByRole("radio", { name: "Luôn bật" }));
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(t.fake.writes).toHaveLength(1);
    });
    expect(t.fake.writes[0]).toMatchObject({ method: "PUT", path: `/api/v1/channels/${P1}/page-settings` });
    expect(t.fake.writes[0]?.body).toEqual({
      roundRobinEnabled: false,
      roundRobinUserIds: [],
      askAddressTemplateId: null,
      inboxTrafficSourceId: null,
      commentTrafficSourceId: null,
      liveSlots: [{ from: "19:00", to: "23:00", trafficSourceId: t.fake.sources[0]?.id }],
      commentPollMode: "on",
      automationMaxAgeMinutes: 30,
      expectedVersion: 2,
    });
  });

  it("hai khối cùng bản cài đặt: lưu khối này xong, khối kia vẫn lưu được với version mới", async () => {
    renderPage();
    const src = await block("Nguồn đơn và nâng cao");
    fireEvent.change(within(src).getByLabelText("Số phút"), { target: { value: "30" } });
    fireEvent.click(within(src).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(t.fake.writes).toHaveLength(1);
    });
    const rr = await block("Xoay vòng phân công");
    fireEvent.click(within(rr).getByRole("button", { name: "Bật xoay vòng phân công" }));
    fireEvent.click(await within(rr).findByRole("checkbox", { name: "Trần Minh An" }));
    fireEvent.click(within(rr).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(t.fake.writes).toHaveLength(2);
    });
    expect(t.fake.writes[1]?.body).toMatchObject({
      roundRobinEnabled: true,
      automationMaxAgeMinutes: 30,
      expectedVersion: 3,
    });
    expect(within(rr).queryByText("Có người vừa sửa, tải lại trang.")).toBeNull();
  });

  it("409 version_conflict khi lưu cài đặt Page → 'Có người vừa sửa, tải lại trang'", async () => {
    t.fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderPage();
    const b = await block("Nguồn đơn và nâng cao");
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    expect(await within(b).findByText("Có người vừa sửa, tải lại trang.")).toBeTruthy();
  });
});

describe("quyền và Page chỉ xem", () => {
  it("không có conversation.settings trên Page này → chỉ xem: hộp báo, nút Lưu khóa kèm lý do", async () => {
    t.fake.grants = [grant("conversation.read"), grant("conversation.settings", { type: "page", id: P2 })];
    renderPage();
    expect(await screen.findByText("Bạn chỉ xem được cài đặt của Page này.")).toBeTruthy();
    const b = await block("Tin chào");
    expect(within(b).getByRole("button", { name: "Lưu" })).toHaveProperty("disabled", true);
    expect(within(b).getByText("Chưa có quyền sửa.")).toBeTruthy();
  });

  it("conversation.settings đúng Page → sửa được", async () => {
    t.fake.grants = [grant("conversation.read"), grant("conversation.settings", { type: "page", id: P1 })];
    renderPage();
    const b = await block("Tin chào");
    expect(within(b).getByRole("button", { name: "Lưu" })).toHaveProperty("disabled", false);
    expect(screen.queryByText("Bạn chỉ xem được cài đặt của Page này.")).toBeNull();
  });

  it("Page đang chỉ xem → băng nói CRM chưa gửi gì, kể cả tin tự động", async () => {
    renderPage();
    expect(await screen.findByText("Page ELIMAZ đang ở chế độ chỉ xem.")).toBeTruthy();
  });
});

describe("chưa lưu (HV52 sửa)", () => {
  it("sửa một khối → chấm 'Chưa lưu'; bấm thẻ khác → hỏi 'Bỏ thay đổi?'; Ở lại giữ trang; Bỏ thay đổi thì đi", async () => {
    const { router } = renderPage();
    const b = await block("Tin chào");
    fireEvent.change(within(b).getByRole("textbox", { name: /^Nội dung/ }), { target: { value: "Chào!" } });
    expect(within(b).getByText(/Chưa lưu/)).toBeTruthy();
    fireEvent.click(screen.getByRole("link", { name: "Nhãn" }));
    let dlg = screen.getByRole("dialog", { name: "Bỏ thay đổi?" });
    expect(within(dlg).getByText(/Còn khối chưa lưu: Tin chào/)).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Ở lại" }));
    expect(router.state.location.pathname).toBe(`/settings/pages/${P1}`);
    fireEvent.click(screen.getByRole("link", { name: "Nhãn" }));
    dlg = screen.getByRole("dialog", { name: "Bỏ thay đổi?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Bỏ thay đổi" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/settings/tags");
    });
  });

  it("không có thay đổi → bấm thẻ đi thẳng, không hỏi", async () => {
    const { router } = renderPage();
    await block("Tin chào");
    fireEvent.click(screen.getByRole("link", { name: "Mẫu trả lời" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/settings/templates");
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("lưu xong thì hết chấm 'Chưa lưu'", async () => {
    renderPage();
    const b = await block("Tin chào");
    fireEvent.change(within(b).getByRole("textbox", { name: /^Nội dung/ }), { target: { value: "Chào!" } });
    fireEvent.click(within(b).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(within(b).queryByText(/Chưa lưu/)).toBeNull();
    });
  });
});

it("/settings/inbox chuyển sang thẻ Page đầu tiên", async () => {
  const { router } = renderScreens(screens, "/settings/inbox");
  await waitFor(() => {
    expect(router.state.location.pathname).toBe(`/settings/pages/${P1}`);
  });
});
