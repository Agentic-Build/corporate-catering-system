/** @type {import('tailwindcss').Config} */
module.exports = {
  theme: {
    extend: {
      colors: {
        "tb-red": {
          50: "#fef2f2",
          500: "#ef4444",
          600: "#dc2626",
          700: "#b91c1c",
        },
        "tb-rose": {
          50: "#fff1f2",
          600: "#e11d48",
          700: "#be123c",
        },
        "tb-amber": {
          50: "#fffbeb",
          300: "#fcd34d",
          400: "#fbbf24",
        },
        "tb-slate": {
          50: "#f8fafc",
          100: "#f1f5f9",
          200: "#e2e8f0",
          300: "#cbd5e1",
          500: "#64748b",
          800: "#1e293b",
          900: "#0f172a",
          950: "#020617",
        },
        "tb-emerald": { 500: "#10b981" },
      },
      fontFamily: {
        "noto-tc": [
          '"Noto Sans TC"',
          '"PingFang TC"',
          '"Microsoft JhengHei"',
          "system-ui",
          "sans-serif",
        ],
        "jetbrains-mono": ['"JetBrains Mono"', "ui-monospace", "monospace"],
      },
      borderRadius: { "tb-2xl": "16px" },
      boxShadow: {
        "tb-sm": "0 1px 2px 0 rgba(15, 23, 42, 0.06)",
        "tb-md": "0 4px 6px -1px rgba(15, 23, 42, 0.10)",
      },
      letterSpacing: { eyebrow: "0.18em", "eyebrow-wide": "0.22em" },
      keyframes: {
        "tb-fade-up": {
          "0%": { opacity: "0", transform: "translateY(8px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        "tb-cart-bump": {
          "0%,100%": { transform: "scale(1)" },
          "50%": { transform: "scale(1.18)" },
        },
      },
      animation: {
        "tb-fade-up": "tb-fade-up 220ms cubic-bezier(.2,0,.2,1) both",
        "tb-cart-bump": "tb-cart-bump 320ms cubic-bezier(.2,0,.2,1)",
      },
    },
  },
};
