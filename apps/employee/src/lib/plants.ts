// Plant + day helpers for the header LocationBar. Day logic lives in
// ./date (shared with the home +page.server.ts) so the 7-day window and the
// Asia/Taipei date handling stay in one place.
export { buildDays, taipeiISO, type DayOption } from "./date";

export interface PlantOption {
  id: string;
  label: string;
}
