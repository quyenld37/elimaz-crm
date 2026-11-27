// Danh sách khách /khach (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-danh-sach.html trạng thái A–C; HV26).
// Mỗi dòng là một khách gốc (các hồ sơ đã gộp là một dòng). Tìm cùng luật như ô tìm khách ở Lên đơn (customerQuery:
// mã KH, SĐT đủ hay 4 số cuối trở lên, tên không dấu); đuôi SĐT khớp được đánh dấu. Tải thêm theo con trỏ.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listCustomers, type CustomerListItem, type CustomerPage } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { customerQuery, displayPhone, shortDate } from "../len-don/orderRules";
import { formatMoney } from "../products/productRules";
import { CreateCustomerDialog } from "./CreateCustomerDialog";
import { CUSTOMER_TOPIC, usePerms, useTags } from "./customerData";
import { markTail } from "./customerRules";
import { ExportCustomersButton } from "./ExportCustomersButton";
import { ImportDialog } from "./ImportDialog";
import { TagsDialog } from "./TagRuleEditor";
import "../don/don.css";
import "./khach.css";

const PAGE = 50;

const MATCH_TEXT: Readonly<Record<NonNullable<CustomerListItem["matchedBy"]>, string>> = {
  code: "Mã khách",
  phone: "SĐT",
  phone_tail: "Đuôi SĐT",
  name: "Tên",
};

const digits = (s: string) => s.replace(/\D/g, "");

/** SĐT hiện trên dòng: số khớp lần tìm (nếu có) với đuôi đã khớp được đánh dấu; không thì số đầu tiên. */
function Phone({ c, q }: { c: CustomerListItem; q: string }) {
  const byPhone = c.matchedBy === "phone" || c.matchedBy === "phone_tail";
  const want = digits(q);
  const phone = (byPhone ? c.phones.find((p) => digits(p).endsWith(want)) : undefined) ?? c.phones[0];
  if (phone === undefined) return <span className="muted">Chưa có</span>;
  const shown = displayPhone(phone);
  if (!byPhone) return <>{shown}</>;
  const [head, tail] = markTail(shown, c.matchedBy === "phone" ? digits(shown).length : want.length);
  return (
    <>
      {head}
      <mark>{tail}</mark>
    </>
  );
}

type Open = "tags" | "import" | "create" | null;

export function CustomerList() {
  const perms = usePerms();
  const tags = useTags();
  const [text, setText] = useState("");
  const [tagId, setTagId] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const tagOptions = (tags.data?.items ?? []).map((t) => (
    <option key={t.id} value={t.id}>
      {t.name}
    </option>
  ));
  const typed = customerQuery(text);
  const q = useDebounced(typed.kind === "query" ? typed.q : "", SEARCH_DELAY_MS);

  const query = { ...(q === "" ? {} : { q }), ...(tagId === "" ? {} : { tagId }), limit: PAGE };
  const list = useInfiniteQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "list", query),
    initialPageParam: "",
    getNextPageParam: (last: CustomerPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listCustomers({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const canImport = perms.can("customer.import") && perms.can("files.upload");
  const canCreate = perms.can("customer.write");

  const actions = (
    <>
      {canImport && (
        <button
          type="button"
          className="btn btn-line"
          onClick={() => {
            setOpen("import");
          }}
        >
          Nhập Excel
        </button>
      )}
      <ExportCustomersButton />
      {canCreate && (
        <button
          type="button"
          className="btn btn-dark"
          onClick={() => {
            setOpen("create");
          }}
        >
          Thêm khách
        </button>
      )}
    </>
  );

  return (
    <div>
      <div className="page-h">
        <div>
          <h1>Khách hàng</h1>
          <p className="sub">Mỗi dòng là một khách (các hồ sơ đã gộp tính là một)</p>
        </div>
        <div className="row">
          {perms.can("customer.tags.manage") && (
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                setOpen("tags");
              }}
            >
              Nhãn
            </button>
          )}
          {actions}
        </div>
      </div>

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="4 số cuối SĐT trở lên, tên không dấu hoặc mã KH"
          aria-label="Tìm khách"
          maxLength={100}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <div className="select-wrap">
          <select
            aria-label="Nhãn"
            value={tagId}
            onChange={(e) => {
              setTagId(e.target.value);
            }}
          >
            <option value="">Mọi nhãn</option>
            {tagOptions}
          </select>
        </div>
      </div>
      {typed.kind === "short" && (
        <p className="field-err" style={{ marginBottom: 12 }}>
          {typed.text}
        </p>
      )}

      {list.isError && <div className="box box-err">{errorMessage(list.error)}</div>}
      {list.isSuccess && rows.length === 0 && (
        <div className="empty">
          {q === "" && tagId === "" ? (
            <>
              <h2>Chưa có khách nào</h2>
              <p className="muted">Khách tự có khi lên đơn, hoặc nhập từ Excel.</p>
              <div className="row">{actions}</div>
            </>
          ) : q === "" ? (
            <h2>Không có khách nào mang nhãn này</h2>
          ) : (
            <>
              <h2>{`Không tìm thấy "${q}"`}</h2>
              <p className="muted">Tên tìm không dấu, đủ mọi từ. Thử tìm bằng 4 số cuối SĐT.</p>
            </>
          )}
        </div>
      )}
      {rows.length > 0 && (
        <>
          <div className="tbl-frame">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Khách</th>
                  <th>SĐT</th>
                  {q !== "" && <th>Khớp theo</th>}
                  <th className="r">Đơn</th>
                  <th className="r">Đã chi</th>
                  <th>Mua gần nhất</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((c) => (
                  <tr key={c.id}>
                    <td className="lead">
                      <div className="c-cus">
                        <Link className="cell-a bold clamp2" to={`/khach/${c.id}`}>
                          {c.fullName}
                        </Link>
                        <span className="small muted num">{c.code}</span>
                      </div>
                    </td>
                    <td data-label="SĐT" className="num">
                      <Phone c={c} q={q} />
                    </td>
                    {q !== "" && (
                      <td data-label="Khớp theo">{c.matchedBy === undefined ? "—" : MATCH_TEXT[c.matchedBy]}</td>
                    )}
                    <td data-label="Đơn" className="r num">
                      {c.stats.orderCount}
                    </td>
                    <td data-label="Đã chi" className="r">
                      <span className="money">{formatMoney(c.stats.totalSpent)}</span>
                    </td>
                    <td data-label="Mua gần nhất" className={c.stats.lastOrderAt === undefined ? "muted" : "num"}>
                      {c.stats.lastOrderAt === undefined ? "—" : shortDate(c.stats.lastOrderAt)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="more">
            <span>{q === "" ? `${String(rows.length)} khách mới nhất` : `${String(rows.length)} khách khớp`}</span>
            {list.hasNextPage && (
              <button
                type="button"
                className="btn btn-line"
                disabled={list.isFetchingNextPage}
                onClick={() => {
                  void list.fetchNextPage();
                }}
              >
                Tải thêm
              </button>
            )}
          </div>
        </>
      )}

      {open === "tags" && (
        <TagsDialog
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "import" && (
        <ImportDialog
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "create" && (
        <CreateCustomerDialog
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
    </div>
  );
}
