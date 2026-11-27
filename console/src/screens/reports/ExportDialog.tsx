// Khung chung của hai hộp xuất (Task 241, M6-18): "Xuất danh sách sản phẩm" và "Xuất danh sách khách". Hộp có nút Hủy /
// Xuất; xuất xong hiện "Đang tạo tệp, xem ở Tệp xuất" kèm "Mở Tệp xuất" và nút Đóng; lỗi của lần gửi hiện dưới ô.
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { Dialog } from "../staff/staffData";
import { EXPORTS_PATH } from "./ExportButton";
import { RunNotice } from "./RunNotice";

export interface ExportDialogProps {
  title: string;
  /** Đang gửi hay dữ liệu chưa hợp lệ: khóa nút Xuất. */
  disabled: boolean;
  /** Lỗi của lần gửi cuối (câu tiếng Việt). */
  error: string | null;
  /** Đã gửi xong (máy nhận yêu cầu). */
  done: boolean;
  onSubmit: () => void;
  onClose: () => void;
  children: ReactNode;
}

export function ExportDialog({ title, disabled, error, done, onSubmit, onClose, children }: ExportDialogProps) {
  return (
    <Dialog
      title={title}
      closeButton
      onClose={onClose}
      footer={
        <>
          {!done && (
            <button type="button" className="btn btn-line" onClick={onClose}>
              Hủy
            </button>
          )}
          {done ? (
            <button type="button" className="btn btn-dark" onClick={onClose}>
              Đóng
            </button>
          ) : (
            <button type="button" className="btn btn-dark" disabled={disabled} onClick={onSubmit}>
              Xuất
            </button>
          )}
        </>
      }
    >
      {done ? (
        <RunNotice
          text="Đang tạo tệp, xem ở Tệp xuất"
          action={
            <Link className="btn btn-line" to={EXPORTS_PATH}>
              Mở Tệp xuất
            </Link>
          }
        />
      ) : (
        <div className="stack">
          {children}
          {error !== null && (
            <p className="field-err" role="alert">
              {error}
            </p>
          )}
        </div>
      )}
    </Dialog>
  );
}

/** Nút mở hộp: giữ trạng thái mở/đóng; `dialog(close)` dựng hộp. */
export function ExportLauncher({ label, dialog }: { label: string; dialog: (close: () => void) => ReactNode }) {
  const [open, setOpen] = useState(false);
  const close = () => {
    setOpen(false);
  };
  return (
    <>
      <button
        type="button"
        className="btn btn-line"
        onClick={() => {
          setOpen(true);
        }}
      >
        {label}
      </button>
      {open && dialog(close)}
    </>
  );
}
