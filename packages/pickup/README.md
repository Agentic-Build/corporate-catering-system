# @tbite/pickup

Shared helpers for the order-pickup QR flow, used by the employee app (renders
the code) and the merchant app (scans it).

- `buildPickupQR(orderId)` → `tbite://pickup?order=<id>` — the QR payload.
- `parsePickupQR(text)` → `{ orderId }` or `null` — parses a scanned payload, rejecting anything that isn't a T-Bite pickup URL.
