// Phần dùng chung của các màn nội dung website "sửa cả danh sách rồi lưu theo revision" (Banner trang chủ Task 459,
// Bộ phối Task 460; HV19 của docs/mockups/m5): thanh lưu, hộp người khác vừa lưu (409) và tải bản mới, ô chữ có bộ
// đếm, bước của form, ô giờ Việt Nam. Hai màn đổi cùng nhau khi mockup HV19 đổi.
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { shortTime } from "../../app/time";
import { isoToVnLocal, vnLocalToIso } from "../ma-giam-gia/couponRules";
import { runeCount } from "../web-campaigns/campaignRules";
import { Dialog } from "../staff/staffData";

/** Ô chữ có bộ đếm "n / max" (đếm theo ký tự như máy chủ); hint nối sau bộ đếm; area = ô nhiều dòng. */
export function Counted(p: {
  label: string;
  /** Chữ hiện trên ô (mặc định là label). */
  caption?: ReactNode;
  value: string;
  max: number;
  placeholder?: string;
  hint?: string;
  area?: boolean;
  /** Khóa ô (ví dụ khi đang lưu). */
  disabled?: boolean;
  onChange: (v: string) => void;
}) {
  const common = {
    className: "input",
    "aria-label": p.label,
    value: p.value,
    maxLength: p.max,
    placeholder: p.placeholder,
    disabled: p.disabled,
    onChange: (e: { target: { value: string } }) => {
      p.onChange(e.target.value);
    },
  };
  return (
    <label className="field">
      <span>{p.caption ?? p.label}</span>
      {p.area === true ? <textarea {...common} /> : <input {...common} />}
      <span className="hint num">
        {runeCount(p.value)} / {p.max}
        {p.hint === undefined ? "" : ` · ${p.hint}`}
      </span>
    </label>
  );
}

/** Một bước có số của form (đường kẻ trên, nhãn đậm). */
export function Step({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="step">
      <p className="label">{label}</p>
      {children}
    </div>
  );
}

/** Ô giờ theo giờ Việt Nam (datetime-local); trống hay sai dạng → undefined. */
export function VnTimeInput({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string | undefined;
  onChange: (iso: string | undefined) => void;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input
        className="input num"
        type="datetime-local"
        value={value === undefined ? "" : isoToVnLocal(value)}
        onChange={(e) => {
          onChange(vnLocalToIso(e.target.value) ?? undefined);
        }}
      />
    </label>
  );
}

/**
 * Thanh lưu dính đáy (hiện khi visible: có thay đổi hay đang lưu): trạng thái, lý do khóa Lưu, Bỏ thay đổi, Lưu; dưới
 * thanh là lỗi của lần lưu (error) hay câu đã lưu (done).
 */
export function SaveBar(p: {
  visible: boolean;
  error: string | null;
  done: string | null;
  saving: boolean;
  locked: boolean;
  /** Lý do nút Lưu khóa; null khi lưu được. */
  blocked: string | null;
  saveLabel: string;
  onDiscard: () => void;
  onSave: () => void;
}) {
  return (
    <>
      {p.visible && (
        <div className="savebar is-sticky">
          <span className="small grow">
            {p.saving ? "Đang lưu…" : "Có thay đổi chưa lưu"}
            {p.blocked !== null && (
              <>
                {" · "}
                <span className="muted">{p.blocked}</span>
              </>
            )}
          </span>
          <button type="button" className="btn btn-quiet" disabled={p.locked} onClick={p.onDiscard}>
            Bỏ thay đổi
          </button>
          <button type="button" className="btn btn-dark" disabled={p.locked || p.blocked !== null} onClick={p.onSave}>
            {p.saving ? "Đang lưu…" : p.saveLabel}
          </button>
        </div>
      )}
      {p.error !== null && <div className="box box-err">{p.error}</div>}
      {p.done !== null && <p className="box box-ok">{p.done}</p>}
    </>
  );
}

/**
 * Hộp 409 (HV19): người khác vừa lưu; "Giữ bản đang sửa" đóng hộp, giữ bản (lần lưu sau vẫn 409), "Tải bản mới" bỏ bản
 * sửa và nạp bản máy chủ. subject là tên danh sách trong câu ("danh sách banner").
 */
/**
 * Câu "<tên> lưu lúc <giờ>." của hộp 409 (mockup HV19, Task 488): chỉ khi bản mới nhất đã đọc (server) khác bản đang
 * sửa dựa vào (base) và có giờ lưu; không có tên thì chỉ giờ.
 */
export function savedByText(
  server: { revision: number; updatedAt?: string; updatedByName?: string },
  base: { revision: number },
): string | null {
  if (server.revision === base.revision || server.updatedAt === undefined) return null;
  const at = shortTime(server.updatedAt);
  return server.updatedByName === undefined ? `Bản mới lưu lúc ${at}.` : `${server.updatedByName} lưu lúc ${at}.`;
}

export function ConflictDialog(p: {
  title: string;
  subject: string;
  /** Bản mới nhất đã đọc và bản đang sửa dựa vào: khác nhau thì hộp nêu ai lưu, lúc nào (savedByText). */
  server: { revision: number; updatedAt?: string; updatedByName?: string };
  base: { revision: number };
  reloading: boolean;
  onKeep: () => void;
  onReload: () => unknown;
}) {
  return (
    <Dialog
      title={p.title}
      onClose={p.onKeep}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={p.onKeep}>
            Giữ bản đang sửa
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={p.reloading}
            onClick={() => {
              void p.onReload();
            }}
          >
            Tải bản mới
          </button>
        </>
      }
    >
      <p>
        {savedByText(p.server, p.base) ?? `Người khác vừa lưu ${p.subject}.`} Bản bạn đang sửa <b>vẫn còn</b> trên màn
        hình, chưa lưu.
      </p>
      <p className="small muted">Tải bản mới sẽ bỏ thay đổi của bạn. Giữ bản đang sửa để chép lại nội dung trước.</p>
    </Dialog>
  );
}

/** Tải bản mới nhất của danh sách (bỏ bộ nhớ đệm) cho nút "Tải bản mới"; đọc lỗi thì giữ hộp để bấm lại. */
export function useReload<T>(key: QueryKey, read: () => Promise<T>, onFresh: (fresh: T) => void) {
  const qc = useQueryClient();
  const [reloading, setReloading] = useState(false);
  const reload = async () => {
    setReloading(true);
    try {
      onFresh(await qc.query({ queryKey: key, staleTime: 0, queryFn: read }));
    } catch {
      // Đọc lỗi: giữ hộp để bấm lại.
    } finally {
      setReloading(false);
    }
  };
  return { reloading, reload };
}
