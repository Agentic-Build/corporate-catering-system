import { redirect, error } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";

export async function GET({ url }) {
  const provider = url.searchParams.get("provider");
  const returnTo = url.searchParams.get("return_to") ?? "/";
  if (provider !== "google" && provider !== "github") throw error(400, "bad provider");

  const apiBaseUrl = env.API_BASE_URL ?? "http://localhost:8080";
  const resp = await fetch(`${apiBaseUrl}/auth/${provider}/start`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ app: "employee", return_to: returnTo }),
  });
  if (!resp.ok) throw error(502, "auth start failed");
  const data = (await resp.json()) as { auth_url: string };
  throw redirect(303, data.auth_url);
}
