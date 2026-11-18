// Chú thích dòng không vào danh mục.
import { x } from "./x";

/** Hàm mẫu: cộng hai số. Câu thứ hai không vào danh mục. */
export { add } from "../lib/add";
export type { Options, State } from "../lib/types";
export {
  multi,
  line,
} from "../lib/multi";
export function inline(a: number): number {
  return a + x;
}
export const VALUE = 1;
const hidden = 2;
