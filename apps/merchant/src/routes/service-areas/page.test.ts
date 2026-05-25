import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import Page from "./+page.svelte";

const data = (myPlantCodes: string[]) => ({
  allPlants: [
    { code: "P1", label: "廠區一", address: "台北" },
    { code: "P2", label: "廠區二", address: "新竹" },
  ],
  myPlantCodes,
});

describe("/service-areas", () => {
  it("checks the checkbox for each pre-selected plant", () => {
    render(Page, { props: { data: data(["P1"]) } } as any);
    expect(screen.getByRole("checkbox", { name: /廠區一/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /廠區二/ })).not.toBeChecked();
  });

  it("opens a confirmation dialog instead of submitting when nothing is selected", async () => {
    const requestSubmit = vi
      .spyOn(HTMLFormElement.prototype, "requestSubmit")
      .mockImplementation(() => {});
    render(Page, { props: { data: data(["P1"]) } } as any);

    await fireEvent.click(screen.getByRole("checkbox", { name: /廠區一/ }));
    await fireEvent.click(screen.getByRole("button", { name: "儲存服務廠區" }));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(requestSubmit).not.toHaveBeenCalled();
  });

  it("submits after the empty selection is confirmed", async () => {
    const requestSubmit = vi
      .spyOn(HTMLFormElement.prototype, "requestSubmit")
      .mockImplementation(() => {});
    render(Page, { props: { data: data(["P1"]) } } as any);

    await fireEvent.click(screen.getByRole("checkbox", { name: /廠區一/ }));
    await fireEvent.click(screen.getByRole("button", { name: "儲存服務廠區" }));
    await fireEvent.click(screen.getByRole("button", { name: "確認暫停" }));

    expect(requestSubmit).toHaveBeenCalledOnce();
  });

  it("submits directly without confirmation when at least one plant is selected", async () => {
    const requestSubmit = vi
      .spyOn(HTMLFormElement.prototype, "requestSubmit")
      .mockImplementation(() => {});
    render(Page, { props: { data: data(["P1"]) } } as any);

    await fireEvent.click(screen.getByRole("button", { name: "儲存服務廠區" }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
