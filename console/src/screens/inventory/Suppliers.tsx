// Màn Kho — danh bạ nhà cung cấp (kế hoạch M1 Task 327; mockup đã duyệt docs/mockups/m1/kho-nha-cung-cap.html, trạng
// thái A–D). Không xóa, chỉ lưu trữ (phiếu cũ giữ tên). Sửa, lưu trữ gửi version đang xem; người khác vừa sửa →
// version_conflict → hộp "Nhà cung cấp đã thay đổi, tải lại.". Nút ghi chỉ hiện khi có inventory.suppliers.manage; API
// vẫn kiểm từng yêu cầu.
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type SyntheticEvent } from "react";
import { createSupplier, getMe, listSuppliers, updateSupplier, type Supplier, type SupplierStatus } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { Dialog } from "../staff/staffData";
import { SEARCH_DELAY_MS, useDebounced } from "./Balances";
import {
  NAME_MAX,
  NOTE_MAX,
  PHONE_MAX,
  SUPPLIERS_KEY,
  TEXT_BAD_PHONE,
  TEXT_CONFLICT_DETAIL,
  TEXT_CONFLICT_TITLE,
  TEXT_EMPTY,
  TEXT_NAME_TAKEN,
  TEXT_NO_MATCH,
  formatPhone,
  noteCount,
  phoneValid,
} from "./supplierRules";
import "./inventory.css";

const PERM_MANAGE = "inventory.suppliers.manage";
const TEXT_ADD = "Thêm nhà cung cấp";

type Editing = { mode: "new" } | { mode: "edit"; s: Supplier };

const isConflict = (e: unknown) => e instanceof ApiError && e.code === "version_conflict";

/** Hộp báo khi máy chủ chặn (mockup trạng thái C): người khác vừa sửa → câu riêng và nút Tải lại. */
function Refusal({ error, message, onReload }: { error: unknown; message: string | null; onReload: () => void }) {
  if (isConflict(error)) {
    return (
      <div className="box box-err" role="alert">
        <b>{TEXT_CONFLICT_TITLE}</b> <span>{TEXT_CONFLICT_DETAIL}</span>
        <div className="row" style={{ marginTop: 8 }}>
          <button type="button" className="btn btn-dark" onClick={onReload}>
            Tải lại
          </button>
        </div>
      </div>
    );
  }
  return (
    <div className="box box-err" role="alert">
      <b>{message ?? errorMessage(error)}</b>
    </div>
  );
}

export function Suppliers() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const canManage = hasPermission(me.data?.grants ?? [], PERM_MANAGE);
  const [q, setQ] = useState("");
  const [showArchived, setShowArchived] = useState(false);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [archiving, setArchiving] = useState<Supplier | null>(null);
  const term = useDebounced(q.trim(), SEARCH_DELAY_MS);

  const list = useQuery({
    queryKey: [...SUPPLIERS_KEY, "list", { q: term, includeArchived: showArchived }],
    queryFn: () =>
      unwrap(
        listSuppliers({
          query: { ...(term !== "" && { q: term }), ...(showArchived && { includeArchived: true }) },
        }),
      ),
    placeholderData: keepPreviousData,
  });

  const setStatus = useWriteAction((key, b: { id: string; version: number; status: SupplierStatus }) =>
    unwrap(
      updateSupplier({
        path: { id: b.id },
        body: { version: b.version, status: b.status },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const runStatus = async (s: Supplier, status: SupplierStatus) => {
    const r = await setStatus.run({ id: s.id, version: s.version, status });
    setArchiving(null);
    if (r.ok) await qc.invalidateQueries({ queryKey: SUPPLIERS_KEY });
  };

  const rows = list.data ?? [];
  const searching = term !== "";
  const addButton = canManage && (
    <button
      type="button"
      className="btn btn-dark"
      onClick={() => {
        setEditing({ mode: "new" });
      }}
    >
      {TEXT_ADD}
    </button>
  );

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Nhà cung cấp</h1>
          <p className="sub">Dùng trên phiếu nhập. Nhà cung cấp không xóa được, chỉ lưu trữ: phiếu cũ vẫn giữ tên.</p>
        </div>
        {addButton !== false && <div className="row">{addButton}</div>}
      </div>

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          // API chỉ tìm theo tên (bỏ dấu); mockup ghi "tên hoặc số điện thoại" — xem tệp task, Quyết định thay chủ shop.
          placeholder="Tìm theo tên"
          aria-label="Tìm nhà cung cấp"
          maxLength={200}
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
          }}
        />
        <label className="check">
          <input
            type="checkbox"
            checked={showArchived}
            onChange={(e) => {
              setShowArchived(e.target.checked);
            }}
          />{" "}
          Hiện cả đã lưu trữ
        </label>
      </div>

      {setStatus.error !== null && (
        <div style={{ marginBottom: 16 }}>
          <Refusal
            error={setStatus.error}
            message={setStatus.message}
            onReload={() => {
              setStatus.reset();
              void qc.invalidateQueries({ queryKey: SUPPLIERS_KEY });
            }}
          />
        </div>
      )}

      {list.isPending && <p className="muted">Đang tải…</p>}
      {list.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(list.error)}
        </div>
      )}

      {list.isSuccess && rows.length === 0 && (
        <div className="empty">
          <h2>{searching ? TEXT_NO_MATCH : TEXT_EMPTY}</h2>
          {!searching && (
            <>
              <p className="muted">Thêm ở đây, hoặc gõ tên ngay trong ô "Nhà cung cấp" khi soạn phiếu nhập.</p>
              {addButton}
            </>
          )}
        </div>
      )}

      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl stbl">
            <thead>
              <tr>
                <th>Tên</th>
                <th>Điện thoại</th>
                <th>Ghi chú</th>
                <th>Trạng thái</th>
                {canManage && (
                  <th className="c-act">
                    <span className="sr">Thao tác</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <SupplierRow
                  key={s.id}
                  s={s}
                  canManage={canManage}
                  busy={setStatus.pending}
                  onEdit={() => {
                    setEditing({ mode: "edit", s });
                  }}
                  onArchive={() => {
                    setStatus.reset();
                    setArchiving(s);
                  }}
                  onRestore={() => {
                    setStatus.reset();
                    void runStatus(s, "active");
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing !== null && (
        <SupplierDialog
          key={editing.mode === "edit" ? `${editing.s.id}@${String(editing.s.version)}` : "new"}
          editing={editing}
          onReload={async () => {
            if (editing.mode !== "edit") return;
            const r = await list.refetch();
            const s = r.data?.find((x) => x.id === editing.s.id);
            setEditing(s === undefined ? null : { mode: "edit", s });
          }}
          onDone={() => {
            setEditing(null);
          }}
        />
      )}
      {archiving !== null && (
        <Dialog
          title={`Lưu trữ "${archiving.name}"?`}
          onClose={() => {
            setArchiving(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setArchiving(null);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-dark"
                disabled={setStatus.pending}
                onClick={() => void runStatus(archiving, "archived")}
              >
                Lưu trữ
              </button>
            </>
          }
        >
          <p>
            Nhà cung cấp này không còn hiện trong ô chọn của phiếu nhập mới. Phiếu cũ vẫn giữ tên. Dùng lại được bất cứ
            lúc nào.
          </p>
        </Dialog>
      )}
    </>
  );
}

interface SupplierRowProps {
  s: Supplier;
  canManage: boolean;
  busy: boolean;
  onEdit: () => void;
  onArchive: () => void;
  onRestore: () => void;
}

function SupplierRow({ s, canManage, busy, onEdit, onArchive, onRestore }: SupplierRowProps) {
  const archived = s.status === "archived";
  return (
    <tr className={archived ? "is-muted" : undefined}>
      <td className="full">
        <span className="s-name">{s.name}</span>
      </td>
      {s.phone === "" ? (
        <td data-label="Điện thoại" className="muted">
          —
        </td>
      ) : (
        <td data-label="Điện thoại" className="num nowrap">
          {formatPhone(s.phone)}
        </td>
      )}
      {s.note === "" ? (
        <td data-label="Ghi chú" className="muted">
          —
        </td>
      ) : (
        <td data-label="Ghi chú" className="wrap-any">
          {s.note}
        </td>
      )}
      <td data-label="Trạng thái">
        {archived ? (
          <span className="chip chip-line">Đã lưu trữ</span>
        ) : (
          <span className="chip chip-ok">Đang dùng</span>
        )}
      </td>
      {canManage && (
        <td className="c-act">
          <div className="row">
            <button type="button" className="btn btn-quiet" onClick={onEdit}>
              Sửa
            </button>
            {archived ? (
              <button type="button" className="btn btn-quiet" disabled={busy} onClick={onRestore}>
                Dùng lại
              </button>
            ) : (
              <button type="button" className="btn btn-quiet" disabled={busy} onClick={onArchive}>
                Lưu trữ
              </button>
            )}
          </div>
        </td>
      )}
    </tr>
  );
}

interface Form {
  id: string;
  version: number;
  name: string;
  phone: string;
  note: string;
}

function SupplierDialog(p: { editing: Editing; onReload: () => Promise<void>; onDone: () => void }) {
  const qc = useQueryClient();
  const formId = useId();
  const nameId = useId();
  const phoneId = useId();
  const noteId = useId();
  const nameErrId = useId();
  const phoneErrId = useId();
  const old = p.editing.mode === "edit" ? p.editing.s : null;
  const [name, setName] = useState(old?.name ?? "");
  const [phone, setPhone] = useState(old?.phone ?? "");
  const [note, setNote] = useState(old?.note ?? "");
  const [phoneBad, setPhoneBad] = useState(false);
  const save = useWriteAction((key, b: Form) => {
    const body = { name: b.name, phone: b.phone, note: b.note };
    const headers = { "Idempotency-Key": key };
    return b.id === ""
      ? unwrap(createSupplier({ body, headers }))
      : unwrap(updateSupplier({ path: { id: b.id }, body: { ...body, version: b.version }, headers }));
  });
  const nameTaken = save.error instanceof ApiError && save.error.code === "supplier_name_taken";

  const submit = async (e: SyntheticEvent) => {
    e.preventDefault();
    if (!phoneValid(phone)) {
      setPhoneBad(true);
      return;
    }
    const r = await save.run({
      id: old?.id ?? "",
      version: old?.version ?? 0,
      name: name.trim(),
      phone: phone.trim(),
      note: note.trim(),
    });
    if (!r.ok) return;
    await qc.invalidateQueries({ queryKey: SUPPLIERS_KEY });
    p.onDone();
  };

  return (
    <Dialog
      title={old === null ? TEXT_ADD : "Sửa nhà cung cấp"}
      drawer
      closeButton
      onClose={p.onDone}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={p.onDone}>
            Hủy
          </button>
          <button type="submit" form={formId} className="btn btn-dark" disabled={save.pending}>
            Lưu
          </button>
        </>
      }
    >
      <form id={formId} className="stack" onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor={nameId}>Tên *</label>
          <input
            id={nameId}
            className={nameTaken ? "input is-err" : "input"}
            required
            maxLength={NAME_MAX}
            value={name}
            aria-invalid={nameTaken}
            aria-describedby={nameTaken ? nameErrId : undefined}
            onChange={(e) => {
              setName(e.target.value);
              if (nameTaken) save.reset();
            }}
          />
          {nameTaken && (
            <span id={nameErrId} className="field-err">
              {TEXT_NAME_TAKEN}
            </span>
          )}
          <span className="hint">Tối đa 200 ký tự</span>
        </div>
        <div className="field">
          <label htmlFor={phoneId}>Điện thoại</label>
          <input
            id={phoneId}
            className={phoneBad ? "input is-err num" : "input num"}
            type="tel"
            inputMode="tel"
            maxLength={PHONE_MAX}
            value={phone}
            aria-invalid={phoneBad}
            aria-describedby={phoneBad ? phoneErrId : undefined}
            onChange={(e) => {
              setPhone(e.target.value);
              setPhoneBad(false);
            }}
          />
          {phoneBad && (
            <span id={phoneErrId} className="field-err">
              {TEXT_BAD_PHONE}
            </span>
          )}
        </div>
        <div className="field">
          <label htmlFor={noteId}>Ghi chú</label>
          <textarea
            id={noteId}
            className="input"
            maxLength={NOTE_MAX}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />
          <span className="hint">{noteCount(note)}</span>
        </div>
        {save.error !== null && !nameTaken && (
          <Refusal error={save.error} message={save.message} onReload={() => void p.onReload()} />
        )}
      </form>
    </Dialog>
  );
}
