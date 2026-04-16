import type { PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals }) => ({
  actor: locals.actor,
  provider: locals.auth.provider,
  expiresAtEpochMs: locals.auth.session?.expiresAtEpochMs ?? null,
  refreshAfterEpochMs: locals.auth.session?.refreshAfterEpochMs ?? null
});
