import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: {
    peak_order_placement: {
      executor: "ramping-arrival-rate",
      startRate: 60,
      timeUnit: "1s",
      preAllocatedVUs: 60,
      maxVUs: 300,
      stages: [
        { target: 180, duration: "5m" },
        { target: 180, duration: "10m" },
        { target: 60, duration: "2m" }
      ]
    }
  },
  thresholds: {
    http_req_duration: ["p(95)<350"],
    http_req_failed: ["rate<0.002"],
    checks: ["rate>0.999"]
  }
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export default function () {
  const menuRes = http.get(`${BASE_URL}/api/v1/employee/menus`);
  check(menuRes, {
    "menu endpoint healthy": (res) => res.status === 200 || res.status === 401
  });

  const readiness = http.get(`${BASE_URL}/health/ready`);
  check(readiness, {
    "readiness endpoint healthy": (res) => res.status === 200
  });

  sleep(0.2);
}
