export function buildPickupQR(orderId: string): string {
  return `tbite://pickup?order=${orderId}`;
}

const PREFIX = "tbite://pickup?";

export function parsePickupQR(text: string): { orderId: string } | null {
  if (!text?.startsWith(PREFIX)) return null;
  const params = new URLSearchParams(text.slice(PREFIX.length));
  const orderId = params.get("order");
  return orderId ? { orderId } : null;
}
