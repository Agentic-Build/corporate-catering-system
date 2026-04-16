<script lang="ts">
  import { enhance } from "$app/forms";
  import type { SubmitFunction } from "@sveltejs/kit";
  import { errorState, idleState, loadingState, successState, type AsyncState } from "$lib/platform/async-state";
  import { zhTW } from "$lib/i18n/zh-tw";
  import type { ActionData, PageData } from "./$types";

  let { data, form }: { data: PageData; form: ActionData | null } = $props();

  let pendingActionKey = $state<string | null>(null);
  let actionState = $state<AsyncState<{ message: string }, string>>(idleState());

  $effect(() => {
    if (!form) {
      return;
    }

    if (form.errorMessage) {
      actionState = errorState(form.errorMessage);
      return;
    }

    if (form.successMessage) {
      actionState = successState({
        message: form.successMessage
      });
    }
  });

  function createSubmitHandler(actionKey: string): SubmitFunction {
    return ({ cancel }) => {
      if (pendingActionKey !== null) {
        cancel();
        return;
      }

      pendingActionKey = actionKey;
      actionState = loadingState();

      return async ({ result, update }) => {
        pendingActionKey = null;

        if (result.type === "failure") {
          actionState = errorState(readErrorMessage(result.data));
          await update({ reset: false });
          return;
        }

        if (result.type === "success") {
          actionState = successState({
            message: readSuccessMessage(result.data)
          });
          await update({ reset: true });
          return;
        }

        if (result.type === "error") {
          actionState = errorState(zhTW.home.actions.errorFallback);
          await update();
          return;
        }

        await update();
      };
    };
  }

  function readErrorMessage(data: unknown): string {
    if (!data || typeof data !== "object") {
      return zhTW.home.actions.errorFallback;
    }

    const message = (data as { errorMessage?: unknown }).errorMessage;
    return typeof message === "string" && message.length > 0
      ? message
      : zhTW.home.actions.errorFallback;
  }

  function readSuccessMessage(data: unknown): string {
    if (!data || typeof data !== "object") {
      return zhTW.home.actions.successFallback;
    }

    const message = (data as { successMessage?: unknown }).successMessage;
    return typeof message === "string" && message.length > 0
      ? message
      : zhTW.home.actions.successFallback;
  }

  function resolveButtonLabel(actionKey: string, defaultLabel: string): string {
    return pendingActionKey === actionKey ? zhTW.home.actions.loading : defaultLabel;
  }
</script>

<section class="grid gap-4">
  <header class="rounded-xl border border-emerald-100 bg-emerald-50/70 p-4">
    <h2 class="text-xl font-bold text-slate-950">{zhTW.home.title}</h2>
    <p class="mt-2 text-sm text-slate-700">{zhTW.home.description}</p>
  </header>

  <section class="grid gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
    <h3 class="text-sm font-semibold text-slate-800">{zhTW.home.portalPreviewLabel}</h3>
    <div class="grid gap-2 md:grid-cols-3">
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.employee}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.employee.lead}</p>
      </article>
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.vendor}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.vendor.lead}</p>
      </article>
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.admin}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.admin.lead}</p>
      </article>
    </div>
  </section>

  <section class="grid gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
    <h3 class="text-sm font-semibold text-slate-800">{zhTW.home.signInActionsLabel}</h3>
    <div class="grid gap-2 md:grid-cols-2">
      <form method="POST" action="?/session" use:enhance={createSubmitHandler("session-employee")}>
        <input type="hidden" name="intent" value="login" />
        <input type="hidden" name="role" value="employee" />
        <input type="hidden" name="next" value="/employee" />
        <button
          class="w-full rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400 disabled:hover:bg-transparent"
          type="submit"
          disabled={pendingActionKey !== null}
        >
          {resolveButtonLabel("session-employee", zhTW.home.signIn.employee)}
        </button>
      </form>
      <form method="POST" action="?/session" use:enhance={createSubmitHandler("session-vendor")}>
        <input type="hidden" name="intent" value="login" />
        <input type="hidden" name="role" value="vendor" />
        <input type="hidden" name="next" value="/vendor" />
        <button
          class="w-full rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400 disabled:hover:bg-transparent"
          type="submit"
          disabled={pendingActionKey !== null}
        >
          {resolveButtonLabel("session-vendor", zhTW.home.signIn.vendor)}
        </button>
      </form>
      <form method="POST" action="?/session" use:enhance={createSubmitHandler("session-admin")}>
        <input type="hidden" name="intent" value="login" />
        <input type="hidden" name="role" value="admin" />
        <input type="hidden" name="next" value="/admin" />
        <button
          class="w-full rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400 disabled:hover:bg-transparent"
          type="submit"
          disabled={pendingActionKey !== null}
        >
          {resolveButtonLabel("session-admin", zhTW.home.signIn.admin)}
        </button>
      </form>
      <form method="POST" action="?/session" use:enhance={createSubmitHandler("session-logout")}>
        <input type="hidden" name="intent" value="logout" />
        <input type="hidden" name="next" value="/" />
        <button
          class="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-semibold text-slate-700 transition hover:bg-slate-100 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400 disabled:hover:bg-transparent"
          type="submit"
          disabled={pendingActionKey !== null}
        >
          {resolveButtonLabel("session-logout", zhTW.home.clearSession)}
        </button>
      </form>
    </div>
    {#if data.actor}
      <form method="POST" action="?/probeApi" use:enhance={createSubmitHandler("probe-api")}>
        <button
          class="rounded-lg border border-amber-500 px-3 py-2 text-sm font-semibold text-amber-700 transition hover:bg-amber-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400 disabled:hover:bg-transparent"
          type="submit"
          disabled={pendingActionKey !== null}
        >
          {resolveButtonLabel("probe-api", zhTW.home.probeApi)}
        </button>
      </form>
    {/if}
    {#if data.actor}
      <a
        class="inline-flex w-fit rounded-lg border border-emerald-600 px-3 py-2 text-sm font-semibold text-emerald-700 transition hover:bg-emerald-50"
        href={`/${data.actor.role}`}
      >
        {zhTW.home.continueToPortal}
      </a>
    {/if}
  </section>

  <section class="rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-700 shadow-sm">
    {#if actionState.status === "idle"}
      {zhTW.home.actions.idle}
    {:else if actionState.status === "loading"}
      {zhTW.home.actions.loading}
    {:else if actionState.status === "success"}
      {actionState.data.message}
    {:else}
      {actionState.error}
    {/if}
  </section>
</section>
