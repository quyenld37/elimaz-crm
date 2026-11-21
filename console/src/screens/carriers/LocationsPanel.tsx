// Thẻ "Địa giới của hãng" trên trang sửa tài khoản (mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái C). Mã
// địa giới của CRM chưa có mã của hãng (listUnmappedLocations, lọc theo hãng; API chưa có mốc "đồng bộ lần cuối", Task
// 131 để lại) và nút "Đồng bộ địa giới" (syncCarrierLocations, việc nền; quyền manage). CRM không đoán mã gần đúng.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { listAdminUnits, listUnmappedLocations, syncCarrierLocations, type CarrierAccount } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { RefusalBox } from "../staff/staffData";
import { QueryState } from "./QueryState";
import { CARRIER_ACCOUNT_TOPIC } from "./carrierRules";

const LEVEL_TEXT = { province: "Tỉnh", ward: "Xã/phường" } as const;
const SHOWN = 50;

export function LocationsPanel({
  account,
  manage,
  canReadUnits,
}: {
  account: CarrierAccount;
  manage: boolean;
  canReadUnits: boolean;
}) {
  const id = useId();
  const qc = useQueryClient();
  const unmappedKey = queryKey(CARRIER_ACCOUNT_TOPIC, "unmapped", account.carrierKey);
  const q = useQuery({
    queryKey: unmappedKey,
    queryFn: () => unwrap(listUnmappedLocations({ query: { carrier: account.carrierKey, limit: SHOWN } })),
  });
  const provinces = useQuery({
    queryKey: queryKey("customer.admin-units", "provinces"),
    enabled: canReadUnits,
    queryFn: () => unwrap(listAdminUnits({})),
    staleTime: Number.POSITIVE_INFINITY,
  });
  const provinceName = (code: string | undefined) =>
    code === undefined ? "" : (provinces.data?.items.find((p) => p.code === code)?.name ?? code);
  const [sent, setSent] = useState(false);
  const sync = useWriteAction((key, accountId: string) =>
    unwrap(syncCarrierLocations({ path: { id: accountId }, headers: { "Idempotency-Key": key } })),
  );
  const items = q.data?.items ?? [];
  const more = q.data?.nextCursor !== undefined && q.data.nextCursor !== "";
  return (
    <section className="card" aria-labelledby={id}>
      <div className="row-between">
        <h2 id={id}>Địa giới của hãng</h2>
        {manage && account.status !== "revoked" && (
          <button
            type="button"
            className="btn btn-line"
            disabled={sync.pending}
            onClick={() => {
              void sync.run(account.id).then((r) => {
                setSent(r.ok);
                if (r.ok) void qc.invalidateQueries({ queryKey: unmappedKey });
              });
            }}
          >
            Đồng bộ địa giới
          </button>
        )}
      </div>
      <QueryState q={q} />
      {q.isSuccess && items.length === 0 && <p className="small">Mọi địa giới của CRM đã khớp mã của hãng.</p>}
      {q.isSuccess && items.length > 0 && (
        <>
          <p className="small">
            <b className="err-text">{`${more ? "Hơn " : ""}${String(items.length)} địa giới chưa khớp`}</b>: đơn có địa
            chỉ ở đây không báo phí, không gửi được qua hãng này.
          </p>
          <div className="tbl-frame" style={{ marginTop: 8 }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>Cấp</th>
                  <th>Mã CRM</th>
                  <th>Tên</th>
                  <th>Thuộc</th>
                </tr>
              </thead>
              <tbody>
                {items.map((u) => (
                  <tr key={`${u.level}-${u.ourCode}`}>
                    <td className="lead">{LEVEL_TEXT[u.level]}</td>
                    <td data-label="Mã CRM" className="num">
                      {u.ourCode}
                    </td>
                    <td data-label="Tên">{u.name}</td>
                    <td data-label="Thuộc">{provinceName(u.parentCode)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {sent && (
        <p className="small" role="status">
          Đã gửi việc đồng bộ. Danh sách tự cập nhật khi xong.
        </p>
      )}
      {sync.error !== null && <RefusalBox error={sync.error} message={sync.message} />}
      <p className="hint">
        Danh sách tự cập nhật sau mỗi lần đồng bộ. CRM không đoán mã gần đúng; báo cho hãng hoặc gửi bằng tài khoản
        khác.
      </p>
    </section>
  );
}
