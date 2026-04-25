import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "orders" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "營運訂單查詢", href: null }
  ]
});
