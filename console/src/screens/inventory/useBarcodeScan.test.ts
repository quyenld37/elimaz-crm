// @vitest-environment jsdom
// Hook quét mã của màn Kiểm kho (kế hoạch M1 Task 329): chuỗi từ máy quét dạng bàn phím (kết thúc bằng Enter) hoặc ô
// nhập; quét cùng mã cộng 1 vào số đếm cục bộ; gửi recordStocktakeCount (số tuyệt đối) sau 800 ms không quét thêm.
import { act, renderHook } from "@testing-library/react";
import type { KeyboardEvent } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StocktakeLine, VariantSearchHit } from "../../api";
import { ApiError, NetworkError } from "../../app/errors";
import { useBarcodeScan, type SaveBody, type ScanOptions } from "./useBarcodeScan";

const ST = "0195f3b2-0000-7000-8000-0000000c0007";

function hit(n: number, barcode: string, sku: string): VariantSearchHit {
  return {
    variantId: `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`,
    productId: "p",
    sku,
    barcode,
    productCode: sku.split("-")[0] ?? sku,
    productName: "Đầm suông công sở",
    color: "Đen",
    size: "S",
    price: 1,
    sellable: true,
  };
}

const DAM = hit(15, "2000000000015", "DAM0123-DEN-S");
const QU = hit(213, "2000000000213", "QU0789-DEN-M");
const CATALOG = [DAM, QU];

function serverLine(variantId: string, counted: number, version: number): StocktakeLine {
  return {
    variantId,
    expectedOnShelf: 7,
    counted,
    delta: counted - 7,
    countedAt: "2026-11-18T09:00:00Z",
    version,
    onHand: 10,
    held: 0,
    inTransit: 3,
    damaged: 0,
    available: 7,
    availableAfter: counted,
  };
}

let lookups: string[];
let saves: { key: string; body: SaveBody }[];
let saveImpl: (key: string, body: SaveBody) => Promise<StocktakeLine>;
let versions: Map<string, number>;

beforeEach(() => {
  vi.useFakeTimers();
  lookups = [];
  saves = [];
  versions = new Map();
  saveImpl = (_key, body) => {
    const v = (versions.get(body.variantId) ?? 0) + 1;
    versions.set(body.variantId, v);
    return Promise.resolve(serverLine(body.variantId, body.counted, v));
  };
});

afterEach(() => {
  vi.useRealTimers();
});

let keySeq = 0;

function setup(extra: Partial<ScanOptions> = {}) {
  const opts: ScanOptions = {
    stocktakeId: ST,
    lines: [],
    lookup: (code) => {
      lookups.push(code);
      return Promise.resolve(CATALOG.find((h) => h.barcode === code || h.sku === code.toUpperCase()) ?? null);
    },
    save: (key, body) => {
      saves.push({ key, body });
      return saveImpl(key, body);
    },
    reload: () => Promise.resolve([]),
    writeOptions: {
      sleep: () => Promise.resolve(),
      newKey: () => `k${String(++keySeq)}`,
    },
    ...extra,
  };
  return renderHook((p: ScanOptions) => useBarcodeScan(p), { initialProps: opts });
}

const conflict = () => Promise.reject(new ApiError({ status: 409, code: "version_conflict", details: {} }));

/** Quét DAM n lần liền rồi chờ qua mốc lưu. */
async function scanDam(result: { current: ReturnType<typeof useBarcodeScan> }, n: number) {
  act(() => {
    for (let i = 0; i < n; i++) result.current.scan("2000000000015");
  });
  await act(() => vi.advanceTimersByTimeAsync(800));
}

const flushMicro = () => act(() => vi.advanceTimersByTimeAsync(0));

describe("useBarcodeScan", () => {
  it("quét 2000000000015 bảy lần liền → tra một lần, số đếm 7, gửi một lần số tuyệt đối 7 sau 800 ms", async () => {
    const { result } = setup();
    act(() => {
      for (let i = 0; i < 7; i++) result.current.scan("2000000000015");
    });
    await flushMicro();
    expect(lookups).toEqual(["2000000000015"]);
    expect(result.current.entries[0]?.counted).toBe(7);
    expect(result.current.entries[0]?.status).toBe("pending");
    expect(result.current.last).toEqual({ code: "2000000000015", variantId: DAM.variantId });
    await act(() => vi.advanceTimersByTimeAsync(799));
    expect(saves).toHaveLength(0);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(saves.map((s) => s.body)).toEqual([{ stocktakeId: ST, variantId: DAM.variantId, counted: 7 }]);
    expect(result.current.entries[0]?.status).toBe("saved");
    expect(result.current.entries[0]?.line?.version).toBe(1);
  });

  it("mỗi lần quét thêm dời mốc 800 ms; lần lưu sau gửi lineVersion của dòng vừa lưu", async () => {
    const { result } = setup();
    act(() => {
      result.current.scan("2000000000015");
    });
    await act(() => vi.advanceTimersByTimeAsync(500));
    act(() => {
      result.current.scan("2000000000015");
    });
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(saves).toHaveLength(0);
    await act(() => vi.advanceTimersByTimeAsync(300));
    expect(saves.map((s) => s.body.counted)).toEqual([2]);
    act(() => {
      result.current.scan("2000000000015");
    });
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves[1]?.body).toEqual({ stocktakeId: ST, variantId: DAM.variantId, counted: 3, lineVersion: 1 });
  });

  it("máy quét dạng bàn phím: gõ chuỗi rồi Enter vào ô nhập → quét, ô nhập trống lại", async () => {
    const { result } = setup();
    act(() => {
      result.current.input.onChange({ target: { value: "qu0789-den-m" } } as never);
    });
    expect(result.current.input.value).toBe("qu0789-den-m");
    const enter = { key: "Enter", preventDefault: vi.fn() } as unknown as KeyboardEvent<HTMLInputElement>;
    act(() => {
      result.current.input.onKeyDown(enter);
    });
    await flushMicro();
    expect(result.current.input.value).toBe("");
    expect(result.current.entries.map((e) => [e.variantId, e.counted])).toEqual([[QU.variantId, 1]]);
  });

  it("gõ số vào ô (số tuyệt đối) và nút ± đổi số đếm, cũng lưu sau 800 ms; không xuống dưới 0", async () => {
    const { result } = setup();
    act(() => {
      result.current.scan("2000000000015");
    });
    await flushMicro();
    act(() => {
      result.current.setCount(DAM.variantId, 12);
      result.current.add(DAM.variantId, -1);
    });
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves.map((s) => s.body.counted)).toEqual([11]);
    act(() => {
      result.current.setCount(DAM.variantId, 0);
      result.current.add(DAM.variantId, -1);
    });
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves.map((s) => s.body.counted)).toEqual([11, 0]);
  });

  it("mã không có trong danh mục → báo không thấy, không gửi; quét mã đúng sau đó xóa báo", async () => {
    const { result } = setup();
    act(() => {
      result.current.scan("2000000009999");
    });
    await flushMicro();
    expect(result.current.notFound).toBe("2000000009999");
    expect(result.current.entries).toEqual([]);
    act(() => {
      result.current.scan("2000000000015");
    });
    await flushMicro();
    expect(result.current.notFound).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves).toHaveLength(1);
  });

  it("hai mã chờ lưu cùng lúc → gửi lần lượt, mỗi mã một yêu cầu", async () => {
    let release: (() => void) | null = null;
    const first = saveImpl;
    saveImpl = (key, body) =>
      new Promise((resolve) => {
        release = () => {
          void first(key, body).then(resolve);
        };
      });
    const { result } = setup();
    act(() => {
      result.current.scan("2000000000015");
      result.current.scan("2000000000213");
    });
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves).toHaveLength(1);
    expect(result.current.entries.find((e) => e.variantId === DAM.variantId)?.status).toBe("saving");
    saveImpl = first;
    await act(async () => {
      release?.();
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(saves.map((s) => s.body.variantId)).toEqual([DAM.variantId, QU.variantId]);
    expect(result.current.entries.every((e) => e.status === "saved")).toBe(true);
  });

  it("lỗi chưa rõ kết quả (mạng) → 'đang thử lại', gửi lại đúng thân với cùng khóa cho tới khi được", async () => {
    let fails = 4; // useWriteAction tự gửi lại 3 lần; lần thứ 4 lỗi tới hook
    const ok = saveImpl;
    saveImpl = (key, body) => (fails-- > 0 ? Promise.reject(new NetworkError()) : ok(key, body));
    const { result } = setup();
    await scanDam(result, 1);
    expect(result.current.entries[0]?.status).toBe("retrying");
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(result.current.entries[0]?.status).toBe("saved");
    expect(new Set(saves.map((s) => s.key)).size).toBe(1);
    expect(new Set(saves.map((s) => JSON.stringify(s.body))).size).toBe(1);
  });

  it("người khác vừa đếm cùng mã (409 version_conflict) → không ghi đè: lấy số trên máy chủ, báo lỗi ở dòng", async () => {
    saveImpl = conflict;
    const reload = vi.fn(() => Promise.resolve([serverLine(DAM.variantId, 5, 4)]));
    const { result } = setup({ reload });
    await scanDam(result, 2);
    expect(reload).toHaveBeenCalledTimes(1);
    const e = result.current.entries[0];
    expect(e?.counted).toBe(5);
    expect(e?.line?.version).toBe(4);
    expect(e?.status).toBe("error");
    expect(e?.message).toBe("Người khác vừa đếm mã này: trên máy chủ là 5. Sửa lại số nếu cần.");
    expect(saves).toHaveLength(1);
  });

  it("409 version_conflict mà máy chủ đã có đúng số của mình (lần trước đã lưu, mất câu trả lời) → coi là đã lưu", async () => {
    saveImpl = conflict;
    const { result } = setup({ reload: () => Promise.resolve([serverLine(DAM.variantId, 1, 2)]) });
    await scanDam(result, 1);
    expect(result.current.entries[0]?.status).toBe("saved");
    expect(result.current.entries[0]?.message).toBeNull();
  });

  it("lỗi dứt khoát khác (phiên đã đóng) → lỗi ở dòng bằng câu tiếng Việt, không tự gửi lại", async () => {
    saveImpl = () => Promise.reject(new ApiError({ status: 409, code: "stocktake_not_open", details: {} }));
    const { result } = setup();
    act(() => {
      result.current.scan("2000000000015");
    });
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(saves).toHaveLength(1);
    expect(result.current.entries[0]?.status).toBe("error");
    expect(result.current.entries[0]?.message).toBe("Phiên kiểm kho đã đóng");
  });

  it("flush gửi ngay số đang chờ và chờ xong; trả true khi mọi dòng đã lưu", async () => {
    const { result } = setup();
    act(() => {
      result.current.scan("2000000000015");
    });
    await flushMicro();
    let done: boolean | undefined;
    await act(async () => {
      done = await result.current.flush();
    });
    expect(done).toBe(true);
    expect(saves).toHaveLength(1);
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(saves).toHaveLength(1);
  });

  it("dòng đã có trên máy chủ (máy khác đếm): hiện sẵn, quét thêm cộng tiếp từ số đó và gửi lineVersion", async () => {
    const { result } = setup({ lines: [serverLine(DAM.variantId, 4, 9)] });
    expect(result.current.entries.map((e) => [e.variantId, e.counted, e.status])).toEqual([
      [DAM.variantId, 4, "saved"],
    ]);
    act(() => {
      result.current.scan("2000000000015");
    });
    await act(() => vi.advanceTimersByTimeAsync(800));
    expect(saves[0]?.body).toEqual({ stocktakeId: ST, variantId: DAM.variantId, counted: 5, lineVersion: 9 });
  });

  it("rời màn trước mốc 800 ms → vẫn gửi số đang chờ một lần (không mất số vừa quét)", async () => {
    const { result, unmount } = setup();
    act(() => {
      result.current.scan("2000000000015");
      result.current.scan("2000000000015");
      result.current.scan("2000000000213");
    });
    await flushMicro();
    unmount();
    expect(saves.map((s) => [s.body.variantId, s.body.counted])).toEqual([
      [DAM.variantId, 2],
      [QU.variantId, 1],
    ]);
    expect(saves.every((s) => s.key !== "")).toBe(true);
    await vi.advanceTimersByTimeAsync(2000);
    expect(saves).toHaveLength(2);
  });

  it("tra mã lỗi mạng → câu lỗi, không cộng", async () => {
    const { result } = setup({ lookup: () => Promise.reject(new NetworkError()) });
    act(() => {
      result.current.scan("2000000000015");
    });
    await flushMicro();
    expect(result.current.lookupError).toMatch(/Mất kết nối/);
    expect(result.current.entries).toEqual([]);
  });
});
