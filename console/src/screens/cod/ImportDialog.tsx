// Hộp "Nhập bảng kê COD" (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái B; HV30; TI02,
// TI04). Chọn tài khoản hãng (listCarrierAccounts), tải tệp lên kho (createUpload mục đích cod_statement → PUT thẳng →
// completeUpload), chọn định dạng, Nhập (importCodStatement, 202). Máy chủ đọc tệp và khớp chạy nền: hộp đóng, dòng mới
// "Đang phân tích" hiện ở danh sách. Tệp đã nhập → 409 statement_duplicate kèm statementId: câu có ngày, tên, kỳ của
// bảng kê cũ và nút sang đó. Mọi đường ghi qua useWriteAction (khóa chống trùng giữ nguyên khi gửi lại).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type DragEvent } from "react";
import { Link } from "react-router";
import {
  completeUpload,
  createUpload,
  getCodStatement,
  importCodStatement,
  listCarrierAccounts,
  type CarrierAccount,
  type CreateUploadRequest,
  type ImportCodStatementRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useAbortOnUnmount, useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { xhrPut } from "../products/useUpload";
import { Dialog } from "../staff/staffData";
import { headers, reloadCod } from "./codData";
import {
  ACCEPT,
  ELIMAZ_FORMAT,
  STATEMENT_PURPOSE,
  TEXT_BAD_FILE,
  TEXT_NO_ACCOUNTS,
  TEXT_UPLOAD_INCOMPLETE,
  accountTitle,
  checkLocalFile,
  contentTypeOf,
  periodText,
  sizeText,
  vnDay,
} from "./codRules";
import "./cod.css";

/** PUT tệp lên đường ký sẵn (mặc định XMLHttpRequest; test thay bản giả). */
export type PutFile = (
  url: string,
  h: Readonly<Record<string, string>>,
  file: File,
  signal: AbortSignal,
) => Promise<void>;

export interface ImportDialogProps {
  onClose: () => void;
  put?: PutFile;
  /** Tài khoản chọn sẵn (Nhập lại từ dòng lỗi). */
  accountId?: string;
  /** Câu lỗi của lần nhập trước (Nhập lại từ dòng lỗi). */
  problem?: string;
}

const accountLabel = (a: CarrierAccount) => (a.status === "revoked" ? `${a.name} (đã thu hồi)` : a.name);

interface Uploaded {
  fileId: string;
  name: string;
  size: number;
}

/** Câu tệp trùng: đọc bảng kê cũ để có ngày nhập, tên, kỳ. */
function Duplicate({ id, onOpen }: { id: string; onOpen: () => void }) {
  const old = useQuery({
    queryKey: queryKey("cod_statement.changed", "detail", id, "head"),
    queryFn: () => unwrap(getCodStatement({ path: { id }, query: { limit: 1 } })),
  });
  const st = old.data?.statement;
  return (
    <div className="box box-err small" role="alert">
      {st === undefined ? (
        <b>Tệp này đã được nhập trước đó.</b>
      ) : (
        <>
          <b>{`Tệp này đã nhập ngày ${vnDay(st.createdAt)}`}</b>
          {` (bảng kê ${accountTitle(st)}, kỳ ${periodText(st)}). Không nhập lần hai.`}
        </>
      )}
      <div className="row" style={{ marginTop: 8 }}>
        <Link className="btn btn-line" to={`/cod/statements/${id}`} onClick={onOpen}>
          Xem bảng kê đó
        </Link>
      </div>
    </div>
  );
}

export function ImportDialog({ onClose, put = xhrPut, accountId, problem: first }: ImportDialogProps) {
  const qc = useQueryClient();
  const abort = useAbortOnUnmount();
  const ids = useId();
  const [account, setAccount] = useState(accountId ?? "");
  const [format, setFormat] = useState("");
  const [file, setFile] = useState<Uploaded | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [over, setOver] = useState(false);
  const [problem, setProblem] = useState<string | null>(first ?? null);
  const [duplicateOf, setDuplicateOf] = useState<string | null>(null);

  const accounts = useQuery({
    queryKey: queryKey("fulfillment.carrier_accounts", "list"),
    queryFn: () => unwrap(listCarrierAccounts({ query: { limit: 100 } })),
  });
  const items = accounts.data?.items ?? [];
  const forbidden = accounts.error instanceof ApiError && accounts.error.status === 403;
  // Chưa chọn (hay tài khoản chọn sẵn không còn trong danh sách): tài khoản đầu tiên.
  const chosen = items.some((a) => a.id === account) ? account : (items[0]?.id ?? "");

  // Ba bước ghi, mỗi bước một khóa chống trùng riêng: xin chỗ trên kho tệp, báo đã tải xong, nhập bảng kê.
  const steps = {
    ticket: useWriteAction((key, body: CreateUploadRequest) => unwrap(createUpload({ body, headers: headers(key) }))),
    done: useWriteAction((key, b: { fileId: string }) =>
      unwrap(completeUpload({ path: { id: b.fileId }, headers: headers(key) })),
    ),
    statement: useWriteAction((key, body: ImportCodStatementRequest) =>
      unwrap(importCodStatement({ body, headers: headers(key) })),
    ),
  };

  const upload = async (f: File): Promise<string | null> => {
    const c = await steps.ticket.run({
      purpose: STATEMENT_PURPOSE,
      fileName: f.name,
      contentType: contentTypeOf(f.name),
      sizeBytes: f.size,
    });
    if (!c.ok) return errorMessage(c.error);
    try {
      await put(c.data.putUrl, c.data.headers, f, abort.current.signal);
    } catch {
      return TEXT_UPLOAD_INCOMPLETE;
    }
    const d = await steps.done.run({ fileId: c.data.fileId });
    if (!d.ok) return errorMessage(d.error);
    setFile({ fileId: c.data.fileId, name: f.name, size: f.size });
    return null;
  };

  const pick = async (f: File | undefined) => {
    if (f === undefined || busy !== null) return;
    setDuplicateOf(null);
    if (!checkLocalFile(f)) {
      setProblem(TEXT_BAD_FILE);
      return;
    }
    setProblem(null);
    setFile(null);
    setBusy(f.name);
    try {
      setProblem(await upload(f));
    } finally {
      setBusy(null);
    }
  };

  const submit = async () => {
    if (file === null || chosen === "") return;
    setProblem(null);
    setDuplicateOf(null);
    const r = await steps.statement.run({
      carrierAccountId: chosen,
      fileId: file.fileId,
      ...(format === "" ? {} : { format }),
    });
    if (r.ok) {
      reloadCod(qc);
      onClose();
      return;
    }
    const e = r.error;
    const old = e instanceof ApiError ? e.details.statementId : undefined;
    if (e instanceof ApiError && e.code === "statement_duplicate" && typeof old === "string") setDuplicateOf(old);
    else setProblem(errorMessage(e));
  };

  // Ô kéo thả: viền đậm khi đang kéo tệp qua; thả thì tải tệp đầu tiên.
  const dropZone = {
    onDragOver: (e: DragEvent) => {
      e.preventDefault();
      setOver(true);
    },
    onDragLeave: () => {
      setOver(false);
    },
    onDrop: (e: DragEvent) => {
      e.preventDefault();
      setOver(false);
      void pick(e.dataTransfer.files[0]);
    },
  };

  const ready = file !== null && chosen !== "" && !forbidden && busy === null && !steps.statement.pending;

  return (
    <Dialog
      title="Nhập bảng kê COD"
      onClose={onClose}
      closeButton
      className="cod-dlg"
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Đóng
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={!ready}
            onClick={() => {
              void submit();
            }}
          >
            Nhập
          </button>
        </>
      }
    >
      {forbidden ? (
        <div className="box box-err small" role="alert">
          {TEXT_NO_ACCOUNTS}
        </div>
      ) : (
        <label className="field">
          <span>Tài khoản hãng *</span>
          <span className="select-wrap">
            <select
              value={chosen}
              disabled={accounts.isPending}
              onChange={(e) => {
                setAccount(e.currentTarget.value);
              }}
            >
              {items.map((a) => (
                <option key={a.id} value={a.id}>
                  {accountLabel(a)}
                </option>
              ))}
            </select>
          </span>
          <span className="hint">Mỗi dòng chỉ khớp với vận đơn của đúng tài khoản này.</span>
        </label>
      )}
      {accounts.isError && !forbidden && (
        <div className="box box-err small" role="alert">
          {errorMessage(accounts.error)}
        </div>
      )}
      <div className={over ? "drop is-over" : "drop"} {...dropZone}>
        <b>{busy === null ? "Kéo tệp vào đây" : `Đang tải lên ${busy}…`}</b>
        <span className="small muted">Excel (.xlsx) hoặc CSV, tối đa 10 MB, 20.000 dòng</span>
        <label className="btn btn-line" htmlFor={`${ids}-tep`}>
          Chọn tệp
        </label>
        <input
          id={`${ids}-tep`}
          type="file"
          accept={ACCEPT}
          hidden
          aria-label="Chọn tệp"
          disabled={busy !== null}
          onChange={(e) => {
            const f = e.currentTarget.files?.[0];
            e.currentTarget.value = "";
            void pick(f);
          }}
        />
      </div>
      {file !== null && (
        <div className="oitem">
          <span className="grow small">
            <b>{file.name}</b>
            {` · ${sizeText(file.size)} · đã tải lên`}
          </span>
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              setFile(null);
            }}
          >
            Bỏ
          </button>
        </div>
      )}
      <label className="field">
        <span>Định dạng</span>
        <span className="select-wrap">
          <select
            value={format}
            onChange={(e) => {
              setFormat(e.currentTarget.value);
            }}
          >
            <option value="">Mặc định của hãng</option>
            <option value={ELIMAZ_FORMAT}>Mẫu chung ELIMAZ</option>
          </select>
        </span>
        <span className="hint">
          Hãng đổi mẫu hoặc tài khoản nhập mã tay: dán số vào mẫu chung ELIMAZ (cột ma_van_don, tien_cod,
          phi_van_chuyen, thuc_nhan; phi_khac, ngay_chuyen tùy chọn).
        </span>
      </label>
      {duplicateOf !== null && <Duplicate id={duplicateOf} onOpen={onClose} />}
      {problem !== null && (
        <div className="box box-err small" role="alert">
          {problem}
        </div>
      )}
    </Dialog>
  );
}
