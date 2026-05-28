package hydra

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

// Bridge serves login + consent + OIDC callback endpoints that connect Hydra's
// OAuth surface to the T-Bite identity model. The user-auth leg is delegated
// to Authentik via OIDC; no password ever touches T-Bite or Hydra. Flow:
// Hydra /oauth/login → Authentik authorize → /oauth/callback → accept Hydra
// login with the resolved user ID → Hydra /oauth/consent (auto-approved).
type Bridge struct {
	Hydra      *AdminClient
	Sessions   identity.SessionStore
	Users      identity.UserRepository
	Identities identity.UserIdentityRepository

	// OIDCProvider is used directly (not via identity.Service.StartLogin) since
	// we want only the user ID for the Hydra subject claim, not a frontend session.
	OIDCProvider *oidc.OIDCProvider
	// OIDCProviderName matches the slug used for UserIdentity rows (AUTH_PROVIDER_SLUGS).
	OIDCProviderName string

	// States is the shared Redis-backed OIDC state store, with its own keyspace prefix.
	States oidc.StateStore

	// PublicBaseURL is the externally-reachable URL of the T-Bite API; used to
	// build the Authentik redirect_uri.
	PublicBaseURL string
}

const bridgeStateApp = "mcp" // disambiguate from SvelteKit flows in shared stores

// LoginHandler answers Hydra's redirect to /oauth/login?login_challenge=xxx.
// No HTML form, no token-paste — we always kick off the Authentik OIDC
// flow when there's no remembered session.
func (b *Bridge) LoginHandler(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "missing login_challenge", http.StatusBadRequest)
		return
	}

	loginReq, err := b.Hydra.GetLoginRequest(r.Context(), challenge)
	if err != nil {
		http.Error(w, "fetch login request: "+err.Error(), http.StatusBadGateway)
		return
	}
	if loginReq.Skip {
		// Hydra recognised a remembered subject; accept and continue.
		b.acceptLoginAndRedirect(w, r, challenge, loginReq.Subject)
		return
	}

	if b.OIDCProvider == nil {
		http.Error(w, "OIDC provider not configured", http.StatusInternalServerError)
		return
	}

	// Build an Authentik authorize URL; redirect_uri is /oauth/callback
	// (must match the Authentik client config).
	state := uuid.NewString()
	auth, err := b.OIDCProvider.BuildAuthURL(r.Context(), state)
	if err != nil {
		http.Error(w, "build auth url: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Stash login_challenge + PKCE verifier + nonce keyed by our state (5m TTL).
	if err := b.States.Put(r.Context(), state, &oidc.StatePayload{
		App:          bridgeStateApp,
		Provider:     b.OIDCProviderName,
		ReturnTo:     challenge,
		PKCEVerifier: auth.PKCEVerifier,
		Nonce:        auth.Nonce,
	}); err != nil {
		http.Error(w, "state store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, auth.URL, http.StatusFound)
}

// CallbackHandler is the Authentik OIDC callback that completes the user
// authentication leg. It exchanges the code, provisions/looks up the user,
// accepts the Hydra login with that user's ID as subject, then forwards
// the browser onward to Hydra's consent step.
func (b *Bridge) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "missing state/code", http.StatusBadRequest)
		return
	}

	payload, err := b.States.Get(r.Context(), state)
	if err != nil {
		http.Error(w, "state lookup: "+err.Error(), http.StatusBadRequest)
		return
	}
	if payload.App != bridgeStateApp {
		// State belongs to a different flow (e.g. employee web login).
		http.Error(w, "state app mismatch", http.StatusBadRequest)
		return
	}
	_ = b.States.Consume(r.Context(), state)

	ui, err := b.OIDCProvider.Exchange(r.Context(), code, payload.PKCEVerifier, payload.Nonce)
	if err != nil {
		http.Error(w, "oidc exchange: "+err.Error(), http.StatusBadGateway)
		return
	}
	if ui.Email == "" || ui.ExternalSubject == "" {
		http.Error(w, "oidc claims missing subject/email", http.StatusBadRequest)
		return
	}

	user, err := b.resolveOrProvisionUser(r.Context(), ui)
	if err != nil {
		http.Error(w, "user resolve: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if user.Status != identity.StatusActive {
		http.Error(w, "account suspended", http.StatusLocked)
		return
	}

	loginChallenge := payload.ReturnTo
	b.acceptLoginAndRedirect(w, r, loginChallenge, user.ID)
}

// resolveOrProvisionUser finds a T-Bite user matching the OIDC subject. Uses
// the same lookup-then-link logic as identity.Service.CompleteLogin minus the
// role gate (MCP supports every role).
func (b *Bridge) resolveOrProvisionUser(ctx context.Context, ui *oidc.Userinfo) (*identity.User, error) {
	provider := identity.Provider(b.OIDCProviderName)

	// Already linked via user_identity.
	link, err := b.Identities.GetByProviderSubject(ctx, provider, ui.ExternalSubject)
	if err == nil {
		return b.Users.GetByID(ctx, link.UserID)
	}
	if !isNotFound(err) {
		return nil, err
	}

	// Same email exists from a prior provider — link the identity.
	existing, err := b.Users.GetByEmail(ctx, ui.Email)
	if err == nil {
		if err := b.Identities.Link(ctx, &identity.UserIdentity{
			UserID:          existing.ID,
			Provider:        provider,
			ExternalSubject: ui.ExternalSubject,
			RawClaims:       ui.Raw,
		}); err != nil {
			return nil, fmt.Errorf("link identity: %w", err)
		}
		return existing, nil
	}
	if !isNotFound(err) {
		return nil, err
	}

	// Auto-provision from Authentik claims (no app-vs-role gate).
	user, err := userFromOIDCClaims(ui)
	if err != nil {
		return nil, err
	}
	if err := b.Users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if err := b.Identities.Link(ctx, &identity.UserIdentity{
		UserID:          user.ID,
		Provider:        provider,
		ExternalSubject: ui.ExternalSubject,
		RawClaims:       ui.Raw,
	}); err != nil {
		return nil, fmt.Errorf("link identity: %w", err)
	}
	return user, nil
}

// userFromOIDCClaims maps Authentik OIDC userinfo to a fresh identity.User.
// Mirrors identity.Service.userFromClaims minus the app-role gate.
func userFromOIDCClaims(ui *oidc.Userinfo) (*identity.User, error) {
	roleStr := claimString(ui.Raw, "tbite_role")
	role := identity.Role(roleStr)
	switch role {
	case identity.RoleEmployee, identity.RoleVendorOperator, identity.RoleWelfareAdmin:
	default:
		return nil, fmt.Errorf("oidc claims missing or invalid tbite_role (got %q)", roleStr)
	}

	u := &identity.User{
		PrimaryEmail: strings.ToLower(ui.Email),
		DisplayName:  ui.DisplayName,
		Role:         role,
		Status:       identity.StatusActive,
	}
	switch role {
	case identity.RoleEmployee:
		employeeID := claimString(ui.Raw, "tbite_employee_id")
		plant := claimString(ui.Raw, "tbite_plant")
		department := claimString(ui.Raw, "tbite_department")
		if employeeID == "" || plant == "" {
			return nil, fmt.Errorf("employee claims require tbite_employee_id and tbite_plant")
		}
		u.EmployeeID = &employeeID
		u.Plant = &plant
		if department != "" {
			u.Department = &department
		}
	case identity.RoleVendorOperator:
		vendorID := claimString(ui.Raw, "tbite_vendor_id")
		if vendorID == "" {
			return nil, fmt.Errorf("vendor operator requires tbite_vendor_id")
		}
		u.VendorID = &vendorID
	}
	return u, nil
}

func claimString(claims map[string]any, key string) string {
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ConsentHandler answers /oauth/consent?consent_challenge=xxx. Auto-approves
// with the user's role/plant claims forwarded into Hydra's session — MCP
// clients are first-party agents, so a consent screen would only add friction.
func (b *Bridge) ConsentHandler(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" {
		http.Error(w, "missing consent_challenge", http.StatusBadRequest)
		return
	}
	consentReq, err := b.Hydra.GetConsentRequest(r.Context(), challenge)
	if err != nil {
		http.Error(w, "fetch consent request: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Claim set Hydra embeds in the JWT; lifting role/plant/department off
	// the user lets MCP tools authorise without an extra DB lookup.
	claims := map[string]any{}
	if user, err := b.Users.GetByID(r.Context(), consentReq.Subject); err == nil {
		claims["email"] = user.PrimaryEmail
		claims["name"] = user.DisplayName
		claims["tbite_role"] = string(user.Role)
		if user.Plant != nil {
			claims["tbite_plant"] = *user.Plant
		}
		if user.Department != nil {
			claims["tbite_department"] = *user.Department
		}
	}

	redirectTo, err := b.Hydra.AcceptConsentRequest(r.Context(), challenge, consentReq.RequestedScope, claims)
	if err != nil {
		http.Error(w, "accept consent: "+err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (b *Bridge) acceptLoginAndRedirect(w http.ResponseWriter, r *http.Request, challenge, subject string) {
	redirectTo, err := b.Hydra.AcceptLoginRequest(r.Context(), challenge, subject)
	if err != nil {
		http.Error(w, "accept login: "+err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// isNotFound returns true for the identity package's NotFound sentinels.
// Matches on error string to avoid an import cycle on the typed sentinels.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "identity: user not found" ||
		err.Error() == "identity: identity not found" ||
		err == identity.ErrUserNotFound ||
		err == identity.ErrIdentityNotFound
}
