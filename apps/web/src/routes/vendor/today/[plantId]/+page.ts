import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => ({
  sectionId: "today" as const,
  plantId: params.plantId,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "今日作業看板", href: "/vendor/today" },
    { label: `廠區 ${params.plantId}`, href: null }
  ]
});
