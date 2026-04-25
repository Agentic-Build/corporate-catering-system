import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "schedule" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "訂購政策", href: null }
  ]
});
