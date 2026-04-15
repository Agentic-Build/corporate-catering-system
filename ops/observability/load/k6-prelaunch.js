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

function buildOrderPayload() {
  return JSON.stringify({
    vendorId: `vendor-${(__VU % 4) + 1}`,
    deliveryEpochDay: 21100 + (__ITER % 5),
    lineItems: [
      { menuItemId: `menu-${(__ITER % 12) + 1}`, quantity: 1, specialRequestOption: "NO_ICE" }
    ]
  });
}

export function peakOrderPlacement() {
  const orderResponse = http.post(
    `${BASE_URL}/api/v1/employee/orders`,
    buildOrderPayload(),
    {
      headers: { "Content-Type": "application/json" },
      tags: { operation: "createEmployeeOrder" }
    }
  );
  check(orderResponse, {
    "create order endpoint accepted request": (res) =>
      res.status === 200 || res.status === 201 || res.status === 202
  });

  checkReadiness();
  sleep(0.05);
}

function parseOrderIdOrFallback(response, fallbackOrderId) {
  if (!response || (response.status !== 200 && response.status !== 201 && response.status !== 202)) {
    return fallbackOrderId;
  }
  try {
    const payload = response.json();
    if (payload && typeof payload === "object" && payload.orderId) {
      return payload.orderId;
    }
    return fallbackOrderId;
  } catch (_error) {
    return fallbackOrderId;
  }
}

export function mixedOrderAndMenuReads() {
  const menuResponse = http.get(`${BASE_URL}/api/v1/employee/menus`, {
    tags: { operation: "listEmployeeMenus" }
  });
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
        { menuItemId: `menu-${(iteration % 12) + 1}`, quantity: 2 }
      ]
    });
  }
  return JSON.stringify({
    operation: "CANCEL",
    cancelReason: "load-test lifecycle mutation"
  });
}

export function peakOrderLifecycleMutations() {
  const syntheticOrderId = `order-lifecycle-${__VU}-${__ITER}`;
  const patchResponse = http.patch(
    `${BASE_URL}/api/v1/employee/orders/${syntheticOrderId}`,
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
    buildOrderPayload(),
    {
      headers: { "Content-Type": "application/json" },
      tags: { operation: "createEmployeeOrder" }
    }
  );
  check(createResponse, {
    "create order endpoint accepted request for pickup flow": (res) =>
      res.status === 200 || res.status === 201 || res.status === 202
  });

  const fallbackOrderId = `order-pickup-${__VU}-${__ITER}`;
  const orderId = parseOrderIdOrFallback(createResponse, fallbackOrderId);
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
