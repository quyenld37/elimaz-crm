// Màn Nhập sản phẩm từ Excel (kế hoạch M1 Task 52; mockup đã duyệt docs/mockups/m1/nhap-excel.html, trạng thái A–F).
//
// A: tải tệp mẫu (getCatalogImportTemplate), chọn hay thả tệp .xlsx. B: tải lên (createUpload purpose catalog_import →
// PUT thẳng lên kho có thanh phần trăm → completeUpload → createCatalogImport), rồi máy chủ kiểm nền. C: có lỗi → bảng
// lỗi theo dòng, không có nút áp dụng. D: hợp lệ → số tạo, cập nhật, giữ nguyên; "Áp dụng…" hỏi lại rồi
// applyCatalogImport (version của bản đang xem). E: đang áp, đã áp. F: tệp đã nhập (duplicate), tệp không đọc được,
// áp thất bại vì dữ liệu vừa đổi.
//
// Mã bản nhập và tên tệp nằm trên URL (?id=&tep=): rời trang rồi quay lại vẫn thấy kết quả. Bản nhập đang kiểm hay
// đang áp được hỏi lại mỗi 2 s (không có báo hiệu realtime cho bản nhập). Mọi đường ghi qua useWriteAction (khóa chống
// trùng sinh lúc bấm, gửi lại cùng khóa khi mạng chập, máy chủ bận).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type DragEvent } from "react";
import { Link, useSearchParams } from "react-router";
import {
  applyCatalogImport,
  completeUpload,
  createCatalogImport,
  createUpload,
  getCatalogImport,
  getCatalogImportTemplate,
  type CatalogImport,
  type CreateCatalogImportRequest,
  type CreateUploadRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useAbortOnUnmount, useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { xhrPut } from "../products/useUpload";
import { Dialog } from "../staff/staffData";
import { ImportErrors } from "./ImportErrors";
import {
  IMPORT_PURPOSE,
  POLL_MS,
  XLSX_TYPE,
  checkLocalFile,
  fileErrors,
  fileNameFrom,
  fmtInt,
  fmtSize,
  fmtTime,
  percent,
  rowText,
  stepOf,
  variantTotal,
  type View,
} from "./importRules";
import "./imports.css";

/** Khóa truy vấn của bản nhập (không có chủ đề realtime). */
const IMPORT_KEY = "catalog.import";
const STEPS = ["Tải tệp mẫu", "Chọn tệp", "Kiểm", "Áp dụng"] as const;
const ACCEPT = `.xlsx,${XLSX_TYPE}`;

export const TEXT_NOT_READABLE = "Không đọc được tệp.";
export const TEXT_NOT_READABLE_HINT =
  "Tệp không phải Excel (.xlsx), hoặc quá 5 MB, hoặc quá 20.000 dòng. Lưu lại bằng Excel dạng .xlsx từ tệp mẫu.";
export const TEXT_UPLOAD_INCOMPLETE = "Tải lên chưa trọn. Chọn lại tệp để tải lên lần nữa.";

/** Lỗi trước khi có bản nhập: tệp không nhận (file) hay lỗi API, mạng (text). */
type Problem = { kind: "file" } | { kind: "text"; text: string };

interface Uploading {
  name: string;
  size: number;
  loaded: number;
}

const headers = (key: string) => ({ "Idempotency-Key": key });
const running = (imp: CatalogImport | undefined) => imp?.status === "validating" || imp?.status === "applying";

function problemOf(err: unknown): Problem {
  if (err instanceof ApiError && (err.code === "file_too_large" || err.code === "file_type_not_allowed"))
    return { kind: "file" };
  return { kind: "text", text: errorMessage(err) };
}

function useImport(id: string | null) {
  return useQuery({
    queryKey: queryKey(IMPORT_KEY, id),
    enabled: id !== null,
    queryFn: () => unwrap(getCatalogImport({ path: { id: id ?? "" } })),
    // Dữ liệu vừa nhận từ createCatalogImport, applyCatalogImport còn tươi tới lượt hỏi kế tiếp.
    staleTime: POLL_MS,
    refetchInterval: (q) => (running(q.state.data) ? POLL_MS : false),
  });
}

function Steps({ view }: { view: View }) {
  const step = stepOf(view);
  return (
    <ol className="steps">
      {STEPS.map((label, i) => {
        const n = i + 1;
        const cls = n < step ? "is-done" : n === step ? "is-on" : undefined;
        return (
          <li key={label} className={cls}>
            <b>{n}</b>
            {label}
          </li>
        );
      })}
    </ol>
  );
}

export function ImportPage() {
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const id = params.get("id");
  const fileName = params.get("tep") ?? "";
  const imp = useImport(id);
  const data = imp.data;
  const original = useImport(data?.status === "duplicate" ? (data.duplicateOf ?? null) : null);

  const [downloaded, setDownloaded] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [uploading, setUploading] = useState<Uploading | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const abort = useAbortOnUnmount();

  const create = useWriteAction((key, body: CreateUploadRequest) =>
    unwrap(createUpload({ body, headers: headers(key) })),
  );
  const complete = useWriteAction((key, b: { id: string }) =>
    unwrap(completeUpload({ path: { id: b.id }, headers: headers(key) })),
  );
  const start = useWriteAction((key, body: CreateCatalogImportRequest) =>
    unwrap(createCatalogImport({ body, headers: headers(key) })),
  );
  const apply = useWriteAction((key, b: { id: string; version: number }) =>
    unwrap(applyCatalogImport({ path: { id: b.id }, body: { version: b.version }, headers: headers(key) })),
  );

  const download = async () => {
    setDownloading(true);
    setDownloadError(null);
    try {
      const p = getCatalogImportTemplate();
      const blob = await unwrap(p);
      const { response } = await p;
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = fileNameFrom(response?.headers.get("content-disposition") ?? null);
      document.body.append(a);
      a.click();
      a.remove();
      setTimeout(() => {
        URL.revokeObjectURL(url);
      }, 0);
      setDownloaded(true);
    } catch (e) {
      setDownloadError(errorMessage(e));
    } finally {
      setDownloading(false);
    }
  };

  const send = async (file: File) => {
    const signal = abort.current.signal;
    const c = await create.run({
      purpose: IMPORT_PURPOSE,
      fileName: file.name,
      contentType: XLSX_TYPE,
      sizeBytes: file.size,
    });
    if (!c.ok) return problemOf(c.error);
    try {
      await xhrPut(c.data.putUrl, c.data.headers, file, signal, (loaded) => {
        setUploading((u) => (u === null ? u : { ...u, loaded }));
      });
    } catch {
      return { kind: "text", text: TEXT_UPLOAD_INCOMPLETE } satisfies Problem;
    }
    const d = await complete.run({ id: c.data.fileId });
    if (!d.ok) return problemOf(d.error);
    const s = await start.run({ fileId: c.data.fileId });
    if (!s.ok) return problemOf(s.error);
    if (signal.aborted) return null;
    qc.setQueryData(queryKey(IMPORT_KEY, s.data.id), s.data);
    setParams({ id: s.data.id, tep: file.name });
    return null;
  };

  const pick = async (file: File | undefined) => {
    if (file === undefined || uploading !== null) return;
    setConfirm(false);
    apply.reset();
    setParams({}, { replace: true });
    if (checkLocalFile(file) !== null) {
      setProblem({ kind: "file" });
      return;
    }
    setProblem(null);
    setUploading({ name: file.name, size: file.size, loaded: 0 });
    try {
      setProblem(await send(file));
    } catch (e) {
      setProblem(problemOf(e));
    } finally {
      setUploading(null);
    }
  };

  const choose = () => {
    input.current?.click();
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    void pick(e.dataTransfer.files[0]);
  };

  const doApply = async () => {
    if (data === undefined) return;
    const r = await apply.run({ id: data.id, version: data.version });
    setConfirm(false);
    if (r.ok) qc.setQueryData(queryKey(IMPORT_KEY, data.id), r.data);
    else void imp.refetch();
  };

  const view: View =
    uploading !== null
      ? { kind: "uploading" }
      : problem === null && id !== null && data !== undefined
        ? { kind: "import", status: data.status }
        : { kind: "idle", downloaded };

  // Lỗi đọc chỉ chặn màn khi chưa có bản nhập nào để hiện; lượt hỏi lại lỗi giữa chừng thì giữ bản đang có.
  const openFailed = id !== null && imp.isError && data === undefined;
  const showStart = uploading === null && (problem !== null || id === null || openFailed);
  const name = fileName === "" ? "Tệp" : fileName;

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Nhập sản phẩm từ Excel</h1>
          <p className="sub">
            Một dòng mỗi biến thể, khớp theo SKU: có rồi thì cập nhật, chưa có thì tạo. Kiểm hết trước; có một lỗi là
            không ghi gì.
          </p>
        </div>
      </div>
      <Steps view={view} />
      <input
        ref={input}
        type="file"
        accept={ACCEPT}
        hidden
        onChange={(e) => {
          const f = e.currentTarget.files?.[0];
          e.currentTarget.value = "";
          void pick(f);
        }}
      />

      <div className="stack">
        {problem?.kind === "file" && (
          <div className="box box-err">
            <b>{TEXT_NOT_READABLE}</b> <span>{TEXT_NOT_READABLE_HINT}</span>
          </div>
        )}
        {problem?.kind === "text" && (
          <div className="box box-err" role="alert">
            <b>Chưa nhập được.</b> <span>{problem.text}</span>
          </div>
        )}
        {problem === null && openFailed && (
          <div className="box box-err" role="alert">
            <b>Không mở được bản nhập.</b> <span>{errorMessage(imp.error)}</span>
          </div>
        )}

        {uploading !== null && (
          <div className="card">
            <div className="row-between">
              <b className="imp-name">{uploading.name}</b>
              <span className="small muted num">
                {fmtSize(uploading.size)} · {percent(uploading.loaded, uploading.size)}%
              </span>
            </div>
            <div className="bar mt-sm">
              <i style={{ width: `${String(percent(uploading.loaded, uploading.size))}%` }} />
            </div>
          </div>
        )}

        {showStart && (
          <div className="cols">
            <div className="card">
              <h2>1. Tải tệp mẫu</h2>
              <p>
                Tệp mẫu có sẵn danh sách danh mục, trạng thái và một cột cho mỗi thuộc tính đang có. Trang "Hướng dẫn"
                trong tệp ghi cách điền.
              </p>
              <button
                type="button"
                className="btn btn-line mt-md"
                disabled={downloading}
                onClick={() => {
                  void download();
                }}
              >
                Tải tệp mẫu (.xlsx)
              </button>
              {downloadError !== null && (
                <p className="err-text mt-sm" role="alert">
                  {downloadError}
                </p>
              )}
              <p className="hint mt-sm">Không nhận tệp xuất từ Nhanh: chép dữ liệu sang tệp mẫu của CRM.</p>
            </div>
            <div className="card">
              <h2>2. Chọn tệp đã điền</h2>
              <div
                className={over ? "drop is-over" : "drop"}
                onDragOver={(e) => {
                  e.preventDefault();
                  setOver(true);
                }}
                onDragLeave={() => {
                  setOver(false);
                }}
                onDrop={onDrop}
              >
                <p>
                  <b>Kéo tệp .xlsx vào đây</b> hoặc
                </p>
                <button type="button" className="btn btn-dark" onClick={choose}>
                  Chọn tệp
                </button>
                <p className="hint">Tối đa 5 MB, 20.000 dòng. Ô có công thức sẽ bị từ chối.</p>
              </div>
            </div>
          </div>
        )}

        {view.kind === "import" && data !== undefined && (
          <ImportView
            imp={data}
            name={name}
            originalAt={original.data?.createdAt}
            onChoose={choose}
            onApply={() => {
              apply.reset();
              setConfirm(true);
            }}
            applyMessage={apply.message}
          />
        )}
      </div>

      {confirm && data?.status === "ready" && (
        <Dialog
          title="Áp dụng tệp nhập?"
          onClose={() => {
            setConfirm(false);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setConfirm(false);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-dark"
                disabled={apply.pending}
                onClick={() => {
                  void doApply();
                }}
              >
                Áp dụng
              </button>
            </>
          }
        >
          <p>
            Tạo <b>{fmtInt(data.summary.create)}</b> biến thể, cập nhật <b>{fmtInt(data.summary.update)}</b> biến thể
            của <b>{fmtInt(data.summary.models)}</b> mẫu. Giá đổi của mẫu đang bán hiện trên web ngay sau đó.
          </p>
        </Dialog>
      )}
    </>
  );
}

interface ImportViewProps {
  imp: CatalogImport;
  name: string;
  /** Giờ tạo bản nhập đã áp cùng tệp (duplicate); chưa đọc được thì undefined. */
  originalAt: string | undefined;
  onChoose: () => void;
  onApply: () => void;
  applyMessage: string | null;
}

function ChooseAgain({ onChoose, label }: { onChoose: () => void; label: string }) {
  return (
    <div className="row mt-sm">
      <button type="button" className="btn btn-dark" onClick={onChoose}>
        {label}
      </button>
    </div>
  );
}

function ImportView({ imp, name, originalAt, onChoose, onApply, applyMessage }: ImportViewProps) {
  const s = imp.summary;
  switch (imp.status) {
    case "validating":
      return (
        <div className="card">
          <div className="row-between">
            <b className="imp-name">{name}</b>
            <span className="chip">Đang kiểm</span>
          </div>
          <p className="small muted mt-sm">
            Đang đọc và kiểm từng dòng, thường dưới một phút. Có thể rời trang; kết quả giữ ở đây.
          </p>
          <div className="bar is-busy mt-sm">
            <i />
          </div>
        </div>
      );
    case "invalid":
      if (fileErrors(imp))
        return (
          <div className="box box-err">
            <b>{TEXT_NOT_READABLE}</b> <span>{imp.errors.map((e) => e.message).join(" ")}</span>
            <ChooseAgain onChoose={onChoose} label="Chọn tệp khác" />
          </div>
        );
      return (
        <>
          <div className="box box-err">
            <b>Chưa ghi gì: tệp có {fmtInt(s.errors ?? imp.errors.length)} lỗi.</b>{" "}
            <span>
              Sửa các dòng dưới trong tệp rồi tải lên lại. Số dòng là số dòng trong Excel (dòng 1 là tiêu đề).
            </span>
            {imp.errors.length < (s.errors ?? 0) && (
              <p className="small mt-sm">Đang hiện {fmtInt(imp.errors.length)} lỗi đầu.</p>
            )}
            <ChooseAgain onChoose={onChoose} label="Chọn tệp đã sửa" />
          </div>
          <ImportErrors errors={imp.errors} />
        </>
      );
    case "ready":
      return (
        <div className="card">
          <div className="row-between">
            <h2 className="imp-name">{name} sẵn sàng</h2>
            <span className="chip chip-ok">Hợp lệ</span>
          </div>
          <div className="kpis">
            <Kpi label="Mẫu" value={s.models} />
            <Kpi label="Biến thể tạo mới" value={s.create} />
            <Kpi label="Biến thể cập nhật" value={s.update} />
            <Kpi label="Giữ nguyên" value={s.unchanged} />
          </div>
          <p className="small muted mt-md">
            Áp dụng ghi tất cả trong một lần: hoặc đủ {fmtInt(variantTotal(s))} biến thể, hoặc không gì cả.
          </p>
          {applyMessage !== null && (
            <p className="err-text mt-sm" role="alert">
              {applyMessage}
            </p>
          )}
          <div className="savebar mt-md">
            <button type="button" className="btn btn-line" onClick={onChoose}>
              Chọn tệp khác
            </button>
            <button type="button" className="btn btn-dark" onClick={onApply}>
              Áp dụng…
            </button>
          </div>
        </div>
      );
    case "applying":
      return (
        <div className="card">
          <div className="row-between">
            <b>Đang áp dụng…</b>
            <span className="chip">Đang ghi</span>
          </div>
          <div className="bar is-busy mt-sm">
            <i />
          </div>
          <p className="small muted mt-sm">Thường dưới 20 giây cho 1.000 biến thể.</p>
        </div>
      );
    case "applied":
      return (
        <div className="box box-ok">
          <b className="ok-text">Đã nhập xong.</b> {fmtInt(s.models)} mẫu: tạo {fmtInt(s.create)} biến thể, cập nhật{" "}
          {fmtInt(s.update)}.
          <div className="row mt-sm">
            <Link className="btn btn-line" to="/san-pham">
              Xem danh sách sản phẩm
            </Link>
          </div>
        </div>
      );
    case "duplicate":
      return (
        <div className="box box-info">
          <b>
            {originalAt === undefined
              ? "Tệp này đã nhập trước đây."
              : `Tệp này đã nhập lúc ${fmtTime(new Date(originalAt))}.`}
          </b>{" "}
          Nội dung giống hệt lần đó nên không có gì thay đổi. Muốn sửa thì sửa tệp rồi tải lên lại.
          <ChooseAgain onChoose={onChoose} label="Chọn tệp khác" />
        </div>
      );
    case "failed": {
      const [only] = imp.errors;
      return (
        <>
          <div className="box box-err">
            <b>Không ghi được, không có gì thay đổi.</b>{" "}
            <span>
              {imp.errors.length === 1 && only !== undefined
                ? `${only.row === 0 ? "" : `Dòng ${rowText(only.row)}: `}${only.message.replace(/[.\s]+$/, "")}. Sửa tệp rồi tải lên lại.`
                : "Sửa các dòng dưới trong tệp rồi tải lên lại."}
            </span>
            <ChooseAgain onChoose={onChoose} label="Chọn tệp đã sửa" />
          </div>
          {imp.errors.length > 1 && <ImportErrors errors={imp.errors} />}
        </>
      );
    }
  }
}

function Kpi({ label, value }: { label: string; value: number }) {
  return (
    <div className="kpi">
      <span className="small muted">{label}</span>
      <div className="v">{fmtInt(value)}</div>
    </div>
  );
}
