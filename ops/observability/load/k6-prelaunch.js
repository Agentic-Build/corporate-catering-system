import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: {
    peak_order_placement: {
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
    mixed_order_and_menu_reads: {
      executor: "constant-arrival-rate",
      exec: "mixedOrderAndMenuReads",
      rate: 180,
      timeUnit: "1s",
      duration: "60s",
      preAllocatedVUs: 140,
      maxVUs: 500,
      startTime: "10s"
    }
  },
  thresholds: {
    "http_reqs{scenario:peak_order_placement}": ["rate>120"],
    "http_reqs{scenario:mixed_order_and_menu_reads}": ["rate>180"],
    "http_req_duration{scenario:peak_order_placement}": ["p(95)<350"],
    "http_req_failed{scenario:peak_order_placement}": ["rate<0.002"],
    "http_req_duration{scenario:mixed_order_and_menu_reads}": ["p(95)<250"],
    "http_req_failed{scenario:mixed_order_and_menu_reads}": ["rate<0.001"],
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
