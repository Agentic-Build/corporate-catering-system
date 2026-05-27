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

// Bridge serves the login + consent + OIDC callback endpoints that connect
// Hydra's OAuth surface to the T-Bite identity model. The user-auth leg is
// delegated to Authentik via OIDC; no password ever touches T-Bite or
// Hydra, and there is no "paste your token" step.
//
// Flow:
//  1. Hydra GET /oauth/login?login_challenge=xxx
//     Bridge.LoginHandler asks Hydra for the request. If Hydra says skip
//     (existing remembered subject), accept immediately. Otherwise the
//     handler builds an Authentik authorize URL with state stuffing the
//     login_challenge into the OIDC state-store payload, then 302s the
//     browser to Authentik.
//  2. Authentik GET /oauth/callback?state=…&code=…
//     Bridge.CallbackHandler looks up the state, exchanges the code via
//     the OIDC provider, finds/creates the matching T-Bite user, accepts
//     the Hydra login with user.ID as subject, and 302s back to Hydra's
//     consent endpoint.
//  3. Hydra GET /oauth/consent?consent_challenge=xxx
//     Bridge.ConsentHandler auto-approves with the user's role/plant
//     claims forwarded into Hydra's session so they land in the JWT.
type Bridge struct {
	Hydra      *AdminClient
	Sessions   identity.SessionStore
	Users      identity.UserRepository
	Identities identity.UserIdentityRepository

	// OIDCProvider is the Authentik provider used for user authentication.
	// We use it directly rather than going through identity.Service.StartLogin
	// because we don't want a SvelteKit-frontend session at the end — only
	// the user ID, which becomes the Hydra subject claim.
	OIDCProvider *oidc.OIDCProvider
	// OIDCProviderName is the Provider slug used to key UserIdentity rows
	// (matches AUTH_PROVIDER_SLUGS).
	OIDCProviderName string

	// States is the same Redis-backed OIDC state store identity.Service
	// uses, just with our own keyspace prefix avoiding collisions.
	States oidc.StateStore

	// PublicBaseURL is the externally-reachable URL of the T-Bite API; we
	// use it to build the Authentik redirect_uri (must match what's
	// configured on the OAuth provider).
	PublicBaseURL string
}

const bridgeStateApp = "mcp" // marker so we don't confuse this state with
//                              SvelteKit frontends in shared stores

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
		// Hydra recognised the same subject from a previous remember-me
		// approval; accept with that subject and continue.
		b.acceptLoginAndRedirect(w, r, challenge, loginReq.Subject)
		return
	}

	if b.OIDCProvider == nil {
		http.Error(w, "OIDC provider not configured", http.StatusInternalServerError)
		return
	}

	// Build an Authentik authorize URL. We pin the redirect_uri to our own
	// /oauth/callback (must match the Authentik client config).
	state := uuid.NewString()
	auth, err := b.OIDCProvider.BuildAuthURL(r.Context(), state)
	if err != nil {
		http.Error(w, "build auth url: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Stash login_challenge + PKCE verifier + nonce keyed by our state.
	// 5-minute TTL matches the identity package default — OIDC flows that
	// take longer than that are almost certainly abandoned.
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
		// This state belongs to a different flow (employee web login).
		// Forward to the regular callback path to avoid silent failures.
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

// resolveOrProvisionUser finds a T-Bite user matching the OIDC subject. It
// uses the same lookup-then-link logic identity.Service.CompleteLogin uses,
// minus the role gate (MCP supports every role).
func (b *Bridge) resolveOrProvisionUser(ctx context.Context, ui *oidc.Userinfo) (*identity.User, error) {
	provider := identity.Provider(b.OIDCProviderName)

	// 1) Already linked via user_identity.
	link, err := b.Identities.GetByProviderSubject(ctx, provider, ui.ExternalSubject)
	if err == nil {
		return b.Users.GetByID(ctx, link.UserID)
	}
	if !isNotFound(err) {
		return nil, err
	}

	// 2) Same email exists from a prior provider — link the identity.
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

	// 3) Auto-provision from Authentik claims. We replicate the role +
	// claim validation identity.Service.CompleteLogin does — without the
	// app-vs-role gate, since MCP accepts every role the user is granted.
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
// Mirrors the logic in identity.Service.userFromClaims minus the app-role
// gate. The Authentik blueprint already populates tbite_* claims for every
// dev user via the T-Bite claim mapper.
func userFromOIDCClaims(ui *oidc.Userinfo) (*identity.User, error) {
	roleStr := claimString(ui.Raw, "tbite_role")
	role := identity.Role(roleStr)
	switch role {
	case identity.RoleEmployee, identity.RoleVendorOperator, identity.RoleWelfareAdmin:
		// ok
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

// ConsentHandler answers /oauth/consent?consent_challenge=xxx. We always
// auto-approve: MCP clients in our deployment are first-party agents
// acting for the calling employee, so adding a consent screen would only
// be friction. The granted scopes are exactly what the client requested.
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

	// Build the claim set Hydra will embed in the JWT access token. We
	// lift role / plant / department off the user so our MCP tools can
	// authorise without an extra DB lookup.
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
// We can't import the exact error types without a cycle, so match on
// error string — cheap and resilient.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "identity: user not found" ||
		err.Error() == "identity: identity not found" ||
		err == identity.ErrUserNotFound ||
		err == identity.ErrIdentityNotFound
}
