import { describe, expect, it } from "vitest";
import { formatDuration, statusLabel } from "./format";

describe("format helpers", () => {
  it("formats a completed duration", () => {
    expect(
      formatDuration("2026-11-20T09:00:00Z", "2026-11-20T09:02:05Z")
    ).toBe("2m 5s");
  });

  it("maps all workflow statuses to Chinese labels", () => {
    expect(statusLabel.running).toBe("运行中");
    expect(statusLabel.timed_out).toBe("超时");
  });
});
