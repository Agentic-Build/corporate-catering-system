// RFC 9457 Problem Details → user-facing string. Prefer `detail` > `title`;
// fall back to a plain string or `String(err)`. Never returns raw JSON.
export function problemMessage(err: unknown): string {
  if (err == null) return "未知錯誤";
  if (typeof err === "string") return err;
  if (typeof err === "object") {
    const o = err as Record<string, unknown>;
    if (typeof o.detail === "string" && o.detail) return o.detail;
    if (typeof o.title === "string" && o.title) return o.title;
    if (typeof o.message === "string" && o.message) return o.message;
  }
  try {
    return String(err);
  } catch {
    return "未知錯誤";
  }
}
