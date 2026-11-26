// Phần chung của các khối cài đặt (mockup docs/mockups/m4/cai-dat.html; HV52 sửa): mỗi khối có nút Lưu riêng, chấm
// "Chưa lưu" cạnh tên khi có thay đổi chưa lưu, và rời trang khi còn khối chưa lưu thì hỏi lại trên trang ("Bỏ thay
// đổi?" / "Ở lại"). Console dùng React Router chế độ khai báo (không có useBlocker): bấm liên kết nội bộ bị chặn ở pha
// capture của document, đóng tab hay tải lại trang thì trình duyệt hỏi (beforeunload).
import { createContext, use, useCallback, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import { ApiError } from "../../app/errors";
import type { WriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox } from "../staff/staffData";

export const TEXT_CONFLICT = "Có người vừa sửa, tải lại trang";
export const TEXT_NO_EDIT = "Chưa có quyền sửa.";

// --- Khối chưa lưu ---

interface DirtyApi {
  set(id: string, title: string, dirty: boolean): void;
}
const DirtyContext = createContext<DirtyApi | null>(null);

/** Bọc trang cài đặt: nhớ khối nào chưa lưu và hỏi lại khi rời trang. */
export function DirtyGuard({ children }: { children: ReactNode }) {
  const [dirty, setDirty] = useState<ReadonlyMap<string, string>>(new Map());
  const api = useMemo<DirtyApi>(
    () => ({
      set(id, title, d) {
        setDirty((m) => {
          if (d === m.has(id) && (!d || m.get(id) === title)) return m;
          const n = new Map(m);
          if (d) n.set(id, title);
          else n.delete(id);
          return n;
        });
      },
    }),
    [],
  );
  const [leaveTo, setLeaveTo] = useState<string | null>(null);
  const navigate = useNavigate();
  const any = dirty.size > 0;
  const anyRef = useRef(any);
  useEffect(() => {
    anyRef.current = any;
  });

  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      if (!anyRef.current || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
      const t = e.target;
      const a = t instanceof Element ? t.closest("a[href]") : null;
      if (!(a instanceof HTMLAnchorElement) || a.target === "_blank") return;
      const url = new URL(a.href, window.location.href);
      if (url.origin !== window.location.origin) return;
      const to = url.pathname + url.search + url.hash;
      if (to === window.location.pathname + window.location.search + window.location.hash) return;
      e.preventDefault();
      e.stopPropagation();
      setLeaveTo(to);
    };
    const onUnload = (e: BeforeUnloadEvent) => {
      if (anyRef.current) e.preventDefault();
    };
    document.addEventListener("click", onClick, true);
    window.addEventListener("beforeunload", onUnload);
    return () => {
      document.removeEventListener("click", onClick, true);
      window.removeEventListener("beforeunload", onUnload);
    };
  }, []);

  return (
    <DirtyContext value={api}>
      {children}
      {leaveTo !== null && (
        <Dialog
          title="Bỏ thay đổi?"
          onClose={() => {
            setLeaveTo(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  const to = leaveTo;
                  anyRef.current = false;
                  setDirty(new Map());
                  setLeaveTo(null);
                  void navigate(to);
                }}
              >
                Bỏ thay đổi
              </button>
              <button
                type="button"
                className="btn btn-dark"
                onClick={() => {
                  setLeaveTo(null);
                }}
              >
                Ở lại
              </button>
            </>
          }
        >
          <p className="small">Còn khối chưa lưu: {[...dirty.values()].join(", ")}. Rời trang thì thay đổi này mất.</p>
        </Dialog>
      )}
    </DirtyContext>
  );
}

// --- Trạng thái một khối: bản gốc đang sửa (version) và giá trị nhập ---

export interface BlockState<B, I> {
  base: B;
  inputs: I;
  setInputs: (f: (i: I) => I) => void;
  dirty: boolean;
  /** Đặt bản gốc mới (sau khi lưu hay tải lại) và dựng lại ô nhập từ nó. */
  reset: (b: B) => void;
}

/** Bản gốc chỉ đổi khi khối tự lưu hay bấm Tải lại: báo hiệu realtime không đè ô đang sửa. */
export function useBlock<B, I>(server: B, toInputs: (b: B) => I): BlockState<B, I> {
  const [base, setBase] = useState(server);
  const [inputs, setInputsState] = useState(() => toInputs(server));
  const toRef = useRef(toInputs);
  useEffect(() => {
    toRef.current = toInputs;
  });
  const reset = useCallback((b: B) => {
    setBase(b);
    setInputsState(toRef.current(b));
  }, []);
  const setInputs = useCallback((f: (i: I) => I) => {
    setInputsState(f);
  }, []);
  const dirty = JSON.stringify(inputs) !== JSON.stringify(toInputs(base));
  return { base, inputs, setInputs, dirty, reset };
}

// --- Khung một khối ---

export interface BlockProps {
  title: string;
  /** Phần bên phải tên khối (công tắc Bật/Tắt, "Tính năng: …"). */
  aside?: ReactNode;
  dirty: boolean;
  canEdit: boolean;
  pending: boolean;
  /** Lỗi của lần lưu cuối (useWriteAction.error) và câu của nó. */
  error: unknown;
  message: string | null;
  /** Lỗi đã hiện tại ô (không hiện thêm hộp lỗi chung). */
  fieldError?: boolean;
  /** Khối tự thấy dữ liệu trên máy chủ đã đổi so với bản đang sửa. */
  staleLocal?: boolean;
  onSave: () => void;
  onReload: () => void;
  /** Liên kết, nút phụ cạnh nút Lưu. */
  extra?: ReactNode;
  saveLabel?: string;
  children: ReactNode;
}

/** Khối cài đặt: tên, chấm "Chưa lưu", nội dung, hộp lỗi, nút Lưu riêng (khóa khi không có quyền, kèm lý do). */
export function Block(p: BlockProps) {
  const id = useId();
  const dirtyApi = use(DirtyContext);
  useEffect(() => {
    dirtyApi?.set(id, p.title, p.dirty);
  }, [dirtyApi, id, p.title, p.dirty]);
  useEffect(
    () => () => {
      dirtyApi?.set(id, p.title, false);
    },
    // Chỉ khi gỡ khối: bỏ dấu chưa lưu của nó.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  const conflict = p.staleLocal === true || (p.error instanceof ApiError && p.error.code === "version_conflict");
  return (
    <section className="card" aria-labelledby={`${id}-h`}>
      <div className="row-between">
        <h2 id={`${id}-h`}>
          {p.title}
          {p.dirty && (
            <span className="unsaved" role="status">
              {" "}
              · Chưa lưu
            </span>
          )}
        </h2>
        {p.aside}
      </div>
      <div className="stack-sm">
        {p.children}
        {conflict ? (
          <RefusalBox
            error={new ApiError({ status: 409, code: "version_conflict", details: {} })}
            message={null}
            conflictTitle={TEXT_CONFLICT}
            onReload={p.onReload}
          />
        ) : (
          p.error !== null && p.fieldError !== true && <RefusalBox error={p.error} message={p.message} />
        )}
        <div className="card-f">
          {p.extra}
          {p.canEdit ? (
            <button type="button" className="btn btn-dark" disabled={p.pending} onClick={p.onSave}>
              {p.pending ? "Đang lưu…" : (p.saveLabel ?? "Lưu")}
            </button>
          ) : (
            <span className="act">
              <button type="button" className="btn btn-dark" disabled>
                {p.saveLabel ?? "Lưu"}
              </button>
              <span className="reason">{TEXT_NO_EDIT}</span>
            </span>
          )}
        </div>
      </div>
    </section>
  );
}

/** Công tắc Bật/Tắt ở đầu khối (mockup .toggle). */
export function OnOff(p: { label: string; on: boolean; disabled?: boolean; onChange: (on: boolean) => void }) {
  return (
    <button
      type="button"
      className="toggle"
      aria-pressed={p.on}
      aria-label={p.label}
      disabled={p.disabled}
      onClick={() => {
        p.onChange(!p.on);
      }}
    >
      {p.on ? "Bật" : "Tắt"}
    </button>
  );
}

/** Nhóm nút chọn một (mockup .seg). */
export function Seg<T extends string>(p: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  disabled?: boolean;
  onChange: (v: T) => void;
}) {
  return (
    <div className="seg" role="radiogroup" aria-label={p.label}>
      {p.options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          className="tab"
          aria-checked={o.value === p.value}
          disabled={p.disabled}
          onClick={() => {
            p.onChange(o.value);
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Câu đếm cạnh nhãn ô (mockup .cnt; .is-over khi vượt). */
export function Cnt({ text, over = false }: { text: string; over?: boolean }) {
  return <span className={over ? "cnt is-over" : "cnt"}>{text}</span>;
}

/** Ô số nằm giữa câu ("Tự gửi sau [3] giờ …"), lỗi báo ngay dưới (mockup cai-dat.html). */
export function NumField(p: {
  before: string;
  after: string;
  label: string;
  value: string;
  error: string | undefined;
  disabled: boolean;
  onChange: (v: string) => void;
}) {
  return (
    <>
      <div className="row">
        <span className="small">{p.before}</span>
        <input
          className={p.error === undefined ? "input num" : "input num is-err"}
          aria-label={p.label}
          style={{ width: 72 }}
          inputMode="numeric"
          value={p.value}
          disabled={p.disabled}
          {...(p.error === undefined ? {} : { "aria-invalid": true })}
          onChange={(e) => {
            p.onChange(e.target.value);
          }}
        />
        <span className="small">{p.after}</span>
      </div>
      {p.error !== undefined && <span className="field-err">{p.error}</span>}
    </>
  );
}

/** Hộp hỏi lại trước một thao tác (Hủy / nút xác nhận); lỗi của thao tác hiện trong hộp. */
export function ConfirmDialog(p: {
  title: string;
  confirmLabel: string;
  /** Nút xác nhận đỏ (ngắt, xóa). */
  danger?: boolean;
  pending: boolean;
  error?: unknown;
  message?: string | null;
  onCancel: () => void;
  onConfirm: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog
      title={p.title}
      closeButton
      onClose={p.onCancel}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={p.onCancel}>
            Hủy
          </button>
          <button
            type="button"
            className={p.danger === true ? "btn btn-danger" : "btn btn-dark"}
            disabled={p.pending}
            onClick={p.onConfirm}
          >
            {p.confirmLabel}
          </button>
        </>
      }
    >
      {p.children}
      {p.error !== undefined && p.error !== null && <RefusalBox error={p.error} message={p.message ?? null} />}
    </Dialog>
  );
}

/** Hỏi lại trước khi xóa mềm (nhãn, mẫu): xóa xong làm mới danh sách listKey rồi báo onDeleted. */
export function DeleteDialog<T>(p: {
  title: string;
  confirmLabel: string;
  id: string;
  remove: WriteAction<{ id: string }, T>;
  listKey: QueryKey;
  onCancel: () => void;
  onDeleted: () => void;
  children: ReactNode;
}) {
  const qc = useQueryClient();
  return (
    <ConfirmDialog
      title={p.title}
      confirmLabel={p.confirmLabel}
      danger
      pending={p.remove.pending}
      error={p.remove.error}
      message={p.remove.message}
      onCancel={p.onCancel}
      onConfirm={() => {
        void p.remove.run({ id: p.id }).then((r) => {
          if (!r.ok) return;
          void qc.invalidateQueries({ queryKey: p.listKey });
          p.onDeleted();
        });
      }}
    >
      {p.children}
    </ConfirmDialog>
  );
}
