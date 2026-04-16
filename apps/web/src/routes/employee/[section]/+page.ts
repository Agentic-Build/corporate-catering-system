import { error } from "@sveltejs/kit";

import { isValidPortalSection } from "$lib/platform/navigation";

import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => {
  if (!isValidPortalSection("employee", params.section) || params.section === "overview") {
    throw error(404, "employee section not found");
  }

  return {
    sectionId: params.section
  };
};
