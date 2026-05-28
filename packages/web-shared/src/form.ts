// FormData text field → string. FormDataEntryValue may be a File; coerce non-strings to "".
export function formStr(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v : "";
}
