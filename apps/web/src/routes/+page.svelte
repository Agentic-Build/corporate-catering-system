<script lang="ts">
  import { enhance } from "$app/forms";
  import type { SubmitFunction } from "@sveltejs/kit";
  import { errorState, idleState, loadingState, successState, type AsyncState } from "$lib/platform/async-state";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { Card, Button } from "$lib/components/ui";
  import type { ActionData, PageData } from "./$types";

  let { data, form }: { data: PageData; form: ActionData | null } = $props();

  let pendingActionKey = $state<string | null>(null);
  let actionState = $state<AsyncState<{ message: string }, string>>(idleState());

  $effect(() => {
    if (!form) return;
    if (form.errorMessage) {
      actionState = errorState(form.errorMessage);
      return;
    }
    if (form.successMessage) {
      actionState = successState({ message: form.successMessage });
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
          actionState = successState({ message: readSuccessMessage(result.data) });
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
    if (!data || typeof data !== "object") return zhTW.home.actions.errorFallback;
    const message = (data as { errorMessage?: unknown }).errorMessage;
    return typeof message === "string" && message.length > 0
      ? message
      : zhTW.home.actions.errorFallback;
  }

  function readSuccessMessage(data: unknown): string {
    if (!data || typeof data !== "object") return zhTW.home.actions.successFallback;
    const message = (data as { successMessage?: unknown }).successMessage;
    return typeof message === "string" && message.length > 0
      ? message
      : zhTW.home.actions.successFallback;
  }

  const roleCards = [
    {
      role: "employee" as const,
      title: zhTW.nav.portals.employee,
      description: "手機優先。瀏覽菜單、多日預購、領餐 QR、查看扣款與申訴。",
      emphasis: "適合：廠內員工",
      accentClass: "border-emerald-200 bg-emerald-50/70",
      next: "/employee"
    },
    {
      role: "vendor" as const,
      title: zhTW.nav.portals.vendor,
      description: "桌機優先。維護菜單、推進備餐 / 配送、列印批次、管理合規文件。",
      emphasis: "適合：合作商家作業人員",
      accentClass: "border-cyan-200 bg-cyan-50/70",
      next: "/vendor"
    },
    {
      role: "admin" as const,
      title: zhTW.nav.portals.admin,
      description: "桌機優先。商家審核、合規生命週期、月結關帳、異常治理、稽核查詢。",
      emphasis: "適合：福委會管理員",
      accentClass: "border-violet-200 bg-violet-50/70",
      next: "/admin"
    }
  ];
</script>

<section class="grid gap-6">
  {#if data.actor}
    <!-- Logged-in landing: emphasize "continue to your portal"; hide other-role login cards -->
    <header class="rounded-2xl border border-emerald-200 bg-emerald-50/60 p-6 shadow-sm">
      <p class="text-xs font-semibold tracking-[0.14em] text-emerald-700">
        {zhTW.nav.portals[data.actor.role]}
      </p>
      <h2 class="mt-1 text-2xl font-bold text-slate-950">
        歡迎回來，{data.actor.displayName}
      </h2>
      <p class="mt-2 text-sm text-slate-700">
        直接前往你的入口，開始今天的工作。
      </p>
      <div class="mt-4 flex flex-wrap gap-2">
        <Button href={`/${data.actor.role}`} variant="primary">
          {zhTW.home.continueToPortal}
        </Button>
        <form method="POST" action="?/session" use:enhance={createSubmitHandler("session-logout")}>
          <input type="hidden" name="intent" value="logout" />
          <input type="hidden" name="next" value="/" />
          <Button type="submit" variant="ghost" loading={pendingActionKey === "session-logout"}>
            {zhTW.home.clearSession}
          </Button>
        </form>
      </div>
    </header>
  {:else}
    <header class="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
      <p class="text-xs font-semibold tracking-[0.14em] text-cyan-700">歡迎</p>
      <h2 class="mt-1 text-2xl font-bold text-slate-950">{zhTW.home.title}</h2>
      <p class="mt-2 text-sm text-slate-600">{zhTW.home.description}</p>
    </header>
  {/if}

  {#if !data.actor}
  <section class="grid gap-3 md:grid-cols-3">
    {#each roleCards as card}
      <article class={`grid gap-3 rounded-2xl border p-5 shadow-sm ${card.accentClass}`}>
        <div class="grid gap-1">
          <p class="text-[11px] font-semibold uppercase tracking-[0.14em] text-slate-500">{card.emphasis}</p>
          <h3 class="text-lg font-bold text-slate-900">{card.title}</h3>
          <p class="text-sm text-slate-700">{card.description}</p>
        </div>
        <form method="POST" action="?/session" use:enhance={createSubmitHandler(`session-${card.role}`)}>
          <input type="hidden" name="intent" value="login" />
          <input type="hidden" name="role" value={card.role} />
          <input type="hidden" name="next" value={card.next} />
          <Button
            type="submit"
            variant="primary"
            fullWidth
            disabled={pendingActionKey !== null}
            loading={pendingActionKey === `session-${card.role}`}
          >
            {zhTW.home.signIn[card.role]}
          </Button>
        </form>
      </article>
    {/each}
  </section>
  {/if}

  {#if data.actor}
    <Card title={zhTW.home.activeSessionTitle}>
      <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
        <div>
          <dt class="text-xs text-slate-500">{zhTW.shell.actorLabel}</dt>
          <dd class="font-medium">{data.actor.displayName} ({data.actor.id})</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">{zhTW.shell.providerLabel}</dt>
          <dd class="font-medium">{data.auth.provider}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">{zhTW.shell.refreshAfterLabel}</dt>
          <dd class="font-medium">
            {data.auth.refreshAfterEpochMs
              ? new Date(data.auth.refreshAfterEpochMs).toLocaleString("zh-TW", { hour12: false, timeZone: "Asia/Taipei" })
              : "-"}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">{zhTW.shell.expiresAtLabel}</dt>
          <dd class="font-medium">
            {data.auth.expiresAtEpochMs
              ? new Date(data.auth.expiresAtEpochMs).toLocaleString("zh-TW", { hour12: false, timeZone: "Asia/Taipei" })
              : "-"}
          </dd>
        </div>
      </dl>

      <div>
        <form method="POST" action="?/probeApi" use:enhance={createSubmitHandler("probe-api")}>
          <Button type="submit" variant="secondary" loading={pendingActionKey === "probe-api"}>
            {zhTW.home.probeApi}
          </Button>
        </form>
      </div>
    </Card>
  {/if}

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
