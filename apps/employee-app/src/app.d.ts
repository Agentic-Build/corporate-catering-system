// SvelteKit ambient types. This is a static SPA — there is no server-side
// `Locals`; auth state lives in the client-side session store.
declare global {
  namespace App {
    // eslint-disable-next-line @typescript-eslint/no-empty-interface
    interface Error {}
    // eslint-disable-next-line @typescript-eslint/no-empty-interface
    interface Locals {}
    // eslint-disable-next-line @typescript-eslint/no-empty-interface
    interface PageData {}
    // eslint-disable-next-line @typescript-eslint/no-empty-interface
    interface Platform {}
  }
}

export {};
