// @vitest-environment jsdom
// Hàng đợi quét của mở sổ (Task 273, HV03, HV05): mỗi lần quét một yêu cầu +1 có khóa riêng; mất mạng thì giữ trong
// hàng đợi (cả khi tải lại trang) và gửi lại ĐÚNG khóa cũ; máy chủ giả đếm theo khóa nên gửi lại không đếm trùng.
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { StocktakeIncrementResult } from "../../../api";
import { ApiError, NetworkError } from "../../../app/errors";
import { useScanQueue, type ScanBody } from "./useScanQueue";

const ST = "0195f3b2-0000-7000-8000-0000000c0007";
const V = "0195f3b2-0000-7000-8000-0000000b0015";

/** Máy chủ giả: đếm mỗi khóa đúng một lần; `failAfterApply` ném lỗi mạng SAU khi đã đếm (mất câu trả lời). */
function fakeServer() {
  const seen = new Map<string, StocktakeIncrementResult>();
  const calls: { key: string; body: ScanBody }[] = [];
  let total = 0;
  const s = {
    calls,
    failAfterApply: new Set<number>(),
    failBefore: new Set<number>(),
    reply: null as null | (() => never),
    get total() {
      return total;
    },
    send: (key: string, body: ScanBody): Promise<StocktakeIncrementResult> => {
      calls.push({ key, body });
      const n = calls.length;
      if (s.reply !== null) s.reply();
      if (s.failBefore.has(n)) return Promise.reject(new NetworkError());
      let r = seen.get(key);
      if (r === undefined) {
        total += body.by;
        r = { variantId: V, counted: total };
        seen.set(key, r);
      }
      if (s.failAfterApply.has(n)) return Promise.reject(new NetworkError());
      return Promise.resolve(r);
    },
  };
  return s;
}

const opts = (server: ReturnType<typeof fakeServer>) => ({
  stocktakeId: ST,
  send: server.send,
  retryMs: 5,
  newKey: (() => {
    let i = 0;
    return () => `key-${String(++i)}`;
  })(),
});

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  localStorage.clear();
});

async function scanAThree(server: ReturnType<typeof fakeServer>) {
  const { result } = renderHook(() => useScanQueue(opts(server)));
  act(() => {
    for (let i = 0; i < 3; i++) result.current.enqueue("A", 1);
  });
  await waitFor(() => {
    expect(result.current.items.every((i) => i.status === "saved")).toBe(true);
  });
  return result;
}

/** Quét một mã với phản hồi giả `reply`, chờ tới khi lần quét có trạng thái `status`. */
async function scanOneAs(server: ReturnType<typeof fakeServer>, code: string, status: "saved" | "error") {
  const { result } = renderHook(() => useScanQueue(opts(server)));
  act(() => {
    result.current.enqueue(code, 1);
  });
  await waitFor(() => {
    expect(result.current.items[0]?.status).toBe(status);
  });
  return result;
}

/** Máy chủ trả lần lượt các lỗi `errors` rồi thành công; một lần quét phải đi hết bằng MỘT khóa. */
async function scanAfterErrors(errors: ApiError[]) {
  const server = fakeServer();
  let n = 0;
  server.reply = () => {
    const e = errors[n++];
    if (e !== undefined) throw e;
    return undefined as never;
  };
  await scanOneAs(server, "A", "saved");
  expect(new Set(server.calls.map((c) => c.key)).size).toBe(1);
  return server;
}

describe("useScanQueue", () => {
  it("quét 3 lần mã A → 3 yêu cầu +1 với 3 khóa khác nhau, máy chủ đếm 3", async () => {
    const server = fakeServer();
    const result = await scanAThree(server);
    expect(server.calls.map((c) => c.body)).toEqual([
      { stocktakeId: ST, code: "A", by: 1 },
      { stocktakeId: ST, code: "A", by: 1 },
      { stocktakeId: ST, code: "A", by: 1 },
    ]);
    expect(new Set(server.calls.map((c) => c.key)).size).toBe(3);
    expect(server.total).toBe(3);
    expect(result.current.items[0]?.result?.counted).toBe(3);
  });

  it("mất mạng giữa lần 2 (chưa tới máy chủ): chờ gửi, rồi gửi lại cùng khóa, tổng đúng 3", async () => {
    const server = fakeServer();
    server.failBefore.add(2);
    await scanAThree(server);
    const second = server.calls.filter((c) => c.key === "key-2");
    expect(second).toHaveLength(2);
    expect(server.calls).toHaveLength(4);
    expect(server.total).toBe(3);
  });

  it("máy chủ đã đếm nhưng mất câu trả lời: gửi lại cùng khóa, không đếm trùng", async () => {
    const server = fakeServer();
    server.failAfterApply.add(2);
    await scanAThree(server);
    expect(server.total).toBe(3);
  });

  it("đang chờ gửi thì báo số lần chờ; đã lưu thì về 0", async () => {
    const server = fakeServer();
    server.failBefore.add(1);
    const { result } = renderHook(() => useScanQueue(opts(server)));
    act(() => {
      result.current.enqueue("A", 1);
    });
    await waitFor(() => {
      expect(result.current.waiting).toBe(1);
    });
    await waitFor(() => {
      expect(result.current.waiting).toBe(0);
    });
  });

  it("lỗi dứt khoát (422 unknown_code): không gửi lại, báo lỗi, mã lỗi kèm theo", async () => {
    const server = fakeServer();
    server.reply = () => {
      throw new ApiError({ status: 422, code: "unknown_code", details: {} });
    };
    const result = await scanOneAs(server, "9999", "error");
    expect(result.current.items[0]?.errorCode).toBe("unknown_code");
    expect(result.current.items[0]?.message).toBe("Không tìm thấy mã hàng này");
    await new Promise((r) => setTimeout(r, 30));
    expect(server.calls).toHaveLength(1);
  });

  it("mã trùng hai người bán (422 ambiguous_code): giữ ứng viên để chọn", async () => {
    const server = fakeServer();
    const cands = [
      { variantId: "v1", sellerId: "s1", sku: "AO-1" },
      { variantId: "v2", sellerId: "s2", sku: "MAY-AO-1" },
    ];
    server.reply = () => {
      throw new ApiError({ status: 422, code: "ambiguous_code", details: { candidates: cands } });
    };
    const result = await scanOneAs(server, "893", "error");
    expect(result.current.items[0]?.candidates).toEqual(cands);
  });

  it("tải lại trang: lần quét chưa gửi được còn trong hàng đợi và gửi lại đúng khóa cũ", async () => {
    const server = fakeServer();
    server.failBefore.add(1);
    const first = renderHook(() => useScanQueue({ ...opts(server), retryMs: 60_000 }));
    act(() => {
      first.result.current.enqueue("A", 1);
    });
    await waitFor(() => {
      expect(first.result.current.waiting).toBe(1);
    });
    first.unmount();
    const second = renderHook(() => useScanQueue(opts(server)));
    expect(second.result.current.waiting).toBe(1);
    await waitFor(() => {
      expect(second.result.current.waiting).toBe(0);
    });
    expect(server.calls.map((c) => c.key)).toEqual(["key-1", "key-1"]);
    expect(server.total).toBe(1);
  });

  it("503 retry_later và 409 request_in_progress coi là chưa rõ: giữ khóa, gửi lại", async () => {
    const server = await scanAfterErrors([
      new ApiError({ status: 503, code: "retry_later", details: {} }),
      new ApiError({ status: 409, code: "request_in_progress", details: {} }),
    ]);
    expect(server.calls).toHaveLength(3);
  });

  it("429 rate_limited: máy chủ chưa làm gì → giữ khóa, gửi lại, không mất lần quét", async () => {
    const server = await scanAfterErrors([new ApiError({ status: 429, code: "rate_limited", details: {} })]);
    expect(server.total).toBe(1);
  });

  it("quét hàng trăm mã: danh sách chỉ giữ 50 lần đã lưu mới nhất, không bỏ lần chưa xong", async () => {
    const server = fakeServer();
    const { result } = renderHook(() => useScanQueue(opts(server)));
    act(() => {
      for (let i = 0; i < 120; i++) result.current.enqueue("A", 1);
    });
    await waitFor(() => {
      expect(server.total).toBe(120);
    });
    await waitFor(() => {
      expect(result.current.waiting).toBe(0);
    });
    expect(result.current.items).toHaveLength(50);
    expect(result.current.items[0]?.result?.counted).toBe(120);
  });
});
