import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";

// The per-day supply editor has been folded into the home dashboard's
// 7-day schedule planner. Permanently redirect /supply → /.
export const load: PageServerLoad = () => {
  throw redirect(301, "/");
};
