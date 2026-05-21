// Shared UI flags for the global overlays (cart drawer, notification modal).
// Lives outside the layout so any screen can open them — e.g. the home
// header bell, or a vendor screen's "view cart" action.
class UiState {
  cartOpen = $state(false);
  notifOpen = $state(false);
}

export const uiState = new UiState();
