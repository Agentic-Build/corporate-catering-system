<script lang="ts">
  import { StateTag } from "@tbite/ui";
  import { statusEntry, type StatusKind } from "./status";

  interface Props {
    kind: StatusKind;
    value: string;
    pulse?: boolean;
  }
  let { kind, value, pulse = false }: Props = $props();
  const entry = $derived(statusEntry(kind, value));
</script>

<StateTag tone={entry.tone} {pulse}>{entry.label}</StateTag>
