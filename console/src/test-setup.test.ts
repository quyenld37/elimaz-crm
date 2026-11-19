// Hạn chờ của findBy*/waitFor là chốt chống treo 30 s (src/test-setup.ts, Task 421), không phải 1 s mặc định.
import { getConfig } from "@testing-library/dom";
import { describe, expect, it } from "vitest";
import { HANG_GUARD_MS } from "./test-setup";

describe("test-setup", () => {
  it("findBy*/waitFor chờ tới chốt chống treo 30 s", () => {
    expect(HANG_GUARD_MS).toBe(30_000);
    expect(getConfig().asyncUtilTimeout).toBe(HANG_GUARD_MS);
  });
});
