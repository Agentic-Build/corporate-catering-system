import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "insights" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "營運分析", href: null }
  ]
});
