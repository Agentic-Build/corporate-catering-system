import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "商家清單", href: "/admin/vendors" },
    { label: params.vendorId, href: `/admin/vendors/${params.vendorId}` },
    { label: "審核決策", href: null }
  ];
  return { breadcrumbs, vendorId: params.vendorId };
};
