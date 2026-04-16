import type { AuthRole } from "./contracts";

export interface RoleGuard {
  prefix: string;
  allowedRoles: readonly AuthRole[];
}

const ROLE_GUARDS: readonly RoleGuard[] = [
  { prefix: "/employee", allowedRoles: ["employee"] },
  { prefix: "/vendor", allowedRoles: ["vendor"] },
  { prefix: "/admin", allowedRoles: ["admin"] },
  { prefix: "/portal/employee", allowedRoles: ["employee"] },
  { prefix: "/portal/vendor", allowedRoles: ["vendor"] },
  { prefix: "/portal/admin", allowedRoles: ["admin"] },
  { prefix: "/console/employee", allowedRoles: ["employee"] },
  { prefix: "/console/vendor", allowedRoles: ["vendor"] },
  { prefix: "/console/admin", allowedRoles: ["admin"] }
];

export function resolveRoleGuard(pathname: string): RoleGuard | null {
  for (const guard of ROLE_GUARDS) {
    if (pathname === guard.prefix || pathname.startsWith(`${guard.prefix}/`)) {
      return guard;
    }
  }

  return null;
}
