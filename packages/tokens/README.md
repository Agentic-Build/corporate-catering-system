# @tbite/tokens

Canonical design tokens for the T-Bite corporate catering system. Sourced from the T-Bite Design System (`colors_and_type.css`).

## Exports

- `@tbite/tokens/tokens.css` — CSS custom properties (colors, typography). Import once per app.
- `@tbite/tokens/fonts.css` — Google Fonts import for `Noto Sans TC` and `JetBrains Mono`.
- `@tbite/tokens/tailwind` — Tailwind preset mapping the key tokens onto Tailwind theme keys.

## Usage

In an app's `src/app.css`:

```css
@import "@tbite/tokens/fonts.css";
@import "@tbite/tokens/tokens.css";
@tailwind base;
@tailwind components;
@tailwind utilities;
```

In `tailwind.config.cjs`:

```js
const preset = require("@tbite/tokens/tailwind");
module.exports = {
  presets: [preset],
  content: ["./src/**/*.{html,svelte,ts}"],
};
```
