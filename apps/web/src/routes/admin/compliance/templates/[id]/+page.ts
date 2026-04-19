import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "合規文件", href: "/admin/compliance/templates" },
    { label: params.id, href: null }
  ];
  return { breadcrumbs, compositeId: params.id };
};
