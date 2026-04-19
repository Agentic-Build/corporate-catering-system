<script lang="ts">
  /**
   * Generic multi-step wizard. Parent provides step labels and a step-index
   * binding. This component renders the stepper UI (labels + progress bar)
   * and exposes `next` / `prev` controls. The actual step content is passed
   * as children + per-step `{#if currentStep === N}` blocks, or via slots
   * per step.
   */
  import type { Snippet } from "svelte";

  interface Step {
    key: string;
    label: string;
    description?: string;
  }

  interface Props {
    steps: Step[];
    currentStep: number;
    children: Snippet<[{ step: Step; index: number }]>;
  }

  let { steps, currentStep = $bindable(0), children }: Props = $props();

  const active = $derived(steps[currentStep] ?? steps[0]);
</script>

<div class="grid gap-5">
  <ol class="grid gap-2 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-{steps.length}">
    {#each steps as step, index}
      {@const state =
        index < currentStep
          ? "done"
          : index === currentStep
            ? "active"
            : "upcoming"}
      <li class="flex items-start gap-2 md:flex-col md:items-center md:text-center">
        <span
          class={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${
            state === "done"
              ? "bg-emerald-500 text-white"
              : state === "active"
                ? "bg-cyan-600 text-white"
                : "bg-slate-200 text-slate-500"
          }`}
        >
          {#if state === "done"}
            ✓
          {:else}
            {index + 1}
          {/if}
        </span>
        <div class="grid gap-0.5 text-left md:text-center">
          <span
            class={`text-xs font-semibold ${state === "upcoming" ? "text-slate-500" : "text-slate-900"}`}
          >
            {step.label}
          </span>
          {#if step.description}
            <span class="hidden text-[11px] text-slate-500 md:block">{step.description}</span>
          {/if}
        </div>
      </li>
    {/each}
  </ol>

  <div>
    {@render children({ step: active, index: currentStep })}
  </div>
</div>
