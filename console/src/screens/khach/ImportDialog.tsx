// Hộp "Nhập khách từ Excel" (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-danh-sach.html trạng thái D; HV27).
// Ba bước: tải mẫu (getCustomerImportTemplate), chọn tệp .xlsx (createUpload purpose customer_import → PUT thẳng lên kho
// → completeUpload → startCustomerImport), kết quả (hỏi lại getCustomerImport tới khi xong). Kết quả chỉ có số đếm và lỗi
// theo dòng, cột, mã lỗi: máy chủ không trả giá trị ô, màn cũng không đọc tệp. Mọi đường ghi qua useWriteAction.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  completeUpload,
  createUpload,
  getCustomerImport,
  getCustomerImportTemplate,
  startCustomerImport,
  type CreateUploadRequest,
  type CustomerImport,
  type StartCustomerImportRequest,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useAbortOnUnmount, useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { POLL_MS, XLSX_TYPE, checkLocalFile, fileNameFrom, fmtInt, rowText } from "../imports/importRules";
import { xhrPut } from "../products/useUpload";
import { Dialog } from "../staff/staffData";
import { headers, reloadCustomers } from "./customerData";
import { importErrorText } from "./customerRules";
import "../imports/imports.css";

const PURPOSE = "customer_import";
const ACCEPT = `.xlsx,${XLSX_TYPE}`;
const STEPS = ["Tải mẫu", "Chọn tệp", "Kết quả"] as const;
const TEXT_NOT_XLSX = "Chỉ nhận tệp Excel .xlsx (lưu lại từ tệp mẫu), tối đa 5 MB.";
const TEXT_UPLOAD_INCOMPLETE = "Tải lên chưa trọn. Chọn lại tệp để tải lên lần nữa.";

type Put = (url: string, h: Readonly<Record<string, string>>, file: File, signal: AbortSignal) => Promise<void>;

export interface ImportDialogProps {
  onClose: () => void;
  /** PUT tệp lên kho (mặc định XMLHttpRequest; test thay bản giả). */
  put?: Put;
  /** Hỏi lại lần nhập đang chạy mỗi pollMs (mặc định 2 giây). */
  pollMs?: number;
}

const running = (imp: CustomerImport | undefined) => imp?.status === "pending" || imp?.status === "running";

function Result({ imp }: { imp: CustomerImport }) {
  if (imp.status === "failed") {
    const code = imp.errors.find((e) => e.row === 0)?.code ?? "import_failed";
    return (
      <div className="box box-err" role="alert">
        <b>Chưa nhập được.</b> {importErrorText(code)}.
      </div>
    );
  }
  if (running(imp)) return <p className="muted">Đang nhập… Có thể đóng hộp này, việc nhập vẫn chạy.</p>;
  const rowErrors = imp.errors.filter((e) => e.row > 0);
  const more = (imp.errorRows ?? 0) - rowErrors.length;
  const kpi = (label: string, v: number | undefined, bad = false) => (
    <div className={bad ? "kpi is-bad" : "kpi"}>
      <p className="small muted">{label}</p>
      <p className="v">{fmtInt(v ?? 0)}</p>
    </div>
  );
  return (
    <>
      <div className="kpis kh-kpis">
        {kpi("Đã thêm", imp.okRows)}
        {kpi("Bỏ qua (SĐT đã có)", imp.skippedRows)}
        {kpi("Lỗi", imp.errorRows, (imp.errorRows ?? 0) > 0)}
        {kpi("Tổng dòng", imp.totalRows)}
      </div>
      {(rowErrors.length > 0 || more > 0) && (
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Dòng</th>
                <th>Cột</th>
                <th>Lỗi</th>
              </tr>
            </thead>
            <tbody>
              {rowErrors.map((e) => (
                <tr key={`${String(e.row)}-${e.column ?? ""}-${e.code}`}>
                  <td data-label="Dòng" className="num">
                    {rowText(e.row)}
                  </td>
                  <td data-label="Cột">{e.column ?? "—"}</td>
                  <td data-label="Lỗi">{importErrorText(e.code)}</td>
                </tr>
              ))}
              {more > 0 && (
                <tr>
                  <td data-label="Dòng" className="num">
                    …
                  </td>
                  <td data-label="Cột">—</td>
                  <td data-label="Lỗi">{`${fmtInt(more)} lỗi khác`}</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

export function ImportDialog({ onClose, put = xhrPut, pollMs = POLL_MS }: ImportDialogProps) {
  const qc = useQueryClient();
  const abort = useAbortOnUnmount();
  const [downloaded, setDownloaded] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [started, setStarted] = useState<{ id: string; name: string } | null>(null);

  // Ba bước ghi, mỗi bước một khóa chống trùng riêng (gửi lại cùng bước thì cùng khóa).
  const begin = useWriteAction((key, body: StartCustomerImportRequest) =>
    unwrap(startCustomerImport({ headers: headers(key), body })),
  );
  const reserve = useWriteAction((key, body: CreateUploadRequest) =>
    unwrap(createUpload({ headers: headers(key), body })),
  );
  const seal = useWriteAction((key, b: { fileId: string }) =>
    unwrap(completeUpload({ headers: headers(key), path: { id: b.fileId } })),
  );
  const imp = useQuery({
    queryKey: queryKey("customer.import", started?.id),
    enabled: started !== null,
    queryFn: () => unwrap(getCustomerImport({ path: { importId: started?.id ?? "" } })),
    refetchInterval: (q) => (q.state.data === undefined || running(q.state.data) ? pollMs : false),
  });
  // Nhập xong: danh sách khách, nhãn có thể đã đổi.
  const done = imp.data?.status === "done";
  useEffect(() => {
    if (done) reloadCustomers(qc);
  }, [done, qc]);

  const download = async () => {
    setProblem(null);
    try {
      const p = getCustomerImportTemplate();
      const blob = await unwrap(p);
      const cd = (await p).response?.headers.get("content-disposition") ?? null;
      const a = Object.assign(document.createElement("a"), {
        href: URL.createObjectURL(blob),
        download: fileNameFrom(cd),
      });
      a.click();
      setTimeout(() => {
        URL.revokeObjectURL(a.href);
      }, 0);
      setDownloaded(true);
    } catch (e) {
      setProblem(errorMessage(e));
    }
  };

  const send = async (file: File): Promise<string | null> => {
    const c = await reserve.run({
      purpose: PURPOSE,
      fileName: file.name,
      contentType: XLSX_TYPE,
      sizeBytes: file.size,
    });
    if (!c.ok) return errorMessage(c.error);
    try {
      await put(c.data.putUrl, c.data.headers, file, abort.current.signal);
    } catch {
      return TEXT_UPLOAD_INCOMPLETE;
    }
    const d = await seal.run({ fileId: c.data.fileId });
    if (!d.ok) return errorMessage(d.error);
    const s = await begin.run({ fileId: c.data.fileId });
    if (!s.ok) return errorMessage(s.error);
    setStarted({ id: s.data.importId, name: file.name });
    return null;
  };

  const pick = async (file: File | undefined) => {
    if (file === undefined || busy !== null) return;
    if (checkLocalFile(file) !== null) {
      setProblem(TEXT_NOT_XLSX);
      return;
    }
    setProblem(null);
    setBusy(file.name);
    try {
      setProblem(await send(file));
    } finally {
      setBusy(null);
    }
  };

  const step = started !== null ? 3 : downloaded ? 2 : 1;
  const title = started === null ? "Nhập khách từ Excel" : `Kết quả nhập · ${started.name}`;

  return (
    <Dialog
      title={title}
      onClose={onClose}
      className="dlg-wide"
      footer={
        <button type="button" className={started === null ? "btn btn-line" : "btn btn-dark"} onClick={onClose}>
          {started === null ? "Đóng" : "Xong"}
        </button>
      }
    >
      <ol className="steps">
        {STEPS.map((label, i) => (
          <li key={label} className={i + 1 < step ? "is-done" : i + 1 === step ? "is-on" : undefined}>
            <b>{i + 1}</b>
            {label}
          </li>
        ))}
      </ol>
      {started === null ? (
        <div className="stack-sm">
          <button
            type="button"
            className="btn btn-line"
            onClick={() => {
              void download();
            }}
          >
            Tải tệp mẫu (.xlsx)
          </button>
          <p className="small muted">
            Cột: Họ tên*, SĐT*, Ngày sinh, Tỉnh/thành, Xã/phường, Số nhà đường, Nhãn, Ghi chú. Tối đa 20.000 dòng, 5 MB.
          </p>
          <p className="hint">Cột SĐT để định dạng Văn bản (Text) trước khi dán số, để Excel không bỏ mất số 0 đầu.</p>
          <div className="drop">
            <p>
              <b>{busy === null ? "Chọn tệp đã điền" : `Đang tải lên ${busy}…`}</b>
            </p>
            <label className="btn btn-line">
              Chọn tệp .xlsx
              <input
                type="file"
                accept={ACCEPT}
                hidden
                disabled={busy !== null}
                onChange={(e) => {
                  const f = e.currentTarget.files?.[0];
                  e.currentTarget.value = "";
                  void pick(f);
                }}
              />
            </label>
          </div>
          <p className="small muted">
            SĐT đã có trong CRM thì bỏ qua dòng đó, không ghi đè hồ sơ cũ. Nhập lại cùng tệp không tạo trùng.
          </p>
          {problem !== null && (
            <div className="box box-err" role="alert">
              {problem}
            </div>
          )}
        </div>
      ) : imp.data !== undefined ? (
        <Result imp={imp.data} />
      ) : imp.isError ? (
        <div className="box box-err" role="alert">
          {errorMessage(imp.error)}
        </div>
      ) : (
        <p className="muted">Đang nhập…</p>
      )}
    </Dialog>
  );
}
