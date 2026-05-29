// FormData text field → string. FormDataEntryValue may be a File; non-strings fall back.
export function formStr(fd: FormData, key: string, fallback = ""): string {
  const v = fd.get(key);
  return typeof v === "string" ? v : fallback;
}
