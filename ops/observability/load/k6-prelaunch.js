import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: {
    "peak-order-placement": {
      executor: "ramping-arrival-rate",
      exec: "peakOrderPlacement",
      startRate: 50,
      timeUnit: "1s",
      preAllocatedVUs: 80,
      maxVUs: 400,
      stages: [
        { target: 120, duration: "20s" },
        { target: 120, duration: "40s" },
        { target: 50, duration: "10s" }
      ]
    },
    "mixed-order-and-menu-reads": {
      executor: "constant-arrival-rate",
      exec: "mixedOrderAndMenuReads",
      rate: 180,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 140,
      maxVUs: 500,
      startTime: "10s"
    },
    "peak-order-lifecycle-mutations": {
      executor: "constant-arrival-rate",
      exec: "peakOrderLifecycleMutations",
      rate: 140,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 160,
      maxVUs: 520,
      startTime: "20s"
    },
    "peak-order-and-pickup-verification": {
      executor: "constant-arrival-rate",
      exec: "peakOrderAndPickupVerification",
      rate: 160,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 180,
      maxVUs: 540,
      startTime: "20s"
    }
  },
  thresholds: {
    "http_reqs{scenario:peak-order-placement}": ["rate>120"],
    "http_reqs{scenario:mixed-order-and-menu-reads}": ["rate>180"],
    "http_reqs{scenario:peak-order-lifecycle-mutations}": ["rate>140"],
    "http_reqs{scenario:peak-order-and-pickup-verification}": ["rate>160"],
    "http_req_duration{scenario:peak-order-placement}": ["p(95)<350", "p(99)<500"],
    "http_req_failed{scenario:peak-order-placement}": ["rate<0.002"],
    "http_req_duration{scenario:mixed-order-and-menu-reads}": ["p(95)<250", "p(99)<400"],
    "http_req_failed{scenario:mixed-order-and-menu-reads}": ["rate<0.001"],
    "http_req_duration{scenario:peak-order-lifecycle-mutations}": ["p(95)<320", "p(99)<480"],
    "http_req_failed{scenario:peak-order-lifecycle-mutations}": ["rate<0.002"],
    "http_req_duration{scenario:peak-order-and-pickup-verification}": ["p(95)<300", "p(99)<450"],
    "http_req_failed{scenario:peak-order-and-pickup-verification}": ["rate<0.002"],
    "checks{check_type:readiness}": ["rate>0.999"]
  }
};

const BASE_URL = __ENV.BASE_URL || "http://127.0.0.1:18080";
const PLANT_ID = (__ENV.PLANT_ID || "fab-a").trim();
const DELIVERY_EPOCH_DAY = Number(__ENV.DELIVERY_EPOCH_DAY || "");
const MENU_VARIANT_COUNT = Number(__ENV.MENU_VARIANT_COUNT || "64");

if (!PLANT_ID) {
  throw new Error("PLANT_ID must be set for prelaunch load verification");
}
if (!Number.isInteger(DELIVERY_EPOCH_DAY) || DELIVERY_EPOCH_DAY <= 0) {
  throw new Error("DELIVERY_EPOCH_DAY must be a positive integer");
}
if (!Number.isInteger(MENU_VARIANT_COUNT) || MENU_VARIANT_COUNT < 1) {
  throw new Error("MENU_VARIANT_COUNT must be a positive integer");
}

function menuItemIdForIteration(iteration) {
  return `menu-${(iteration % MENU_VARIANT_COUNT) + 1}`;
}

function civilFromDays(daysSinceEpoch) {
  const shiftedDays = daysSinceEpoch + 719468;
  const era = Math.floor((shiftedDays >= 0 ? shiftedDays : shiftedDays - 146096) / 146097);
  const dayOfEra = shiftedDays - era * 146097;
  const yearOfEra =
    Math.floor(
      (dayOfEra - Math.floor(dayOfEra / 1460) + Math.floor(dayOfEra / 36524) - Math.floor(dayOfEra / 146096)) /
        365
    );
  const year = yearOfEra + era * 400;
  const dayOfYear = dayOfEra - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const monthPiece = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * monthPiece + 2) / 5) + 1;
  const month = monthPiece + (monthPiece < 10 ? 3 : -9);
  const adjustedYear = year + (month <= 2 ? 1 : 0);
  return { year: adjustedYear, month, day };
}

function epochDayToIsoDate(epochDay) {
  const { year, month, day } = civilFromDays(epochDay);
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

const DELIVERY_DATE = epochDayToIsoDate(DELIVERY_EPOCH_DAY);

function checkReadiness() {
  const readiness = http.get(`${BASE_URL}/health/ready`, {
    tags: { endpoint: "health_ready" }
  });
  check(
    readiness,
    {
      "readiness endpoint healthy": (res) => res.status === 200
    },
    { check_type: "readiness" }
  );
}

function buildOrderPayload(iteration) {
  return JSON.stringify({
    plantId: PLANT_ID,
    deliveryDate: DELIVERY_DATE,
    lineItems: [
      {
        menuItemId: menuItemIdForIteration(iteration),
        quantity: 1,
        specialRequests: ["NO_UTENSILS"]
      }
    ]
  });
}

export function peakOrderPlacement() {
  const orderResponse = http.post(
    `${BASE_URL}/api/v1/employee/orders`,
    buildOrderPayload(__ITER),
    {
      headers: { "Content-Type": "application/json" },
      tags: { operation: "createEmployeeOrder" }
    }
  );
  check(orderResponse, {
    "create order endpoint accepted request": (res) => res.status === 201
  });

  checkReadiness();
  sleep(0.05);
}

function parseOrderIdStrict(response) {
  if (!response || response.status !== 201) {
    throw new Error(`create order request failed with status ${response ? response.status : "unknown"}`);
  }
  let payload;
  try {
    payload = response.json();
  } catch (_error) {
    throw new Error("create order response is not valid JSON");
  }
  if (payload && typeof payload === "object" && typeof payload.orderId === "string" && payload.orderId.trim().length > 0) {
    return payload.orderId;
  }
  throw new Error("create order response missing required `orderId`");
}

export function mixedOrderAndMenuReads() {
  const menuResponse = http.get(
    `${BASE_URL}/api/v1/employee/menus?plantId=${encodeURIComponent(PLANT_ID)}&view=week&menuDate=${encodeURIComponent(
      DELIVERY_DATE
    )}`,
    {
      tags: { operation: "listEmployeeMenus" }
    }
  );
  check(menuResponse, {
    "menu endpoint healthy": (res) => res.status === 200
  });

  if (__ITER % 3 === 0) {
    peakOrderPlacement();
  } else {
    checkReadiness();
  }

  sleep(0.03);
}

function buildOrderPatchPayload(iteration) {
  if (iteration % 2 === 0) {
    return JSON.stringify({
      operation: "REPLACE_LINE_ITEMS",
      lineItems: [
        {
          menuItemId: menuItemIdForIteration(iteration + 1),
          quantity: 1,
          specialRequests: ["NO_UTENSILS"]
        }
      ]
    });
  }
  return JSON.stringify({
    operation: "CANCEL",
    cancelReason: "load-test lifecycle mutation"
  });
}

export function peakOrderLifecycleMutations() {
  const createResponse = http.post(
    `${BASE_URL}/api/v1/employee/orders`,
    buildOrderPayload(__ITER),
    {
      headers: { "Content-Type": "application/json" },
      tags: { operation: "createEmployeeOrder" }
    }
  );
  check(createResponse, {
    "create order endpoint accepted request for lifecycle flow": (res) => res.status === 201
  });

  const orderId = parseOrderIdStrict(createResponse);
  const patchResponse = http.patch(
    `${BASE_URL}/api/v1/employee/orders/${orderId}`,
    buildOrderPatchPayload(__ITER),
    {
      headers: { "Content-Type": "application/json" },
      tags: {
        operation: "updateEmployeeOrder",
        name: "PATCH /api/v1/employee/orders/{orderId}"
      }
    }
  );
  check(patchResponse, {
    "update order endpoint accepted request": (res) =>
      res.status === 200 || res.status === 202
  });

  checkReadiness();
  sleep(0.03);
}

function buildPickupVerificationPayload() {
  const numericCode = __ITER % 999999;
  const zeroPaddedCode = `000000${numericCode}`.slice(-6);
  return JSON.stringify({
    verificationCode: `TOTP-${zeroPaddedCode}`
  });
}

export function peakOrderAndPickupVerification() {
  const createResponse = http.post(
    `${BASE_URL}/api/v1/employee/orders`,
    buildOrderPayload(__ITER),
    {
      headers: { "Content-Type": "application/json" },
      tags: { operation: "createEmployeeOrder" }
    }
  );
  check(createResponse, {
    "create order endpoint accepted request for pickup flow": (res) => res.status === 201
  });

  const orderId = parseOrderIdStrict(createResponse);
  const pickupResponse = http.post(
    `${BASE_URL}/api/v1/employee/orders/${orderId}/pickup-verifications`,
    buildPickupVerificationPayload(),
    {
      headers: { "Content-Type": "application/json" },
      tags: {
        operation: "verifyPickupOrder",
        name: "POST /api/v1/employee/orders/{orderId}/pickup-verifications"
      }
    }
  );
  check(pickupResponse, {
    "pickup verification endpoint accepted request": (res) =>
      res.status === 200 || res.status === 202
  });

  checkReadiness();
  sleep(0.03);
}
