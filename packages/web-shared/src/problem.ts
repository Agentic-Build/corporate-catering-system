// RFC 9457 Problem Details → user-facing string. Prefer `detail` > `title`;
// fall back to a plain primitive. Never returns raw JSON or "[object Object]".
export function problemMessage(err: unknown): string {
  if (err == null) return "未知錯誤";
  if (typeof err === "string") return err;
  if (typeof err === "object") {
    const o = err as Record<string, unknown>;
    if (typeof o.detail === "string" && o.detail) return o.detail;
    if (typeof o.title === "string" && o.title) return o.title;
    if (typeof o.message === "string" && o.message) return o.message;
    return "未知錯誤";
  }
  if (typeof err === "number" || typeof err === "boolean" || typeof err === "bigint") {
    return String(err);
  }
  return "未知錯誤";
}
