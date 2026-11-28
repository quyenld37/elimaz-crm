// Mảnh giao diện dùng chung của hai màn M7 (Kiểm kho mở sổ, Hàng hoàn của đơn cũ): hộp lỗi, hộp thử lại, chân hộp xác nhận.
import { useEffect, useRef, type ReactNode } from "react";
import { errorMessage } from "../../app/errors";

export function ErrorBox(props: { error: unknown; text?: string }) {
  return (
    <div className="box box-err" role="alert">
      {props.text ?? errorMessage(props.error)}
    </div>
  );
}

/** Lỗi tải một danh sách: câu mở đầu, lý do của máy chủ, nút Thử lại. */
export function RetryBox(props: { title: string; error: unknown; onRetry: () => void }) {
  return (
    <div className="box box-err" role="alert">
      <b>{props.title}</b> <span>{errorMessage(props.error)}</span>
      <div className="row" style={{ marginTop: 8 }}>
        <button type="button" className="btn btn-line" onClick={props.onRetry}>
          Thử lại
        </button>
      </div>
    </div>
  );
}

/** Chân hộp xác nhận: "Quay lại" và nút chính. */
export function ConfirmFooter(props: {
  onBack: () => void;
  onConfirm: () => void;
  pending: boolean;
  label: ReactNode;
  danger?: boolean;
}) {
  return (
    <>
      <button type="button" className="btn btn-line" onClick={props.onBack}>
        Quay lại
      </button>
      <button
        type="button"
        className={props.danger === true ? "btn btn-danger" : "btn btn-dark"}
        disabled={props.pending}
        onClick={props.onConfirm}
      >
        {props.label}
      </button>
    </>
  );
}

/** Chạy `fn` sau `ms` mili giây kể từ lần gọi cuối (gộp nhiều lần gọi nhanh thành một); hủy khi màn đóng. */
export function useLater(fn: () => void, ms: number): () => void {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const last = useRef(fn);
  useEffect(() => {
    last.current = fn;
  });
  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );
  return () => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      last.current();
    }, ms);
  };
}
