import { error } from "@sveltejs/kit";

import { isValidPortalSection } from "$lib/platform/navigation";

import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => {
  if (!isValidPortalSection("admin", params.section) || params.section === "overview") {
    throw error(404, "admin section not found");
  }

  return {
    sectionId: params.section
  };
};
