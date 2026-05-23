import type { PageServerLoad } from "./$types";
import { redirect } from "@sveltejs/kit";

// "我的" aggregation page — the mobile bottom-nav Profile tab. The shell's
// LayoutServerLoad already supplies `user`; this load only guards auth.
export const load: PageServerLoad = ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  return {};
};
