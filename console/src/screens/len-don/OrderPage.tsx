// Trang /don/moi: đầu trang theo mockup docs/mockups/m2/len-don.html và form lên đơn (OrderForm, dùng lại ở M4).
// Tạo xong form tự trống để lên đơn tiếp (HV02); hộp báo trong form có liên kết mở đơn vừa tạo.
import { OrderForm } from "./OrderForm";

export function OrderPage() {
  return (
    <>
      <div className="page-h">
        <div>
          <h1>Lên đơn</h1>
          <p className="sub">
            Đơn nhập tay: khách gọi điện, nhắn Zalo, Messenger. Khung này dùng lại cạnh hội thoại ở M4.
          </p>
        </div>
        <div className="row keys-desk">
          <span className="kbd">F2</span>
          <span className="small muted">khách</span>
          <span className="kbd">F3</span>
          <span className="small muted">thêm dòng</span>
          <span className="kbd">Ctrl</span>
          <span className="kbd">Enter</span>
          <span className="small muted">lưu và xác nhận</span>
        </div>
      </div>
      <OrderForm onCreated={() => undefined} />
    </>
  );
}
