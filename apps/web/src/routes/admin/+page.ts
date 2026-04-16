import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "overview" as const
});
