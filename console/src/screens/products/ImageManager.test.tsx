// @vitest-environment jsdom
// Khối Ảnh của màn Sửa mẫu (Task 325; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, D, E): lưới ảnh theo thứ
// tự, ảnh bìa, ảnh mất có ghi chú, dời ← →, xóa có Hoàn tác, đổi màu, ảnh gốc, gắn ảnh vừa xử lý xong.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import type { Product, ProductImage } from "../../api";
import { ImageManager, type ImageManagerProps } from "./ImageManager";
import { fail, ok } from "./productsTestkit";
import type { UploadItem, Uploads } from "./useUpload";

vi.mock("../../api", async (orig) => ({
  ...(await orig<typeof import("../../api")>()),
  attachProductImage: vi.fn(),
  reorderProductImages: vi.fn(),
  deleteProductImage: vi.fn(),
  restoreProductImage: vi.fn(),
  setProductImageColor: vi.fn(),
  getOriginalImageUrl: vi.fn(),
}));

const PID = "0195f3b2-0000-7000-8000-000000000001";
const id = (n: number) => `0195f3b2-0000-7000-8000-0000000002${String(n).padStart(2, "0")}`;
const fid = (n: number) => `0195f3b2-0000-7000-8000-0000000003${String(n).padStart(2, "0")}`;

const VARIANTS = [{ width: 400, format: "jpeg" as const, url: "https://anh.test/img/1-400.jpg" }];
const img = (n: number, extra: Partial<ProductImage> = {}): ProductImage => ({
  id: id(n),
  fileId: fid(n),
  position: n,
  status: "ready",
  variants: VARIANTS,
  ...extra,
});

let images: ProductImage[];
const product = (imgs: ProductImage[], version: number): Product => ({
  id: PID,
  code: "DAM0123",
  name: "Đầm",
  description: "",
  article: [],
  articleHtml: "",
  status: "active",
  version,
  variants: [],
  images: imgs,
});

function fakeUploads(items: UploadItem[] = []): Uploads {
  return { items, add: vi.fn(), dismiss: vi.fn(), update: vi.fn() };
}

function renderManager(p: Partial<ImageManagerProps> = {}) {
  const props: ImageManagerProps = {
    productId: PID,
    images,
    version: 7,
    colors: ["Đen", "Be"],
    canManage: true,
    canUpload: true,
    busy: false,
    uploads: fakeUploads(),
    onProduct: vi.fn(),
    onPending: vi.fn(),
    ...p,
  };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const r = render(
    <QueryClientProvider client={qc}>
      <ImageManager {...props} />
    </QueryClientProvider>,
  );
  return { ...r, props };
}

const tiles = () => screen.getAllByTestId("img-tile");

beforeEach(() => {
  images = [
    img(1, { color: "Đen" }),
    img(2, { color: "Be" }),
    img(3, { status: "lost", variants: undefined, color: "Đen" }),
  ];
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe("ImageManager — hiện ảnh", () => {
  it("ảnh bìa, số ảnh, ảnh mất có ghi chú và nút Tải lại", () => {
    renderManager();
    expect(screen.getByText("3 ảnh")).toBeTruthy();
    expect(tiles().length).toBe(3);
    expect(within(tiles()[0] ?? document.body).getByText("1 · Ảnh bìa")).toBeTruthy();
    const lost = tiles()[2] ?? document.body;
    expect(within(lost).getByText("Ảnh bị mất")).toBeTruthy();
    expect(within(lost).getByText("Kho tệp không còn ảnh này. Tải lại ảnh.")).toBeTruthy();
    expect(within(lost).getByRole("button", { name: "Tải lại" })).toBeTruthy();
    expect(within(lost).queryByRole("button", { name: "Ảnh gốc" })).toBeNull();
    expect(screen.getByRole("button", { name: "Dời ảnh 1 sang trái" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Dời ảnh 2 sang phải" }).hasAttribute("disabled")).toBe(false);
    const sel = screen.getByRole("combobox", { name: "Màu của ảnh 1" });
    expect(sel instanceof HTMLSelectElement && sel.value).toBe("Đen");
    expect(screen.getByText("Kéo ảnh vào đây")).toBeTruthy();
  });

  it("không có catalog.manage → không nút, không vùng tải lên; không files.upload → không vùng tải lên", () => {
    renderManager({ canManage: false });
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByText("Kéo ảnh vào đây")).toBeNull();
    cleanup();
    renderManager({ canUpload: false });
    expect(screen.queryByText("Kéo ảnh vào đây")).toBeNull();
    expect(screen.getByRole("button", { name: "Xóa ảnh 1" })).toBeTruthy();
  });

  it("chưa có ảnh → 0 ảnh, không lưới", () => {
    images = [];
    renderManager();
    expect(screen.getByText("0 ảnh")).toBeTruthy();
    expect(screen.queryAllByTestId("img-tile")).toEqual([]);
  });

  it("chọn tệp → uploads.add; thả tệp vào vùng tải lên → uploads.add", () => {
    const uploads = fakeUploads();
    renderManager({ uploads });
    const f = new File(["x"], "a.jpg", { type: "image/jpeg" });
    fireEvent.change(screen.getByLabelText("Chọn ảnh từ máy"), { target: { files: [f] } });
    expect(uploads.add).toHaveBeenCalledWith([f]);
    fireEvent.drop(screen.getByTestId("img-drop"), { dataTransfer: { files: [f], types: ["Files"] } });
    expect(uploads.add).toHaveBeenCalledTimes(2);
  });

  it("dòng đang tải lên, đang xử lý, bị từ chối (Bỏ)", () => {
    const uploads = fakeUploads([
      { key: "k1", name: "IMG_2040.jpg", phase: "processing" },
      { key: "k2", name: "bang-gia.jpg", phase: "failed", message: "Tệp không phải ảnh" },
    ]);
    renderManager({ uploads });
    expect(screen.getByText("Đang tải lên (chưa thuộc mẫu)")).toBeTruthy();
    expect(screen.getByText("Đang xử lý ảnh, thường dưới 30 giây. Xong thì ảnh tự vào cuối lưới.")).toBeTruthy();
    expect(screen.getByText("Tệp không phải ảnh")).toBeTruthy();
    expect(screen.getByText("bang-gia.jpg · không được thêm vào mẫu")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Bỏ" }));
    expect(uploads.dismiss).toHaveBeenCalledWith("k2");
  });
});

describe("ImageManager — ghi", () => {
  it("dời ảnh 1 sang phải → reorderProductImages theo version; version mới = cũ + 1 → nhận", async () => {
    const after = [images[1], images[0], images[2]].filter((x) => x !== undefined);
    vi.mocked(api.reorderProductImages).mockImplementation(() => ok(product(after, 8)));
    const { props } = renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Dời ảnh 1 sang phải" }));
    await waitFor(() => {
      expect(props.onProduct).toHaveBeenCalledWith(product(after, 8), true);
    });
    const call = vi.mocked(api.reorderProductImages).mock.calls[0]?.[0];
    expect(call?.body).toEqual({ productVersion: 7, imageIds: [id(2), id(1), id(3)] });
    expect(call?.path).toEqual({ id: PID });
    expect(call?.headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(props.onPending).toHaveBeenCalledWith(true);
    // onProduct chạy trong thân thao tác; pending chỉ giảm ở .finally của hàng đợi rồi effect mới báo onPending(false),
    // nên phải chờ (kiểm ngay sau khi thấy onProduct thì đỏ chập chờn).
    await waitFor(() => {
      expect(props.onPending).toHaveBeenLastCalledWith(false);
    });
  });

  it("người khác vừa sửa (version_conflict) → câu Mẫu đã thay đổi", async () => {
    vi.mocked(api.reorderProductImages).mockImplementation(() => fail(409, "version_conflict"));
    const { props } = renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Dời ảnh 2 sang trái" }));
    expect(await screen.findByText("Mẫu đã thay đổi, tải lại.")).toBeTruthy();
    expect(props.onProduct).not.toHaveBeenCalled();
  });

  it("xóa → ảnh biến mất, thông báo Hoàn tác (10) đếm lùi; Hoàn tác → restoreProductImage", async () => {
    const left = [images[0], images[2]].filter((x) => x !== undefined);
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok(product(left, 8)));
    vi.mocked(api.restoreProductImage).mockImplementation(() => ok(product(images, 9)));
    const { props } = renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 2" }));
    expect(await screen.findByText("Đã xóa ảnh 2. Khôi phục được trong 30 ngày.")).toBeTruthy();
    expect(props.onProduct).toHaveBeenCalledWith(product(left, 8), true);
    expect(vi.mocked(api.deleteProductImage).mock.calls[0]?.[0].path).toEqual({ id: PID, imageId: id(2) });
    fireEvent.click(screen.getByRole("button", { name: "Hoàn tác (10)" }));
    await waitFor(() => {
      expect(api.restoreProductImage).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.restoreProductImage).mock.calls[0]?.[0].path).toEqual({ id: PID, imageId: id(2) });
    await waitFor(() => {
      expect(screen.queryByText("Đã xóa ảnh 2. Khôi phục được trong 30 ngày.")).toBeNull();
    });
  });

  it("thông báo xóa tự tắt sau 10 giây", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok(product([], 8)));
    renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 1" }));
    await screen.findByRole("button", { name: "Hoàn tác (10)" });
    await act(() => Promise.resolve()); // chạy effect bật bộ đếm
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByRole("button", { name: "Hoàn tác (7)" })).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(7100);
    });
    expect(screen.queryByRole("button", { name: /^Hoàn tác/ })).toBeNull();
  });

  it("xóa mà version nhảy hơn 1 (người khác vừa sửa) → không nhận version", async () => {
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok(product([], 10)));
    const { props } = renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 1" }));
    await waitFor(() => {
      expect(props.onProduct).toHaveBeenCalledWith(product([], 10), false);
    });
  });

  it("đổi màu ảnh → setProductImageColor; Chưa gắn → không gửi color", async () => {
    vi.mocked(api.setProductImageColor).mockImplementation(() => ok(product(images, 8)));
    const { props } = renderManager();
    fireEvent.change(screen.getByRole("combobox", { name: "Màu của ảnh 1" }), { target: { value: "Be" } });
    await waitFor(() => {
      expect(props.onProduct).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.setProductImageColor).mock.calls[0]?.[0].body).toEqual({ productVersion: 7, color: "Be" });
    expect(vi.mocked(api.setProductImageColor).mock.calls[0]?.[0].path).toEqual({ id: PID, imageId: id(1) });
    fireEvent.change(screen.getByRole("combobox", { name: "Màu của ảnh 2" }), { target: { value: "" } });
    await waitFor(() => {
      expect(props.onProduct).toHaveBeenCalledTimes(2);
    });
    expect(vi.mocked(api.setProductImageColor).mock.calls[1]?.[0].body).toEqual({ productVersion: 8 });
  });

  it("ảnh mang màu không còn biến thể → hiện màu cũ, ghi rõ", () => {
    images = [img(1, { color: "Kem" })];
    renderManager();
    const sel = screen.getByRole("combobox", { name: "Màu của ảnh 1" });
    expect(sel instanceof HTMLSelectElement && sel.value).toBe("Kem");
    expect(screen.getByRole("option", { name: "Màu: Kem (không còn biến thể)" })).toBeTruthy();
  });

  it("Ảnh gốc → getOriginalImageUrl rồi tải đường dẫn ký sẵn", async () => {
    vi.mocked(api.getOriginalImageUrl).mockImplementation(() =>
      ok({ url: "https://kho.test/orig/abc?sig=1", expiresAt: "2026-11-18T10:05:00Z" }),
    );
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    renderManager();
    fireEvent.click(within(tiles()[1] ?? document.body).getByRole("button", { name: "Ảnh gốc" }));
    await waitFor(() => {
      expect(click).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.getOriginalImageUrl).mock.calls[0]?.[0].path).toEqual({ id: PID, imageId: id(2) });
    const a = click.mock.contexts[0];
    expect(a instanceof HTMLAnchorElement && a.href).toBe("https://kho.test/orig/abc?sig=1");
    click.mockRestore();
  });

  it("ảnh vừa xử lý xong → attachProductImage theo version rồi bỏ dòng tải lên; đang lưu mẫu thì chờ", async () => {
    const ready: UploadItem = { key: "k1", name: "a.jpg", phase: "ready", fileId: fid(9) };
    const after = [...images, img(9)];
    vi.mocked(api.attachProductImage).mockImplementation(() => ok(product(after, 8), 201));
    const uploads = fakeUploads([ready]);
    const { props, rerender } = renderManager({ uploads, busy: true });
    await act(() => Promise.resolve());
    expect(api.attachProductImage).not.toHaveBeenCalled();
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <ImageManager {...props} busy={false} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(uploads.dismiss).toHaveBeenCalledWith("k1");
    });
    expect(vi.mocked(api.attachProductImage).mock.calls[0]?.[0].body).toEqual({ productVersion: 7, fileId: fid(9) });
    expect(uploads.update).toHaveBeenCalledWith("k1", { phase: "attaching" });
    expect(props.onProduct).toHaveBeenCalledWith(product(after, 8), true);
  });

  it("Tải lại ảnh mất: gắn ảnh mới cùng màu, dời vào chỗ ảnh mất, rồi xóa ảnh mất", async () => {
    const ready: UploadItem = {
      key: "k1",
      name: "moi.jpg",
      phase: "ready",
      fileId: fid(9),
      replaces: { imageId: id(3), color: "Đen" },
    };
    const attached = [...images, img(9, { color: "Đen" })];
    const ordered = [attached[0], attached[1], attached[3], attached[2]].filter((x) => x !== undefined);
    const final = [attached[0], attached[1], attached[3]].filter((x) => x !== undefined);
    vi.mocked(api.attachProductImage).mockImplementation(() => ok(product(attached, 8), 201));
    vi.mocked(api.reorderProductImages).mockImplementation(() => ok(product(ordered, 9)));
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok(product(final, 10)));
    const uploads = fakeUploads([ready]);
    const { props } = renderManager({ uploads });
    await waitFor(() => {
      expect(api.deleteProductImage).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.attachProductImage).mock.calls[0]?.[0].body).toEqual({
      productVersion: 7,
      fileId: fid(9),
      color: "Đen",
    });
    expect(vi.mocked(api.reorderProductImages).mock.calls[0]?.[0].body).toEqual({
      productVersion: 8,
      imageIds: [id(1), id(2), id(9), id(3)],
    });
    expect(vi.mocked(api.deleteProductImage).mock.calls[0]?.[0].path).toEqual({ id: PID, imageId: id(3) });
    await waitFor(() => {
      expect(props.onProduct).toHaveBeenLastCalledWith(product(final, 10), true);
    });
  });

  it("chọn tệp từ nút Tải lại → uploads.add kèm ảnh cần thay", () => {
    const uploads = fakeUploads();
    renderManager({ uploads });
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    const f = new File(["x"], "moi.jpg", { type: "image/jpeg" });
    fireEvent.change(screen.getByLabelText("Chọn ảnh thay ảnh bị mất"), { target: { files: [f] } });
    expect(uploads.add).toHaveBeenCalledWith([f], { imageId: id(3), color: "Đen" });
  });

  it("đang lưu mẫu (busy) → dời, xóa, đổi màu, kéo thả không gửi gì (tránh xung đột với các bước lưu)", async () => {
    renderManager({ busy: true });
    fireEvent.click(screen.getByRole("button", { name: "Dời ảnh 1 sang phải" }));
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 1" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Màu của ảnh 1" }), { target: { value: "Be" } });
    const [a, b] = tiles();
    expect(a?.getAttribute("draggable")).toBe("false");
    if (a !== undefined && b !== undefined) {
      fireEvent.dragStart(a, { dataTransfer: { effectAllowed: "" } });
      fireEvent.drop(b, { dataTransfer: { files: [] } });
    }
    await act(() => Promise.resolve());
    expect(api.reorderProductImages).not.toHaveBeenCalled();
    expect(api.deleteProductImage).not.toHaveBeenCalled();
    expect(api.setProductImageColor).not.toHaveBeenCalled();
  });

  it("gắn lại tệp đã gắn (already_exists) → bỏ dòng, báo ảnh đã có, không báo người khác sửa", async () => {
    vi.mocked(api.attachProductImage).mockImplementation(() => fail(409, "already_exists"));
    const uploads = fakeUploads([{ key: "k1", name: "a.jpg", phase: "ready", fileId: fid(9) }]);
    const onConflict = vi.fn();
    renderManager({ uploads, onConflict });
    expect(await screen.findByText("Ảnh này đã có trong mẫu. Tải lại trang để thấy.")).toBeTruthy();
    expect(uploads.dismiss).toHaveBeenCalledWith("k1");
    expect(onConflict).not.toHaveBeenCalled();
  });

  it("gắn lỗi version_conflict → dòng báo lỗi có nút Gắn lại", async () => {
    vi.mocked(api.attachProductImage).mockImplementation(() => fail(409, "version_conflict"));
    const uploads = fakeUploads([{ key: "k1", name: "a.jpg", phase: "ready", fileId: fid(9) }]);
    renderManager({ uploads });
    await waitFor(() => {
      expect(uploads.update).toHaveBeenCalledWith("k1", {
        phase: "failed",
        message: "Mẫu đã thay đổi, tải lại.",
        retryAttach: true,
      });
    });
  });
});
