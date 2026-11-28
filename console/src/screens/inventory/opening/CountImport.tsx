// Nhập số đếm từ Excel của phiên mở sổ (Task 273; mockup docs/mockups/m7/kiem-kho-mo-so.html, trạng thái D; HV09).
// Chọn chế độ (Đặt bằng số trong tệp là mặc định; Cộng thêm: mỗi tệp một lần) → tải tệp .xlsx (createUpload mục đích
// inventory_count_import → PUT thẳng lên kho → completeUpload) → createStocktakeCountImport → máy chủ đọc ở nền
// (queued → running → done|failed); màn hỏi lại mỗi 2 s. Lỗi từng dòng chỉ có số dòng và mã lỗi, không nội dung ô.
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type DragEvent } from "react";
import {
  completeUpload,
  createStocktakeCountImport,
  createUpload,
  getStocktakeCountImport,
  type CountImportMode,
  type StocktakeCountImport,
} from "../../../api";
import { ApiError, errorMessage, unwrap } from "../../../app/errors";
import { useAbortOnUnmount, useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { POLL_MS, XLSX_TYPE, checkLocalFile, fmtInt } from "../../imports/importRules";
import { xhrPut } from "../../products/useUpload";
import { importRowReason } from "./openingRules";

/** Mục đích tệp của số đếm (platform/files: inventory_count_import). */
export const COUNT_IMPORT_PURPOSE = "inventory_count_import";

export const TEXT_ALREADY_APPLIED = "Tệp này đã được cộng vào phiên rồi";
export const TEXT_UPLOAD_INCOMPLETE = "Tệp chưa tải lên hết. Chọn lại tệp để thử lại";

const headers = (key: string) => ({ "Idempotency-Key": key });
const MODE_TEXT: Readonly<Record<CountImportMode, string>> = {
  replace: "đặt bằng số trong tệp",
  add: "cộng thêm vào số đã đếm",
};

type Problem = { kind: "file"; name: string } | { kind: "error"; name: string; error: unknown };

export function CountImport(props: { stocktakeId: string; onDone?: () => void }) {
  const { stocktakeId, onDone } = props;
  const [mode, setMode] = useState<CountImportMode>("replace");
  const [picked, setPicked] = useState<{ id: string; name: string } | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [busy, setBusy] = useState(false);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const abort = useAbortOnUnmount();

  const create = useWriteAction(
    (key, body: { purpose: string; fileName: string; contentType: string; sizeBytes: number }) =>
      unwrap(createUpload({ body, headers: headers(key) })),
  );
  const complete = useWriteAction((key, fileId: string) =>
    unwrap(completeUpload({ path: { id: fileId }, headers: headers(key) })),
  );
  const start = useWriteAction((key, b: { stocktakeId: string; fileId: string; mode: CountImportMode }) =>
    unwrap(
      createStocktakeCountImport({
        path: { id: b.stocktakeId },
        body: { fileId: b.fileId, mode: b.mode },
        headers: headers(key),
      }),
    ),
  );

  const imp = useQuery({
    queryKey: queryKey("inventory.count_import", picked?.id ?? ""),
    enabled: picked !== null,
    queryFn: () => unwrap(getStocktakeCountImport({ path: { id: picked?.id ?? "" } })),
    refetchInterval: (q) => (q.state.data?.status === "queued" || q.state.data?.status === "running" ? POLL_MS : false),
  });
  const data: StocktakeCountImport | undefined = imp.data;

  const finished = data?.status === "done";
  const doneRef = useRef(onDone);
  useEffect(() => {
    doneRef.current = onDone;
  });
  useEffect(() => {
    if (finished) doneRef.current?.();
  }, [finished]);

  const send = async (file: File): Promise<Problem | null> => {
    const signal = abort.current.signal;
    const c = await create.run({
      purpose: COUNT_IMPORT_PURPOSE,
      fileName: file.name,
      contentType: XLSX_TYPE,
      sizeBytes: file.size,
    });
    if (!c.ok) return { kind: "error", name: file.name, error: c.error };
    try {
      await xhrPut(c.data.putUrl, c.data.headers, file, signal);
    } catch {
      return { kind: "error", name: file.name, error: new Error(TEXT_UPLOAD_INCOMPLETE) };
    }
    const d = await complete.run(c.data.fileId);
    if (!d.ok) return { kind: "error", name: file.name, error: d.error };
    const s = await start.run({ stocktakeId, fileId: c.data.fileId, mode });
    if (!s.ok) return { kind: "error", name: file.name, error: s.error };
    setPicked({ id: s.data.id, name: file.name });
    return null;
  };

  const pick = async (file: File | undefined) => {
    if (file === undefined || busy) return;
    setPicked(null);
    if (checkLocalFile(file) !== null) {
      setProblem({ kind: "file", name: file.name });
      return;
    }
    setProblem(null);
    setBusy(true);
    try {
      setProblem(await send(file));
    } finally {
      setBusy(false);
    }
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    void pick(e.dataTransfer.files[0]);
  };

  const name = picked?.name ?? "Tệp";
  const reading = busy || data?.status === "queued" || data?.status === "running";

  return (
    <div className="stack">
      <div className="card">
        <h2>Nhập số đếm từ Excel</h2>
        <p className="small muted">
          Trang đầu của tệp có hai cột <b>Mã</b> (SKU hoặc barcode) và <b>Số đếm</b>. Cùng một mã xuất hiện nhiều dòng
          thì cộng lại. Ô có công thức không được đọc: dán giá trị.
        </p>
        <div className="opts" style={{ marginTop: 16 }}>
          <label className={mode === "replace" ? "opt is-on" : "opt"}>
            <input
              type="radio"
              name="count-mode"
              checked={mode === "replace"}
              disabled={reading}
              onChange={() => {
                setMode("replace");
              }}
            />
            <span className="bold">Đặt bằng số trong tệp</span>
            <span className="hint">
              Mã có trong tệp lấy đúng số của tệp. Nhập lại cùng tệp không đổi gì. Dùng khi một nhóm đếm xong cả khu
              vực.
            </span>
          </label>
          <label className={mode === "add" ? "opt is-on" : "opt"}>
            <input
              type="radio"
              name="count-mode"
              checked={mode === "add"}
              disabled={reading}
              onChange={() => {
                setMode("add");
              }}
            />
            <span className="bold">Cộng thêm vào số đã đếm</span>
            <span className="hint">
              Cộng số trong tệp vào số đang có (kể cả số vừa quét). Một tệp chỉ cộng được một lần.
            </span>
          </label>
        </div>
        <div
          className="drop"
          style={{ marginTop: 16, ...(over ? { borderColor: "var(--ink)" } : {}) }}
          onDragOver={(e) => {
            e.preventDefault();
            setOver(true);
          }}
          onDragLeave={() => {
            setOver(false);
          }}
          onDrop={onDrop}
        >
          <b>Kéo tệp .xlsx vào đây</b>
          <span className="small muted">hoặc</span>
          <button
            type="button"
            className="btn btn-line"
            disabled={reading}
            onClick={() => {
              input.current?.click();
            }}
          >
            Chọn tệp
          </button>
          <span className="hint">Tối đa 5 MB</span>
          <input
            ref={input}
            type="file"
            hidden
            aria-label="Tệp số đếm .xlsx"
            accept={`.xlsx,${XLSX_TYPE}`}
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              void pick(f);
            }}
          />
        </div>
        {problem !== null && <ProblemBox problem={problem} />}
      </div>

      {reading && (
        <div className="card">
          <div className="row-between">
            <h2>{name}</h2>
            <span className="chip">Đang đọc tệp</span>
          </div>
          <p className="small muted">Máy đang đọc và cộng vào phiên. Có thể rời trang; kết quả nằm ở đây khi xong.</p>
        </div>
      )}

      {data?.status === "failed" && (
        <div className="box box-err" role="alert">
          <b>{`Không đọc được ${name}.`}</b>{" "}
          <span>Trang đầu không có cột "Mã" và "Số đếm", hoặc tệp hỏng. Không số nào được đổi.</span>
        </div>
      )}

      {data?.status === "done" && (
        <div className="card">
          <div className="row-between">
            <h2>{name}</h2>
            <span className="chip chip-ok">Đã nhập</span>
          </div>
          <p>
            <b>{`${fmtInt(data.appliedRows)} dòng`}</b>
            {` đã vào phiên (${MODE_TEXT[data.mode]}).`}
            {data.errorRows.length > 0 && (
              <>
                {" "}
                <b className="err-text">{`${fmtInt(data.errorRows.length)} dòng lỗi`}</b>
                {" không vào; sửa trong tệp rồi nhập lại cả tệp."}
              </>
            )}
          </p>
          {data.errorRows.length > 0 && (
            <div className="tbl-frame" style={{ marginTop: 8, maxHeight: 320, overflow: "auto" }}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Dòng</th>
                    <th>Lỗi</th>
                  </tr>
                </thead>
                <tbody>
                  {data.errorRows.map((r) => (
                    <tr key={`${String(r.row)}-${r.code}`} className="is-err">
                      <td data-label="Dòng" className="num">
                        {r.row}
                      </td>
                      <td data-label="Lỗi">{importRowReason(r)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="hint" style={{ marginTop: 8 }}>
            Chỉ hiện số dòng và lỗi, không chép nội dung ô.
          </p>
        </div>
      )}

      {imp.isError && data === undefined && (
        <div className="box box-err" role="alert">
          <b>{errorMessage(imp.error)}</b>
        </div>
      )}
    </div>
  );
}

function ProblemBox({ problem }: { problem: Problem }) {
  if (problem.kind === "file") {
    return (
      <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
        <b>{`Không nhận tệp ${problem.name}.`}</b> <span>Chỉ nhận tệp .xlsx, tối đa 5 MB.</span>
      </div>
    );
  }
  const already = problem.error instanceof ApiError && problem.error.code === "import_already_applied";
  return (
    <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
      <b>{already ? TEXT_ALREADY_APPLIED : errorMessage(problem.error)}</b>
      {already && (
        <>
          {" "}
          <span>Cộng lần nữa sẽ đếm trùng. Muốn nhập lại thì chọn "Đặt bằng số trong tệp".</span>
        </>
      )}
    </div>
  );
}
