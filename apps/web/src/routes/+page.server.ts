import type { PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals }) => ({
  actor: locals.actor,
  provider: locals.auth.provider,
  session: locals.auth.session
});
