export type ExperienceMode = "mobile-first" | "desktop-first";

export interface LayoutPresentation {
  shellContainerClass: string;
  navPanelClass: string;
  sectionGridClass: string;
}

export function resolveLayoutPresentation(experienceMode: ExperienceMode): LayoutPresentation {
  if (experienceMode === "mobile-first") {
    return {
      shellContainerClass: "mx-auto w-full max-w-md px-4 pb-24 pt-5 md:max-w-3xl md:px-6 md:pb-12",
      navPanelClass: "grid gap-4 rounded-2xl border border-slate-200 bg-white/90 p-4 shadow-sm",
      sectionGridClass: "grid-cols-1"
    };
  }

  return {
    shellContainerClass: "mx-auto w-full max-w-7xl px-4 pb-12 pt-5 md:px-8 lg:px-10",
    navPanelClass:
      "grid gap-4 rounded-2xl border border-slate-200 bg-white/90 p-5 shadow-sm md:grid-cols-[1fr,2fr]",
    sectionGridClass: "grid-cols-1 md:grid-cols-3"
  };
}
