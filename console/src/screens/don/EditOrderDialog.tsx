// Sửa đơn (mockup docs/mockups/m2/don-chi-tiet.html; HV23, HV24): mở lại form Lên đơn (OrderForm, chế độ sửa) điền
// sẵn từ đơn như đã đọc lúc mở. Báo hiệu order.changed trong lúc sửa không đổi phần đang gõ (form giữ bản lúc mở); lưu
// gặp 409 version_conflict thì form giữ phần đang gõ, "Tải lại đơn" đọc bản mới và dựng lại form từ bản đó.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getOrder, type Order } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { OrderForm } from "../len-don/OrderForm";
import { Dialog } from "../staff/staffData";
import { entityMeta, orderKey, reloadOrder } from "./orderData";

export function EditOrderDialog({ order, onClose }: { order: Order; onClose: () => void }) {
  const qc = useQueryClient();
  const [snap, setSnap] = useState(order);
  const [round, setRound] = useState(0);

  const [reloadError, setReloadError] = useState<string | null>(null);
  const reload = async () => {
    setReloadError(null);
    let fresh: Order;
    try {
      fresh = await qc.query({
        queryKey: orderKey(order.id),
        meta: entityMeta(order.id),
        staleTime: 0,
        queryFn: () => unwrap(getOrder({ path: { id: order.id } })),
      });
    } catch (e) {
      setReloadError(errorMessage(e));
      return;
    }
    setSnap(fresh);
    setRound((r) => r + 1);
  };

  return (
    <Dialog
      title={`Sửa đơn ${order.code}`}
      className="dlg-form"
      onClose={onClose}
      footer={
        <button type="button" className="btn btn-line" onClick={onClose}>
          Đóng
        </button>
      }
    >
      {reloadError !== null && <div className="box box-err">{reloadError}</div>}
      <OrderForm
        key={`${String(snap.version ?? 0)}-${String(round)}`}
        edit={{
          order: snap,
          onSaved: (o) => {
            reloadOrder(qc, o.id);
            onClose();
          },
          onReload: () => {
            void reload();
          },
        }}
      />
    </Dialog>
  );
}
