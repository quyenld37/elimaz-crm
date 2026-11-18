// Tải ảnh sản phẩm thẳng lên kho tệp (kế hoạch M1 Task 325; spec 04 mục kho tệp, Task 41–43).
//
// Mỗi tệp: createUpload (xin đường PUT ký sẵn) → PUT thẳng lên putUrl kèm headers (XMLHttpRequest: không phải
// /api/v1, eslint chỉ cho lớp realtime dùng fetch) → completeUpload → chờ ảnh xử lý xong: hỏi getFile mỗi 2 s, báo
// hiệu realtime files.file đánh thức sớm, tối đa 60 s. Ảnh ready thì dòng thành "ready" để khối Ảnh gắn vào mẫu
// (attachProductImage cần version của bản nháp nên ImageManager gắn, không phải hook này).
//
// Hook sống ở trang sửa mẫu, ngoài biểu mẫu: lưu mẫu hay "Tải lại mẫu" dựng lại biểu mẫu nhưng không làm mất ảnh đang
// tải. Tải lên lần lượt từng tệp (createUpload, completeUpload qua useWriteAction: một yêu cầu một lúc); chờ xử lý chạy
// song song.
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { completeUpload, createUpload, getFile, type CreateUploadRequest, type FileInfo } from "../../api";
import { NetworkError, unwrap } from "../../app/errors";
import { abortReason as aborted, defaultSleep, useAbortOnUnmount, useWriteAction } from "../../app/idempotency";
import { TOPICS, queryKey } from "../../app/queryClient";
import {
  IMAGE_PURPOSE,
  rejectText,
  TEXT_PROCESSING_TIMEOUT,
  TEXT_UPLOAD_INCOMPLETE,
  uploadErrorText,
} from "./imageRules";

/** Ảnh mất cần thay: ảnh mới gắn với màu này rồi vào đúng chỗ ảnh mất (ImageManager). */
export interface Replace {
  imageId: string;
  color?: string;
}

/** uploading: đang xin đường, PUT, báo xong; processing: chờ xử lý; ready: chờ gắn; attaching: đang gắn; failed. */
export type UploadPhase = "uploading" | "processing" | "ready" | "attaching" | "failed";

export interface UploadItem {
  key: string;
  /** Tên tệp trên máy (chỉ để hiện). */
  name: string;
  phase: UploadPhase;
  fileId?: string;
  /** Câu lỗi khi failed. */
  message?: string;
  /** failed lúc gắn (tệp đã xử lý xong): bấm "Gắn lại" để gắn lần nữa. */
  retryAttach?: boolean;
  replaces?: Replace;
}

export type UploadPatch = Partial<Omit<UploadItem, "key" | "name">>;

export interface Uploads {
  readonly items: readonly UploadItem[];
  /** Bắt đầu tải các tệp (theo thứ tự chọn). replaces: tệp thay một ảnh mất. */
  add: (files: readonly File[], replaces?: Replace) => void;
  /** Bỏ một dòng (ảnh lỗi, hay đã gắn xong). */
  dismiss: (key: string) => void;
  /** Đổi trạng thái một dòng (ImageManager: attaching, failed, ready để gắn lại). */
  update: (key: string, patch: UploadPatch) => void;
}

/** PUT tệp lên đường ký sẵn. Lỗi mạng → NetworkError (gửi lại được); kho trả mã lỗi → Error khác. */
export type PutFile = (
  url: string,
  headers: Readonly<Record<string, string>>,
  file: File,
  signal: AbortSignal,
) => Promise<void>;

export interface UploadOptions {
  put?: PutFile;
  /** Chờ ms (từ chối khi signal hủy). Mặc định setTimeout. */
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
  /** Đồng hồ ms. Mặc định Date.now. */
  now?: () => number;
}

const POLL_MS = 2000;
const WAIT_MS = 60_000;
const PUT_RETRIES = 3;
const PUT_RETRY_MS = 1000;

/** PUT bằng XMLHttpRequest (eslint chỉ cho lớp realtime dùng fetch; đây là đường của kho tệp, không phải /api/v1). */
export const xhrPut: PutFile = (url, headers, file, signal) =>
  new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open("PUT", url);
    for (const [k, v] of Object.entries(headers)) x.setRequestHeader(k, v);
    const onAbort = () => {
      x.abort();
    };
    // Gỡ bộ nghe khi PUT xong: signal sống suốt trang sửa mẫu, không giữ tham chiếu tới tệp đã tải.
    const settle = (fn: () => void) => () => {
      signal.removeEventListener("abort", onAbort);
      fn();
    };
    x.onload = settle(() => {
      if (x.status >= 200 && x.status < 300) resolve();
      else reject(new Error(`kho tệp trả ${String(x.status)}`));
    });
    x.onerror = settle(() => {
      reject(new NetworkError());
    });
    x.onabort = settle(() => {
      reject(aborted(signal));
    });
    signal.addEventListener("abort", onAbort, { once: true });
    x.send(file);
  });

export function useUploads(opts: UploadOptions = {}): Uploads {
  const qc = useQueryClient();
  const [items, setItems] = useState<UploadItem[]>([]);
  const optsRef = useRef(opts);
  const queue = useRef<{ key: string; file: File }[]>([]);
  const running = useRef(false);
  const abort = useAbortOnUnmount();

  useEffect(() => {
    optsRef.current = opts;
  });

  const headers = (key: string) => ({ "Idempotency-Key": key });
  const create = useWriteAction((key, body: CreateUploadRequest) =>
    unwrap(createUpload({ body, headers: headers(key) })),
  );
  const complete = useWriteAction((key, b: { id: string }) =>
    unwrap(completeUpload({ path: { id: b.id }, headers: headers(key) })),
  );

  const update = useCallback((key: string, patch: UploadPatch) => {
    setItems((xs) => xs.map((x) => (x.key === key ? { ...x, ...patch } : x)));
  }, []);

  /** Chờ tối đa ms, hoặc tới khi báo hiệu realtime làm cũ truy vấn của tệp id. */
  const wake = useCallback(
    async (id: string, ms: number, signal: AbortSignal) => {
      const sleep = optsRef.current.sleep ?? defaultSleep;
      const ac = new AbortController();
      const onAbort = () => {
        ac.abort(aborted(signal));
      };
      signal.addEventListener("abort", onAbort, { once: true });
      let unsubscribe: () => void = () => undefined;
      const woken = new Promise<void>((resolve) => {
        unsubscribe = qc.getQueryCache().subscribe((e) => {
          const k: unknown = e.query.queryKey;
          if (e.type !== "updated" || e.action.type !== "invalidate" || !Array.isArray(k)) return;
          if (k[0] === TOPICS.file && k[1] === id) resolve();
        });
      });
      try {
        await Promise.race([sleep(ms, ac.signal), woken]);
      } finally {
        unsubscribe();
        signal.removeEventListener("abort", onAbort);
        ac.abort();
      }
      signal.throwIfAborted();
    },
    [qc],
  );

  const waitReady = useCallback(
    async (key: string, first: FileInfo, signal: AbortSignal) => {
      const now = optsRef.current.now ?? Date.now;
      const deadline = now() + WAIT_MS;
      let cur = first;
      for (;;) {
        if (cur.status === "ready") {
          update(key, { phase: "ready" });
          return;
        }
        if (cur.status === "rejected" || cur.status === "lost") {
          update(key, { phase: "failed", message: rejectText(cur.rejectReason) });
          return;
        }
        if (now() >= deadline) {
          update(key, { phase: "failed", message: TEXT_PROCESSING_TIMEOUT });
          return;
        }
        await wake(cur.id, POLL_MS, signal);
        const id = cur.id;
        try {
          cur = await qc.query({
            queryKey: queryKey(TOPICS.file, id),
            queryFn: () => unwrap(getFile({ path: { id } })),
            staleTime: 0,
          });
        } catch {
          // Đọc lỗi (mạng, máy chủ bận): hỏi lại ở lượt sau cho tới hết 60 s.
        }
      }
    },
    [qc, update, wake],
  );

  const putWithRetry = useCallback(async (url: string, h: Record<string, string>, file: File, signal: AbortSignal) => {
    const put = optsRef.current.put ?? xhrPut;
    const sleep = optsRef.current.sleep ?? defaultSleep;
    for (let attempt = 0; ; attempt++) {
      try {
        await put(url, h, file, signal);
        return true;
      } catch (e) {
        if (!(e instanceof NetworkError) || attempt >= PUT_RETRIES) return false;
        await sleep(PUT_RETRY_MS, signal);
      }
    }
  }, []);

  const uploadOne = useCallback(
    async (key: string, file: File, signal: AbortSignal) => {
      const c = await create.run({
        purpose: IMAGE_PURPOSE,
        fileName: file.name,
        contentType: file.type === "" ? "application/octet-stream" : file.type,
        sizeBytes: file.size,
      });
      if (!c.ok) {
        update(key, { phase: "failed", message: uploadErrorText(c.error) });
        return;
      }
      const id = c.data.fileId;
      update(key, { fileId: id });
      if (!(await putWithRetry(c.data.putUrl, c.data.headers, file, signal))) {
        update(key, { phase: "failed", message: TEXT_UPLOAD_INCOMPLETE });
        return;
      }
      const d = await complete.run({ id });
      if (!d.ok) {
        update(key, { phase: "failed", message: uploadErrorText(d.error) });
        return;
      }
      if (d.data.status !== "ready" && d.data.status !== "rejected") update(key, { phase: "processing" });
      void waitReady(key, d.data, signal).catch(() => undefined); // chỉ từ chối khi màn đóng
    },
    [create, complete, update, putWithRetry, waitReady],
  );

  const pump = useCallback(async () => {
    if (running.current) return;
    running.current = true;
    try {
      for (let job = queue.current.shift(); job !== undefined; job = queue.current.shift()) {
        const signal = abort.current.signal;
        const closed = () => signal.aborted;
        if (closed()) return;
        try {
          await uploadOne(job.key, job.file, signal);
        } catch {
          if (closed()) return;
          update(job.key, { phase: "failed", message: TEXT_UPLOAD_INCOMPLETE });
        }
      }
    } finally {
      running.current = false;
    }
  }, [uploadOne, update, abort]);

  const add = useCallback(
    (files: readonly File[], replaces?: Replace) => {
      const fresh: UploadItem[] = files.map((f) => ({
        key: crypto.randomUUID(),
        name: f.name,
        phase: "uploading",
        ...(replaces === undefined ? {} : { replaces }),
      }));
      setItems((xs) => [...xs, ...fresh]);
      fresh.forEach((it, i) => {
        const file = files[i];
        if (file !== undefined) queue.current.push({ key: it.key, file });
      });
      void pump();
    },
    [pump],
  );

  const dismiss = useCallback((key: string) => {
    setItems((xs) => xs.filter((x) => x.key !== key));
  }, []);

  return { items, add, dismiss, update };
}
