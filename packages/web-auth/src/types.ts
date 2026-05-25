export type Role = "employee" | "vendor_operator" | "welfare_admin";

export interface SessionUser {
  user_id: string;
  email: string;
  display_name: string;
  role: Role;
  employee_id?: string;
  plant?: string;
  department?: string;
  vendor_id?: string;
}

export interface AuthOptions {
  apiBaseUrl: string;
  cookieDomain?: string;
  cookieSecure?: boolean;
  // Per-app cookie name so the 3 apps don't collide on localhost (cookies aren't port-scoped).
  // Defaults to COOKIE_NAME for backward compatibility.
  cookieName?: string;
}
