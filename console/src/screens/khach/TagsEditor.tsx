// Thẻ "Nhãn" của hồ sơ khách (mockup khach-ho-so.html trạng thái A; HV28): nhãn gắn tay có nút × (removeCustomerTag),
// nhãn theo quy tắc ghi "theo quy tắc" và không có nút gỡ (quy tắc tự gắn, tự gỡ); ô "+ Thêm nhãn" có các nhãn chưa gắn tay.
// Máy chủ trả mỗi nhãn một dòng: nguồn "staff" khi có ít nhất một dòng gắn tay (queries/customer/tags.sql
// ListGroupTags), nên nhãn đang theo quy tắc vẫn gắn tay thêm được; gỡ tay chỉ gỡ dòng gắn tay. Cần customer.write.
import { useQueryClient } from "@tanstack/react-query";
import { addCustomerTag, removeCustomerTag, type Customer } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { headers, reloadCustomers, useTags } from "./customerData";
import { tagChip } from "./customerRules";

interface TagBody {
  id: string;
  tagId: string;
  add: boolean;
}

export function TagsEditor({ customer, canWrite }: { customer: Customer; canWrite: boolean }) {
  const qc = useQueryClient();
  const tags = useTags();
  const write = useWriteAction((key, b: TagBody) => {
    const opts = { path: { id: b.id, tagId: b.tagId }, headers: headers(key) };
    return unwrap(b.add ? addCustomerTag(opts) : removeCustomerTag(opts));
  });
  const run = async (tagId: string, add: boolean) => {
    const r = await write.run({ id: customer.rootId, tagId, add });
    if (r.ok) reloadCustomers(qc);
  };
  const linked = new Set(customer.tags.filter((l) => l.source === "staff").map((l) => l.tag.id));
  const free = (tags.data?.items ?? []).filter((t) => !linked.has(t.id));
  const staff = customer.tags.filter((l) => l.source === "staff");
  const rule = customer.tags.filter((l) => l.source === "rule");

  return (
    <div className="card">
      <h2>Nhãn</h2>
      <div className="p-tags">
        {staff.map((l) => (
          <span key={`s-${l.tag.id}`} className="tag">
            {l.tag.name}
            {canWrite && (
              <button
                type="button"
                aria-label={`Gỡ nhãn ${l.tag.name}`}
                disabled={write.pending}
                onClick={() => {
                  void run(l.tag.id, false);
                }}
              >
                ×
              </button>
            )}
          </span>
        ))}
        {rule.map((l) => (
          <span key={`r-${l.tag.id}`} className={tagChip(l.tag.color)}>
            {`${l.tag.name} · theo quy tắc`}
          </span>
        ))}
        {customer.tags.length === 0 && <span className="muted">Chưa có nhãn</span>}
        {canWrite && free.length > 0 && (
          <span className="select-wrap">
            <select
              aria-label="Thêm nhãn"
              value=""
              disabled={write.pending}
              onChange={(e) => {
                if (e.target.value !== "") void run(e.target.value, true);
              }}
            >
              <option value="">+ Thêm nhãn</option>
              {free.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </span>
        )}
      </div>
      {write.message !== null && (
        <p className="err-text" role="alert">
          {write.message}
        </p>
      )}
      <p className="hint" style={{ marginTop: 8 }}>
        Nhãn theo quy tắc tự gắn và tự gỡ khi chỉ số đổi; không gỡ tay được.
      </p>
    </div>
  );
}
