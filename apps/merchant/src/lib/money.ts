// Amounts are integer NTD (no cents) per the design spec — display as-is, no /100.
export function formatMinor(minor: number | null | undefined): string {
  return "NT$" + Math.round(minor ?? 0).toLocaleString();
}
