// Bảng lỗi theo dòng của bản nhập (mockup docs/mockups/m1/nhap-excel.html, trạng thái C): Dòng, Cột, Lỗi. Câu lỗi
// do máy chủ viết sẵn tiếng Việt (CatalogImportError.message), hiện nguyên văn như chữ thường.
import type { CatalogImportError } from "../../api";
import { columnText, rowText } from "./importRules";

export function ImportErrors({ errors }: { errors: readonly CatalogImportError[] }) {
  return (
    <div className="tbl-frame">
      <table className="tbl etbl">
        <thead>
          <tr>
            <th className="c-row">Dòng</th>
            <th className="c-col">Cột</th>
            <th>Lỗi</th>
          </tr>
        </thead>
        <tbody>
          {errors.map((e, i) => (
            <tr key={`${String(e.row)}-${e.column}-${String(i)}`}>
              <td data-label="Dòng" className="num bold">
                {rowText(e.row)}
              </td>
              <td data-label="Cột">{columnText(e.column)}</td>
              <td data-label="Lỗi">{e.message}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
