// Phần chung của các hộp thoại trong khối Vận chuyển sau khi gửi (Task 134): chân hộp (Đóng + nút chính mờ khi còn lý
// do), dòng lý do mờ và câu lỗi của lần ghi cuối.

/** Chân hộp: "Đóng" và nút chính; why khác null thì nút mờ, lý do ở title. */
export function DialogActions(props: {
  label: string;
  danger?: boolean;
  why: string | null;
  pending: boolean;
  onSubmit: () => void;
  onClose: () => void;
}) {
  return (
    <>
      <button type="button" className="btn btn-quiet" onClick={props.onClose}>
        Đóng
      </button>
      <button
        type="button"
        className={props.danger === true ? "btn btn-danger" : "btn btn-dark"}
        disabled={props.why !== null || props.pending}
        title={props.why ?? undefined}
        onClick={props.onSubmit}
      >
        {props.label}
      </button>
    </>
  );
}

/** Lý do nút chính đang mờ và câu lỗi của lần ghi cuối (nếu có). */
export function DialogNotes({ why, message }: { why: string | null; message: string | null }) {
  return (
    <>
      {why !== null && <p className="reason">{why}</p>}
      {message !== null && <div className="box box-err small">{message}</div>}
    </>
  );
}
