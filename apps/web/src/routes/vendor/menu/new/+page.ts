import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "menu" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "菜單總覽", href: "/vendor/menu" },
    { label: "新增菜單", href: null }
  ]
});
