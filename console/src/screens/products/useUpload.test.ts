// @vitest-environment jsdom
// Tải ảnh thẳng lên kho (Task 325): createUpload → PUT lên putUrl kèm headers → completeUpload → chờ ảnh xử lý xong
// (getFile mỗi 2 s, tối đa 60 s) → sẵn sàng gắn. Ảnh bị từ chối hiện câu theo lý do.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import type { FileInfo, Upload } from "../../api";
import { NetworkError } from "../../app/errors";
import { fail, ok } from "./productsTestkit";
import {
  useUploads,
  type PutFile,
  type Replace,
  type UploadItem,
  type UploadOptions,
  type UploadPhase,
} from "./useUpload";

vi.mock("../../api", async (orig) => ({
  ...(await orig<typeof import("../../api")>()),
  createUpload: vi.fn(),
  completeUpload: vi.fn(),
  getFile: vi.fn(),
}));

const fileId = (name: string) =>
  `0195f3b2-0000-7000-8000-${name.padEnd(12, "0").slice(0, 12)}`.replace(/[^0-9a-f-]/g, "a");
const info = (id: string, status: FileInfo["status"], rejectReason?: string): FileInfo => ({
  id,
  purpose: "product_image",
  status,
  variants: [],
  ...(rejectReason === undefined ? {} : { rejectReason }),
});

/** Máy chủ giả: tên tệp → các trạng thái lần lượt của getFile (phần tử đầu là kết quả completeUpload). */
function fakeServer(states: Record<string, FileInfo["status"][]>, reasons: Record<string, string> = {}) {
  const byId = new Map<string, string>();
  const seen = new Map<string, number>();
  const next = (id: string) => {
    const name = byId.get(id) ?? "";
    const list = states[name] ?? ["ready"];
    const n = seen.get(id) ?? 0;
    seen.set(id, n + 1);
    const st = list[Math.min(n, list.length - 1)] ?? "ready";
    return info(id, st, st === "rejected" ? reasons[name] : undefined);
  };
  vi.mocked(api.createUpload).mockImplementation((o) => {
    const id = fileId(o.body.fileName);
    byId.set(id, o.body.fileName);
    const up: Upload = {
      fileId: id,
      putUrl: `https://kho.test/tmp/${id}?sig=1`,
      headers: { "Content-Type": o.body.contentType },
      expiresAt: "2026-11-18T10:15:00Z",
    };
    return ok(up, 201);
  });
  vi.mocked(api.completeUpload).mockImplementation((o) => ok(next(o.path.id)));
  vi.mocked(api.getFile).mockImplementation((o) => ok(next(o.path.id)));
}

function setup(opts: UploadOptions) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
  return renderHook(() => useUploads(opts), { wrapper });
}

const jpg = (name: string, size = 1000) => new File([new Uint8Array(size)], name, { type: "image/jpeg" });
const instant = () => Promise.resolve();
const putOk: PutFile = () => Promise.resolve();

/** Dựng hook, thêm một tệp name, chờ dòng của nó tới phase; trả hàm đọc dòng đó và kết quả hook. */
async function uploadOneWith(opts: UploadOptions, name: string, phase: UploadPhase, replaces?: Replace) {
  const { result } = setup(opts);
  act(() => {
    if (replaces === undefined) result.current.add([jpg(name)]);
    else result.current.add([jpg(name)], replaces);
  });
  const item = (): UploadItem => {
    const it = result.current.items[0];
    if (it === undefined) throw new Error("không có dòng tải lên");
    return it;
  };
  await waitFor(() => {
    expect(item().phase).toBe(phase);
  });
  return { item, result };
}

const uploadOne = async (opts: UploadOptions, name: string, phase: UploadPhase, replaces?: Replace) =>
  (await uploadOneWith(opts, name, phase, replaces)).item;

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("useUploads", () => {
  it("3 ảnh, một ảnh là HTML đổi đuôi → 2 sẵn sàng gắn, 1 báo Tệp không phải ảnh", async () => {
    fakeServer(
      { "a.jpg": ["processing", "ready"], "html-as.jpg": ["processing", "rejected"], "c.jpg": ["ready"] },
      { "html-as.jpg": "not_image" },
    );
    const put = vi.fn<PutFile>(() => Promise.resolve());
    const sleep = vi.fn(() => Promise.resolve());
    const { result } = setup({ put, sleep });
    act(() => {
      result.current.add([jpg("a.jpg"), jpg("html-as.jpg"), jpg("c.jpg")]);
    });
    expect(result.current.items.map((i) => i.name)).toEqual(["a.jpg", "html-as.jpg", "c.jpg"]);
    await waitFor(() => {
      expect(result.current.items.every((i) => i.phase === "ready" || i.phase === "failed")).toBe(true);
    });
    const [a, h, c] = result.current.items;
    expect(a?.phase).toBe("ready");
    expect(a?.fileId).toBe(fileId("a.jpg"));
    expect(c?.phase).toBe("ready");
    expect(h?.phase).toBe("failed");
    expect(h?.message).toBe("Tệp không phải ảnh");

    expect(api.createUpload).toHaveBeenCalledTimes(3);
    const call = vi.mocked(api.createUpload).mock.calls[0]?.[0];
    expect(call?.body).toEqual({
      purpose: "product_image",
      fileName: "a.jpg",
      contentType: "image/jpeg",
      sizeBytes: 1000,
    });
    expect(call?.headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(put).toHaveBeenCalledTimes(3);
    expect(put.mock.calls[0]?.[0]).toBe(`https://kho.test/tmp/${fileId("a.jpg")}?sig=1`);
    expect(put.mock.calls[0]?.[1]).toEqual({ "Content-Type": "image/jpeg" });
    expect(vi.mocked(api.completeUpload).mock.calls[0]?.[0].headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(sleep).toHaveBeenCalledWith(2000, expect.anything());
  });

  it("tùy chọn purpose (ảnh banner, Task 459) gửi đúng mục đích; add trả mã dòng của từng tệp", async () => {
    fakeServer({ "b.jpg": ["ready"] });
    const { result } = setup({ put: putOk, sleep: instant, purpose: "web_banner" });
    let keys: string[] = [];
    act(() => {
      keys = result.current.add([jpg("b.jpg")]);
    });
    expect(keys).toEqual(result.current.items.map((i) => i.key));
    await waitFor(() => {
      expect(result.current.items[0]?.phase).toBe("ready");
    });
    expect(vi.mocked(api.createUpload).mock.calls[0]?.[0].body.purpose).toBe("web_banner");
  });

  it("máy chủ không nhận loại, cỡ tệp → báo ngay, không PUT", async () => {
    vi.mocked(api.createUpload).mockImplementation(() => fail(400, "file_too_large"));
    const put = vi.fn<PutFile>(() => Promise.resolve());
    const item = await uploadOne({ put, sleep: instant }, "to.jpg", "failed");
    expect(item().message).toBe("Ảnh quá 20 MB");
    expect(put).not.toHaveBeenCalled();
  });

  it("PUT rớt mạng → gửi lại (tối đa 3 lần); vẫn rớt → Tải lên chưa trọn", async () => {
    fakeServer({});
    let n = 0;
    const put = vi.fn<PutFile>(() => {
      n++;
      return n <= 2 ? Promise.reject(new NetworkError()) : Promise.resolve();
    });
    await uploadOne({ put, sleep: instant }, "a.jpg", "ready");
    expect(put).toHaveBeenCalledTimes(3);

    const bad = vi.fn<PutFile>(() => Promise.reject(new NetworkError()));
    const item = await uploadOne({ put: bad, sleep: instant }, "b.jpg", "failed");
    expect(item().message).toBe("Tải lên chưa trọn, chọn lại ảnh");
    expect(bad).toHaveBeenCalledTimes(4);
    expect(api.completeUpload).toHaveBeenCalledTimes(1);
  });

  it("xử lý quá 60 s → Xử lý ảnh không xong; Bỏ xóa dòng", async () => {
    fakeServer({ "a.jpg": ["processing"] });
    let t = 0;
    const sleep = vi.fn((ms: number) => {
      t += ms;
      return Promise.resolve();
    });
    const { item, result } = await uploadOneWith({ put: putOk, sleep, now: () => t }, "a.jpg", "failed");
    expect(item().message).toBe("Xử lý ảnh không xong, chọn lại ảnh");
    expect(vi.mocked(api.getFile).mock.calls.length).toBeGreaterThanOrEqual(29);
    expect(vi.mocked(api.getFile).mock.calls.length).toBeLessThanOrEqual(31);
    act(() => {
      result.current.dismiss(item().key);
    });
    expect(result.current.items).toEqual([]);
  });

  it("ảnh thay ảnh mất mang theo mã ảnh và màu cần thay", async () => {
    fakeServer({});
    const item = await uploadOne({ put: putOk, sleep: instant }, "moi.jpg", "ready", {
      imageId: "lost-1",
      color: "Đen",
    });
    expect(item().replaces).toEqual({ imageId: "lost-1", color: "Đen" });
  });
});
