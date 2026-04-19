import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ params }) => ({
  sectionId: "batches" as const,
  batchId: params.batchId,
  breadcrumbs: [
    { label: "商家入口", href: "/vendor" },
    { label: "備餐批次", href: "/vendor/batches" },
    { label: params.batchId, href: null }
  ]
});
