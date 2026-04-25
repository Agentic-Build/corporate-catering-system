import type { PageLoad } from "./$types";

export const load: PageLoad = async () => ({
  sectionId: "batches" as const,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "備餐批次", href: "/vendor/batches" },
    { label: "建立批次", href: null }
  ]
});
