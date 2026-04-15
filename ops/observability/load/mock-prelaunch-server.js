const http = require("node:http");

const PORT = Number(process.env.PORT || "18080");

function jsonResponse(res, statusCode, body) {
  const payload = JSON.stringify(body);
  res.writeHead(statusCode, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(payload)
  });
  res.end(payload);
}

const server = http.createServer((req, res) => {
  const { method, url } = req;

  if (method === "GET" && url === "/health/ready") {
    return jsonResponse(res, 200, { status: "ready" });
  }
  if (method === "GET" && url === "/health/live") {
    return jsonResponse(res, 200, { status: "live" });
  }
  if (method === "GET" && url === "/health/startup") {
    return jsonResponse(res, 200, { status: "startup" });
  }
  if (method === "GET" && url === "/api/v1/employee/menus") {
    return jsonResponse(res, 200, {
      items: [
        {
          menuItemId: "menu-1",
          vendorId: "vendor-1",
          availableQuantity: 60
        }
      ]
    });
  }
  if (method === "POST" && url === "/api/v1/employee/orders") {
    return jsonResponse(res, 201, {
      orderId: `order-${Date.now()}`,
      status: "ACCEPTED"
    });
  }

  return jsonResponse(res, 404, { error: "not_found" });
});

server.listen(PORT, "127.0.0.1", () => {
  process.stdout.write(`mock prelaunch server listening on ${PORT}\n`);
});

const shutdown = () => {
  server.close(() => process.exit(0));
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
