import type { AuthRole } from "$lib/server/auth/contracts";

export const PORTAL_ROLES = ["employee", "vendor", "admin"] as const;

export type PortalRole = (typeof PORTAL_ROLES)[number];

interface PortalSectionDefinition {
  id: string;
  segment: string | null;
}

const PORTAL_SECTIONS: Record<PortalRole, readonly PortalSectionDefinition[]> = {
  employee: [
    { id: "overview", segment: null },
    { id: "orders", segment: "orders" },
    { id: "payroll", segment: "payroll" }
  ],
  vendor: [
    { id: "overview", segment: null },
    { id: "fulfillment", segment: "fulfillment" },
    { id: "menu", segment: "menu" }
  ],
  admin: [
    { id: "overview", segment: null },
    { id: "vendors", segment: "vendors" },
    { id: "anomalies", segment: "anomalies" }
  ]
};

export interface PortalLink {
  role: PortalRole;
  href: string;
  active: boolean;
  locked: boolean;
}

export interface SectionLink {
  id: string;
  href: string;
  active: boolean;
}

export interface RoleAwareNavigation {
  activePortal: PortalRole | null;
  rolePortal: PortalRole | null;
  sectionPortal: PortalRole | null;
  portalLinks: PortalLink[];
  sectionLinks: SectionLink[];
  activeSectionId: string | null;
}

export function getPortalBasePath(role: PortalRole): string {
  return `/${role}`;
}

export function getPortalSections(role: PortalRole): readonly PortalSectionDefinition[] {
  return PORTAL_SECTIONS[role];
}

export function getPortalSectionHref(role: PortalRole, sectionId: string): string {
  const section = PORTAL_SECTIONS[role].find((entry) => entry.id === sectionId);
  if (!section) {
    throw new Error(`unsupported section ${sectionId} for portal ${role}`);
  }

  if (!section.segment) {
    return getPortalBasePath(role);
  }

  return `${getPortalBasePath(role)}/${section.segment}`;
}

export function resolvePortalFromPath(pathname: string): PortalRole | null {
  for (const role of PORTAL_ROLES) {
    const basePath = getPortalBasePath(role);
    if (pathname === basePath || pathname.startsWith(`${basePath}/`)) {
      return role;
    }
  }

  return null;
}

export function resolveSectionIdFromPath(role: PortalRole, pathname: string): string | null {
  const basePath = getPortalBasePath(role);
  if (!pathname.startsWith(basePath)) {
    return null;
  }

  const remaining = pathname.slice(basePath.length);
  const segment = remaining.startsWith("/") ? remaining.slice(1).split("/")[0] : "";

  if (!segment) {
    return "overview";
  }

  const section = PORTAL_SECTIONS[role].find((entry) => entry.segment === segment);
  return section?.id ?? null;
}

export function isValidPortalSection(role: PortalRole, sectionId: string): boolean {
  return PORTAL_SECTIONS[role].some((entry) => entry.id === sectionId);
}

export function buildRoleAwareNavigation(
  actorRole: AuthRole | null,
  pathname: string
): RoleAwareNavigation {
  const activePortal = resolvePortalFromPath(pathname);
  const rolePortal = isPortalRole(actorRole) ? actorRole : null;
  const sectionPortal = rolePortal ?? activePortal;

  const portalLinks = PORTAL_ROLES.map((role) => {
    const href = getPortalBasePath(role);

    return {
      role,
      href,
      active: activePortal === role,
      locked: rolePortal !== null && rolePortal !== role
    } satisfies PortalLink;
  });

  const sectionLinks = sectionPortal
    ? PORTAL_SECTIONS[sectionPortal].map((section) => ({
        id: section.id,
        href: section.segment
          ? `${getPortalBasePath(sectionPortal)}/${section.segment}`
          : getPortalBasePath(sectionPortal),
        active: resolveSectionIdFromPath(sectionPortal, pathname) === section.id
      }))
    : [];

  return {
    activePortal,
    rolePortal,
    sectionPortal,
    portalLinks,
    sectionLinks,
    activeSectionId: sectionPortal ? resolveSectionIdFromPath(sectionPortal, pathname) : null
  };
}

function isPortalRole(role: AuthRole | null): role is PortalRole {
  return role === "employee" || role === "vendor" || role === "admin";
}
