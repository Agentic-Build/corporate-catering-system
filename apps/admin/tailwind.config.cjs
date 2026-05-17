const preset = require("@tbite/tokens/tailwind");
module.exports = {
  presets: [preset],
  content: ["./src/**/*.{html,svelte,ts}", "../../packages/ui/src/**/*.{svelte,ts}"],
};
