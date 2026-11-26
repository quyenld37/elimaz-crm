// @vitest-environment jsdom
// Kế hoạch M4 Task 177 (LV03, LV07–LV09 phía màn hình): khung chốt comment. Tìm biến thể không gửi sellerId; chốt gửi
// đúng thân có khóa; thành công → dòng "Đã lên đơn … · An", khung hiện trạng thái xin địa chỉ; 409 comment_claimed,
// out_of_stock, open_draft_exists (Thêm vào đơn gửi add_to_draft + orderId + expectedVersion), 422 seller_mismatch;
// không có quyền tạo đơn → nút khóa kèm lý do; nhịp tim drafting_order.
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { stubDialog } from "../don/donTestkit";
import {
  HITS,
  OLD_DRAFT,
  ORD_OLD,
  S_LIVE,
  claimResult,
  comment,
  fakeLayout,
  grantsOf,
  installLiveFake,
  renderLive,
  type LiveFake,
} from "./liveTestkit";

beforeEach(() => {
  fakeLayout();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const COMMENT = comment(5, {
  text: "chốt đầm be L ạ 0903 222 333",
  hasPhone: true,
  customer: { id: "c5", name: "Phạm Ngọc Anh" },
});
const HIT = HITS[0];

async function openAndPick(p: Partial<LiveFake> = {}) {
  const fake = installLiveFake({ comments: [comment(1), COMMENT], ...p });
  renderLive(`/live/${S_LIVE}`);
  fireEvent.click(await screen.findByText("Phạm Ngọc Anh"));
  await screen.findByText("Chốt comment 20:00:05");
  const box = screen.getByRole("combobox", { name: "Thêm sản phẩm theo mã, tên hoặc barcode" });
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: "DAM" } });
  fireEvent.click(await screen.findByRole("option", { name: new RegExp(HIT?.sku ?? "") }));
  await screen.findByText("Tạm tính");
  return fake;
}

const claimWrites = (f: LiveFake) => f.writes.filter((w) => w.path.endsWith("/claim"));
const chot = () => screen.getByRole("button", { name: "Tạo đơn nháp và xin địa chỉ" });

describe("ClaimPanel", () => {
  it("tìm biến thể gọi searchVariants với q, limit, không có sellerId; hiện có thể bán; SĐT trong comment", async () => {
    const fake = await openAndPick();
    const q = fake.searchCalls.at(-1);
    expect(q?.get("q")).toBe("DAM");
    expect(q?.get("limit")).toBeTruthy();
    expect(q?.has("sellerId")).toBe(false);
    expect(screen.getByRole("button", { name: "Dùng số 0903 222 333" })).toBeTruthy();
    expect(screen.getByText('Nhắn riêng xin địa chỉ (mẫu "Xin địa chỉ live")')).toBeTruthy();
  });

  it("chốt thành công: thân đúng, có khóa; dòng 'Đã lên đơn DH0001241 · Trần Minh An'; khung hiện đã xếp tin xin địa chỉ", async () => {
    const fake = await openAndPick();
    fireEvent.click(chot());
    await screen.findByText("Đã tạo đơn nháp DH0001241");
    const w = claimWrites(fake);
    expect(w).toHaveLength(1);
    expect(w[0]?.path).toBe(`/api/v1/live-comments/${COMMENT.messageId}/claim`);
    expect(w[0]?.key).toBeTruthy();
    expect(w[0]?.body).toEqual({ items: [{ variantId: HIT?.variantId, qty: 1 }], mode: "new", askAddress: true });
    expect(screen.getByText("Đã lên đơn DH0001241 · Trần Minh An")).toBeTruthy();
    expect(screen.getByText(/Đã xếp tin nhắn riêng xin địa chỉ/)).toBeTruthy();
  });

  it("chốt được nhưng Meta không cho xin địa chỉ → 'Chưa xin được địa chỉ.' kèm lý do của máy chủ", async () => {
    const fake = await openAndPick();
    fake.replies.push({
      status: 201,
      body: claimResult({ ask: { status: "skipped_policy", reason: "Page đang ở chế độ chỉ xem" } }),
    });
    fireEvent.click(chot());
    await screen.findByText("Chưa xin được địa chỉ.");
    expect(screen.getByText(/Page đang ở chế độ chỉ xem/)).toBeTruthy();
  });

  it("409 comment_claimed → 'Trần Minh An đang lên đơn cho comment này.' và nút mở đơn", async () => {
    const fake = await openAndPick();
    fake.replies.push({
      status: 409,
      body: {
        error: "comment_claimed",
        request_id: "t",
        claimedBy: "Trần Minh An",
        orderId: ORD_OLD,
        orderCode: "DH0001240",
      },
    });
    fireEvent.click(chot());
    await screen.findByText("Trần Minh An đang lên đơn cho comment này.");
    expect(screen.getByRole("link", { name: "Mở đơn DH0001240" }).getAttribute("href")).toBe(`/don/${ORD_OLD}`);
  });

  it("409 out_of_stock {available:0} → 'Hết hàng (còn 0)' kèm tên món, không tạo gì", async () => {
    const fake = await openAndPick();
    fake.replies.push({
      status: 409,
      body: {
        error: "out_of_stock",
        request_id: "t",
        variantId: HIT?.variantId,
        variantIds: [HIT?.variantId],
        available: 0,
      },
    });
    fireEvent.click(chot());
    await screen.findByText(/^Hết hàng \(còn 0\)/);
    expect(screen.getByText(/Chưa tạo đơn nháp, không giữ món nào/)).toBeTruthy();
  });

  it("409 open_draft_exists → hai nút; 'Thêm vào đơn' gửi add_to_draft kèm orderId, expectedVersion; 'Tạo đơn mới' gửi forceNew", async () => {
    const fake = await openAndPick();
    const draft = { error: "open_draft_exists", request_id: "t", drafts: [OLD_DRAFT] };
    fake.replies.push({ status: 409, body: draft });
    fireEvent.click(chot());
    await screen.findByText("Khách đã có đơn nháp DH0001238 trong phiên này.");
    fireEvent.click(screen.getByRole("button", { name: "Tạo đơn mới" }));
    await waitFor(() => {
      expect(claimWrites(fake)).toHaveLength(2);
    });
    expect(claimWrites(fake)[1]?.body).toMatchObject({ mode: "new", forceNew: true });
    await screen.findByText("Đã tạo đơn nháp DH0001241");
  });

  it("'Thêm vào đơn DH0001238' gửi mode add_to_draft, orderId, expectedVersion; khóa khác lần chốt đầu", async () => {
    const fake = await openAndPick();
    fake.replies.push({ status: 409, body: { error: "open_draft_exists", request_id: "t", drafts: [OLD_DRAFT] } });
    fireEvent.click(chot());
    fireEvent.click(await screen.findByRole("button", { name: "Thêm vào đơn DH0001238" }));
    await screen.findByText("Đã thêm vào đơn DH0001241");
    const [first, second] = claimWrites(fake);
    expect(second?.body).toEqual({
      items: [{ variantId: HIT?.variantId, qty: 1 }],
      mode: "add_to_draft",
      askAddress: true,
      orderId: ORD_OLD,
      expectedVersion: 4,
    });
    expect(second?.key).not.toBe(first?.key);
  });

  it("422 seller_mismatch → 'Sản phẩm không thuộc người bán của Page này.'", async () => {
    const fake = await openAndPick();
    fake.replies.push({ status: 422, body: { error: "seller_mismatch", request_id: "t" } });
    fireEvent.click(chot());
    await screen.findByText("Sản phẩm không thuộc người bán của Page này.");
  });

  it("503 retry_later rồi 201 → hai yêu cầu cùng Idempotency-Key", async () => {
    const fake = await openAndPick();
    fake.replies.push({ status: 503, body: { error: "retry_later", request_id: "t" } });
    fireEvent.click(chot());
    await screen.findByText("Đã tạo đơn nháp DH0001241", undefined, { timeout: 3000 });
    const [a, b] = claimWrites(fake);
    expect(claimWrites(fake)).toHaveLength(2);
    expect(a?.key).toBe(b?.key);
  });

  it("không có order.create → nút chốt khóa kèm lý do; bỏ tick xin địa chỉ → askAddress false", async () => {
    await openAndPick({ grants: grantsOf("conversation.read", "live.operate", "inventory.read") });
    expect(chot()).toHaveProperty("disabled", true);
    expect(screen.getByText("Chưa có quyền tạo đơn.")).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox"));
    expect(screen.getByRole("button", { name: "Tạo đơn nháp" })).toBeTruthy();
  });

  it("mở khung → nhịp tim drafting_order (khóa mới mỗi nhịp); người khác đang ở hội thoại hiện tên", async () => {
    const fake = await openAndPick({ presence: [{ userId: "u-an", displayName: "An", state: "viewing" }] });
    await screen.findByText("An cũng đang xem");
    const beats = fake.writes.filter((w) => w.path.endsWith("/presence"));
    expect(beats.length).toBeGreaterThanOrEqual(1);
    expect(beats[0]?.path).toBe(`/api/v1/conversations/${COMMENT.conversationId}/presence`);
    expect(beats[0]?.body).toEqual({ state: "drafting_order" });
  });

  it("comment đã chốt → khung hiện 'Đã lên đơn … bởi …', không có nút chốt", async () => {
    installLiveFake({
      comments: [
        comment(5, {
          customer: { id: "c5", name: "Phạm Ngọc Anh" },
          claim: { by: { id: "x", name: "Lan" }, orderId: ORD_OLD, orderCode: "DH0001238" },
        }),
      ],
    });
    renderLive(`/live/${S_LIVE}`);
    fireEvent.click(await screen.findByText("Phạm Ngọc Anh"));
    await screen.findByText("Đã lên đơn DH0001238 bởi Lan.");
    expect(screen.queryByRole("button", { name: "Tạo đơn nháp và xin địa chỉ" })).toBeNull();
  });
});
