import { API_BASE_URL } from "$lib/server/env";
import type { PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ fetch, locals }) => {
  let apiHealth: "ok" | "down" = "down";
  try {
    const res = await fetch(`${API_BASE_URL}/healthz`);
    if (res.ok) apiHealth = "ok";
  } catch {}
  return { apiHealth, user: locals.user };
};
