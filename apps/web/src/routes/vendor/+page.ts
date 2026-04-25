import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "today" as const,
  breadcrumbs: [{ label: "商家入口", href: "/vendor" }, { label: "今日儀表板", href: null }]
});
