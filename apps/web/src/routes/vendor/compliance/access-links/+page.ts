import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "compliance" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "合規狀態", href: "/vendor/compliance" },
    { label: "建立下載連結", href: null }
  ]
});
