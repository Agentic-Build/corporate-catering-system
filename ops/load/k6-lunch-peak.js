import http from "k6/http";
import { check, sleep, group } from "k6";

// Hard-SLO thresholds (design doc §9.2)
export const options = {
  scenarios: {
    browse_menu: {
      executor: "ramping-vus",
      exec: "browseMenu",
      startVUs: 0,
      stages: [
        { duration: "30s", target: 100 },
        { duration: "60s", target: 100 },
        { duration: "10s", target: 0 },
      ],
      tags: { operation: "browse_menu" },
    },
    place_orders: {
      executor: "constant-arrival-rate",
      exec: "placeOrder",
      rate: 5,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 20,
      maxVUs: 50,
      tags: { operation: "place_order" },
      startTime: "30s",
    },
    pickup_codes: {
      executor: "constant-vus",
      exec: "getPickupCode",
      vus: 50,
      duration: "60s",
      tags: { operation: "pickup_code" },
      startTime: "30s",
    },
  },
  thresholds: {
    "http_req_duration{operation:browse_menu}": ["p(95)<300"],
    "http_req_duration{operation:place_order}": ["p(95)<500"],
    "http_req_duration{operation:pickup_code}": ["p(95)<100"],
    "http_req_failed": ["rate<0.01"],
  },
};

const API = __ENV.API_BASE_URL || "http://localhost:8080";
const TOKEN_EMPLOYEE = __ENV.K6_TOKEN_EMPLOYEE || "";
const PLANT = __ENV.K6_PLANT || "F12B-3F";
const DAY = __ENV.K6_DAY || new Date().toISOString().slice(0, 10);
// Pre-seeded ready order IDs (comma-separated) for pickup_codes scenario:
const READY_ORDER_IDS = (__ENV.K6_READY_ORDER_IDS || "").split(",").filter(Boolean);

const headers = TOKEN_EMPLOYEE ? { Authorization: `Bearer ${TOKEN_EMPLOYEE}` } : {};

export function browseMenu() {
  group("browse_menu", () => {
    const r = http.get(`${API}/api/employee/menu?plant=${PLANT}&day=${DAY}`, { headers });
    check(r, {
      "browse 200": (res) => res.status === 200,
      "has items": (res) => {
        try { return Array.isArray(res.json("items")); } catch { return false; }
      },
    });
  });
  sleep(0.5);
}

export function placeOrder() {
  group("place_order", () => {
    // Body uses one of the pre-seeded menu items provided via env
    const MENU_ITEM_ID = __ENV.K6_MENU_ITEM_ID;
    if (!MENU_ITEM_ID) return;
    const payload = JSON.stringify({
      plant: PLANT,
      supply_date: DAY,
      items: [{ menu_item_id: MENU_ITEM_ID, qty: 1 }],
    });
    const r = http.post(`${API}/api/employee/orders`, payload, {
      headers: { ...headers, "content-type": "application/json" },
    });
    check(r, {
      "place 201 or 409 oos": (res) => res.status === 201 || res.status === 409,
    });
  });
}

export function getPickupCode() {
  group("pickup_code", () => {
    if (READY_ORDER_IDS.length === 0) return;
    const orderID = READY_ORDER_IDS[Math.floor(Math.random() * READY_ORDER_IDS.length)];
    const r = http.get(`${API}/api/employee/orders/${orderID}/pickup-code`, { headers });
    check(r, {
      "pickup 200 or 409": (res) => res.status === 200 || res.status === 409,
    });
  });
}
