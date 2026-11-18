// Màn Kho — danh sách phiếu kho (kế hoạch M1 Task 328; mockup đã duyệt docs/mockups/m1/kho-phieu.html, trạng thái A).
// Lọc kho, trạng thái, loại gửi thẳng listStockDocuments; máy chủ chỉ trả phiếu của kho người xem được đọc. Danh sách
// API không kèm dòng hay người tạo, nên bảng không có cột "Số lượng" và cột "Tạo" chỉ có giờ (tệp task, Q3).
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import {
  getMe,
  listStockDocuments,
  listWarehouses,
  type StockDocument,
  type StockDocumentKind,
  type StockDocumentStatus,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { TOPICS, queryKey } from "../../app/queryClient";
import { KINDS, KIND_LABEL, STATUSES, STATUS_CHIP, STATUS_LABEL, formatTime } from "./documentRules";
import { WarehouseSelect } from "./WarehouseSelect";
import "./inventory.css";

export const DOCS_PATH = "/kho/phieu";
export const NEW_DOC = "moi";
export const PAGE_SIZE = 50;

const isKind = (v: string): v is StockDocumentKind => KINDS.some((k) => k === v);
const isStatus = (v: string): v is StockDocumentStatus => STATUSES.some((s) => s === v);

export function DocumentList() {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const canCreate = hasPermission(me.data?.grants ?? [], "inventory.documents.create");
  const warehouses = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    queryFn: () => unwrap(listWarehouses()),
  });
  const [warehouseId, setWarehouseId] = useState("");
  const [status, setStatus] = useState<StockDocumentStatus | "">("");
  const [kind, setKind] = useState<StockDocumentKind | "">("");

  // Phiếu đổi khi tồn đổi (ghi sổ): khóa bắt đầu bằng chủ đề số dư để báo hiệu inventory.balance tải lại danh sách.
  const docs = useInfiniteQuery({
    queryKey: queryKey(TOPICS.balance, "documents", { warehouseId, status, kind }),
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(
        listStockDocuments({
          query: {
            limit: PAGE_SIZE,
            ...(warehouseId !== "" && { warehouseId }),
            ...(status !== "" && { status }),
            ...(kind !== "" && { kind }),
            ...(pageParam !== "" && { cursor: pageParam }),
          },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
    placeholderData: keepPreviousData,
  });

  const list = warehouses.data ?? [];
  const rows = docs.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Phiếu kho</h1>
          <p className="sub">
            Phiếu nháp chưa đổi tồn. Chỉ khi ghi sổ tồn kho mới thay đổi; phiếu đã ghi sổ không sửa được.
          </p>
        </div>
        {canCreate && (
          <div className="row">
            <Link className="btn btn-dark" to={`${DOCS_PATH}/${NEW_DOC}`}>
              Tạo phiếu
            </Link>
          </div>
        )}
      </div>

      <div className="toolbar">
        <div className="select-wrap">
          <WarehouseSelect
            ariaLabel="Kho"
            allLabel="Mọi kho"
            warehouses={list}
            value={warehouseId}
            onChange={setWarehouseId}
          />
        </div>
        <div className="select-wrap">
          <select
            aria-label="Trạng thái phiếu"
            value={status}
            onChange={(e) => {
              const v = e.target.value;
              setStatus(isStatus(v) ? v : "");
            }}
          >
            <option value="">Mọi trạng thái</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {STATUS_LABEL[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Loại phiếu"
            value={kind}
            onChange={(e) => {
              const v = e.target.value;
              setKind(isKind(v) ? v : "");
            }}
          >
            <option value="">Mọi loại</option>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {KIND_LABEL[k]}
              </option>
            ))}
          </select>
        </div>
      </div>

      {docs.isError && (
        <div className="box box-err" role="alert" style={{ marginBottom: 16 }}>
          {errorMessage(docs.error)}
        </div>
      )}
      {docs.isPending && <p className="muted">Đang tải…</p>}
      {docs.isSuccess && rows.length === 0 && (
        <div className="empty">
          <h2>{status === "" && kind === "" ? "Chưa có phiếu kho nào" : "Không có phiếu nào khớp"}</h2>
        </div>
      )}

      {rows.length > 0 && (
        <>
          <div className="tbl-frame">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Phiếu</th>
                  <th>Loại</th>
                  <th>Nhà cung cấp</th>
                  <th>Trạng thái</th>
                  <th>Tạo</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((d) => (
                  <DocRow key={d.id} d={d} />
                ))}
              </tbody>
            </table>
          </div>
          {docs.hasNextPage && (
            <div className="more">
              <span>{`Đang hiện ${String(rows.length)} phiếu mới nhất.`}</span>
              <button
                type="button"
                className="btn btn-quiet"
                disabled={docs.isFetchingNextPage}
                onClick={() => void docs.fetchNextPage()}
              >
                {`Tải thêm ${String(PAGE_SIZE)}`}
              </button>
            </div>
          )}
        </>
      )}
    </>
  );
}

function DocRow({ d }: { d: StockDocument }) {
  return (
    <tr className={d.status === "cancelled" ? "is-muted" : undefined}>
      <td className="lead">
        <Link className="bold cell-a" to={`${DOCS_PATH}/${encodeURIComponent(d.id)}`}>
          {d.code}
        </Link>
      </td>
      <td data-label="Loại">{KIND_LABEL[d.kind]}</td>
      <td data-label="Nhà cung cấp" className={d.supplierName === "" ? "muted" : undefined}>
        {d.supplierName === "" ? "—" : <span className="clamp2 sup-name">{d.supplierName}</span>}
      </td>
      <td data-label="Trạng thái">
        <span className={STATUS_CHIP[d.status]}>{STATUS_LABEL[d.status]}</span>
      </td>
      <td data-label="Tạo" className="small">
        {formatTime(d.createdAt)}
      </td>
    </tr>
  );
}
