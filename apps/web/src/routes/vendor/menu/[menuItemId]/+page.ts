import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => ({
  sectionId: "menu" as const,
  menuItemId: params.menuItemId,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "菜單總覽", href: "/vendor/menu" },
    { label: `編輯 ${params.menuItemId}`, href: null }
  ]
});
