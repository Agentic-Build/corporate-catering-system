# @tbite/ui

Shared Svelte 5 component library for the three frontends (employee, merchant,
admin), built on the design tokens in [`@tbite/tokens`](../tokens).

Exports (see `src/index.ts`): layout/shell (`PageHeader`, `LocationBar`,
`Tabs`, `WeekCalendar`), inputs (`Button`, `SearchInput`, `Toggle`,
`ProviderButton`), surfaces (`Card`, `Modal`, `Drawer`, `EmptyState`),
domain pieces (`MealCard`, `StatCard`, `StateTag`), and brand (`TBiteLogo`,
`Icon` + the `IconName` type).

```svelte
<script>
  import { Button, MealCard } from "@tbite/ui";
</script>
```
