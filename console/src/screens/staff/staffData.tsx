// Dữ liệu và phần dùng chung giữa danh sách nhân viên và trang sửa quyền (kế hoạch M1 Task 54).
import { useQuery } from "@tanstack/react-query";
import { useEffect, useId, useMemo, useRef, type ReactNode } from "react";
import { getMe, listPermissions, listRoles, listStaff, type Grant } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { permissionKinds, type PermissionKinds } from "./staffRules";

/** Khóa truy vấn danh sách nhân viên; ghi xong thì làm mới khóa này. */
export const STAFF_KEY = queryKey("identity.staff");

export function useStaffData() {
  const staff = useQuery({ queryKey: STAFF_KEY, queryFn: () => unwrap(listStaff()) });
  const roles = useQuery({ queryKey: queryKey("identity.roles"), queryFn: () => unwrap(listRoles()) });
  // Cùng khóa với khung (src/shell/App.tsx) nên không gọi lại /api/v1/me.
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return { staff, roles, me };
}

/**
 * Loại phạm vi của từng quyền để làm mờ vai trò đúng luật máy chủ (canGrant). listPermissions đòi
 * identity.roles.manage (khác identity.users.manage của màn này): không có quyền đó, đang tải hay lỗi → bảng rỗng (luật
 * chặt). Cùng khóa với màn Vai trò (roleData.ts).
 */
export function usePermissionKinds(grants: readonly Grant[]): PermissionKinds {
  const permissions = useQuery({
    queryKey: queryKey("identity.permissions"),
    queryFn: () => unwrap(listPermissions()),
    enabled: hasPermission(grants, "identity.roles.manage"),
    staleTime: Infinity,
  });
  return useMemo(() => permissionKinds(permissions.data), [permissions.data]);
}

// Câu thứ hai của hộp báo khi máy chủ chặn (mockup trạng thái D); câu đầu là câu của mã lỗi.
const REFUSAL_DETAIL: Readonly<Record<string, string>> = {
  last_owner: "Không khóa hay gỡ vai trò Chủ của người Chủ còn hoạt động cuối cùng được.",
  grant_exceeds_actor: "Vai trò này có quyền mà tài khoản của bạn không có.",
  cannot_modify_self: "Nhờ một Chủ khác làm việc này.",
  version_conflict: "Thay đổi của bạn chưa được lưu.",
  role_archived: "Chọn vai trò khác.",
  role_name_taken: "Đặt tên khác.",
  owner_role_locked: "Vai trò Chủ luôn có mọi quyền, kể cả quyền mới thêm sau này.",
  unknown_permission: "Danh sách quyền vừa đổi.",
};
const TEXT_VERSION_CONFLICT = "Quyền của người này vừa được người khác sửa, tải lại";

const sentence = (s: string) => (/[.!?]$/.test(s) ? s : `${s}.`);

export interface RefusalBoxProps {
  error: unknown;
  /** Câu đã có của useWriteAction (gồm câu "đang bận"); null thì dựng từ error. */
  message: string | null;
  /** Có thì hiện nút "Tải lại" khi bị version_conflict. */
  onReload?: () => void;
  /** Câu đầu khi bị version_conflict (mặc định: câu của màn Nhân viên). */
  conflictTitle?: string;
}

/** Hộp báo khi máy chủ chặn một thao tác (mockup trạng thái D). */
export function RefusalBox({ error, message, onReload, conflictTitle = TEXT_VERSION_CONFLICT }: RefusalBoxProps) {
  const code = error instanceof ApiError ? error.code : "";
  const title = code === "version_conflict" ? conflictTitle : (message ?? errorMessage(error));
  const detail = Object.hasOwn(REFUSAL_DETAIL, code) ? REFUSAL_DETAIL[code] : undefined;
  return (
    <div className="box box-err" role="alert">
      <b>{sentence(title)}</b>
      {detail !== undefined && <span> {detail}</span>}
      {code === "version_conflict" && onReload !== undefined && (
        <div className="row" style={{ marginTop: 8 }}>
          <button type="button" className="btn btn-dark" onClick={onReload}>
            Tải lại
          </button>
        </div>
      )}
    </div>
  );
}

export interface DialogProps {
  title: string;
  /** Tấm trượt bên phải (máy tính) thay vì hộp giữa màn; trên điện thoại cả hai là tấm trượt từ đáy. */
  drawer?: boolean;
  /** Có nút × ở đầu hộp. */
  closeButton?: boolean;
  onClose: () => void;
  children: ReactNode;
  footer: ReactNode;
}

/** Hộp thoại theo mockup M1 (.dlg): <dialog> mở bằng showModal nên trình duyệt giữ focus bên trong và đóng bằng Esc. */
export function Dialog({ title, drawer = false, closeButton = false, onClose, children, footer }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });
  useEffect(() => {
    const d = ref.current;
    if (d === null) return;
    if (!d.open) d.showModal();
    const cancel = (e: Event) => {
      e.preventDefault();
      onCloseRef.current();
    };
    d.addEventListener("cancel", cancel);
    return () => {
      d.removeEventListener("cancel", cancel);
      if (d.open) d.close();
    };
  }, []);
  return (
    <dialog ref={ref} className={drawer ? "dlg drawer" : "dlg"} aria-labelledby={titleId}>
      <div className="dlg-h">
        <h3 id={titleId}>{title}</h3>
        {closeButton && (
          <button type="button" className="ibtn" aria-label="Đóng" onClick={onClose}>
            ×
          </button>
        )}
      </div>
      <div className="dlg-b">{children}</div>
      <div className="dlg-f">{footer}</div>
    </dialog>
  );
}
