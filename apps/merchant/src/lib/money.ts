/**
 * Format a money amount stored in minor units (cents) for display.
 * e.g. 12000 → "NT$120". Used by the reconciliation / settlement pages,
 * whose API amounts (`gross_minor`) are minor units per the design spec.
 */
export function formatMinor(minor: number | null | undefined): string {
  const cents = minor ?? 0;
  return "NT$" + Math.round(cents / 100).toLocaleString();
}
