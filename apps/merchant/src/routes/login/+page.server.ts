import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (locals.user) throw redirect(303, url.searchParams.get("return_to") ?? "/");
  return {
    returnTo: url.searchParams.get("return_to") ?? "/",
    providers: await authProviders(),
  };
};

async function authProviders() {
  const resp = await fetch(`${API_BASE_URL}/auth/providers`);
  if (!resp.ok) return [];
  const body = (await resp.json()) as { items?: Array<{ slug: string; display_name: string }> };
  return body.items ?? [];
}
