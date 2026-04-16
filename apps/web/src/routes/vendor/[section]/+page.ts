import { error } from "@sveltejs/kit";

import { isValidPortalSection } from "$lib/platform/navigation";

import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => {
  if (!isValidPortalSection("vendor", params.section) || params.section === "overview") {
    throw error(404, "vendor section not found");
  }

  return {
    sectionId: params.section
  };
};
