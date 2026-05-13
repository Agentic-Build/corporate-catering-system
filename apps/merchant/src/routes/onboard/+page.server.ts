import { error } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";

const INVITE_COOKIE = "tbite_invite";

export const load: PageServerLoad = async ({ cookies, url }) => {
  const invite = url.searchParams.get("invite");
  if (!invite) throw error(400, "missing invite code");
  // Store invite code in HttpOnly cookie for the duration of the OIDC flow.
  cookies.set(INVITE_COOKIE, invite, {
    path: "/",
    httpOnly: true,
    sameSite: "lax",
    secure: false, // dev
    maxAge: 60 * 10, // 10 minutes
  });
  return { invite };
};
