import type { SessionUser } from "@tbite/web-auth";

declare global {
  namespace App {
    interface Locals {
      user: SessionUser | null;
      apiToken: string | undefined;
    }
  }
}
export {};
