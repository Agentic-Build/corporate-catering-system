import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";

// /supply was folded into the home dashboard's 7-day schedule planner.
export const load: PageServerLoad = () => {
  throw redirect(301, "/");
};
