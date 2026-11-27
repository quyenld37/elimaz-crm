// Số đếm cạnh mục menu của ba danh sách vận hành (HV02 của mockup M3): cùng nguồn với thanh tab, tải lại theo báo hiệu.
// Chưa đọc được số (đang tải, lỗi, thiếu quyền fulfillment.read) → không vẽ gì.
import { NavCount } from "../../shared";
import { useOpsCounts } from "./opsData";

export function OpsNavCount(props: { which: "callbacks" | "stuck" | "review" }) {
  const c = useOpsCounts().data;
  if (c === undefined) return null;
  if (props.which === "callbacks") return <NavCount n={c.callbacksOpen} late={c.callbacksOverdue} />;
  return <NavCount n={props.which === "stuck" ? c.stuck : c.review} />;
}
