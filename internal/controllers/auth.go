package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"seo-crawler/internal/models"

	"github.com/o1egl/paseto/v2"
	"golang.org/x/crypto/bcrypt"
)

type authRequest struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// minPasswordLength matches the signup form's client-side minlength hint —
// enforced here too, since a client-side attribute alone is trivially
// bypassed by calling the API directly.
const minPasswordLength = 8

// claimsExpired reports whether a decrypted PASETO token's "exp" claim is
// missing, unparseable, or in the past. The cookie's own Expires attribute
// only controls browser-side deletion — the server must independently
// enforce expiry, since a copied/replayed cookie would otherwise remain
// valid forever.
func claimsExpired(claims map[string]interface{}) bool {
	expStr, ok := claims["exp"].(string)
	if !ok {
		return true
	}
	exp, err := time.Parse(time.RFC3339, expStr)
	if err != nil {
		return true
	}
	return time.Now().After(exp)
}

// issueSessionCookies encrypts a fresh PASETO token for (userID, name,
// email) and sets both session cookies on the response. Shared by login,
// refresh, and account updates (a name change must reissue the cookie too,
// since the display name is baked into the token's claims).
func (c *Controller) issueSessionCookies(w http.ResponseWriter, userID, name, email string) error {
	v2 := paseto.NewV2()
	now := time.Now()
	exp := now.Add(24 * time.Hour)

	claims := map[string]interface{}{
		"sub":     userID,
		"user_id": userID,
		"name":    name,
		"email":   email,
		"iat":     now.Format(time.RFC3339),
		"exp":     exp.Format(time.RFC3339),
	}

	token, err := v2.Encrypt(c.PasetoKey, claims, nil)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "auth_user",
		// URL-encoded because Go's net/http auto-wraps a raw cookie value
		// containing spaces in double quotes (per RFC 6265's restriction on
		// bare cookie-octets) — those literal quote characters would then
		// show up in the UI for any name with a space (most names). PathEscape
		// (not QueryEscape) specifically: QueryEscape encodes a space as "+"
		// (form-encoding convention), but JS's decodeURIComponent only
		// understands "%20" — it leaves a literal "+" untouched. The header's
		// getCookie() calls decodeURIComponent, so PathEscape is the one that
		// actually round-trips correctly.
		Value:    url.PathEscape(name),
		Path:     "/",
		HttpOnly: false,
		Secure:   c.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
	return nil
}

func (c *Controller) HandleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" || req.Name == "" {
		http.Error(w, `{"error":"name, email and password are required"}`, http.StatusBadRequest)
		return
	}
	if len(req.Password) < minPasswordLength {
		http.Error(w, fmt.Sprintf(`{"error":"password must be at least %d characters"}`, minPasswordLength), http.StatusBadRequest)
		return
	}

	existing, err := c.Store.GetUserByEmail(req.Email)
	if err != nil {
		log.Printf("error checking existing user: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if existing != nil {
		http.Error(w, `{"error":"email is already registered"}`, http.StatusConflict)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("error hashing password: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	user := &models.User{
		Name:         req.Name,
		Email:        req.Email,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	if err := c.Store.CreateUser(user); err != nil {
		log.Printf("error creating user: %v", err)
		http.Error(w, `{"error":"failed to create user"}`, http.StatusInternalServerError)
		return
	}

	// Fire-and-forget: email delivery must never block or fail the signup
	// response — SMTP can be slow or briefly unavailable, and account
	// creation already succeeded regardless of whether the email lands.
	if c.Mailer != nil {
		go func(email, name string) {
			if err := c.Mailer.SendHTML(email, "Welcome to Auditly!", welcomeEmailHTML(name)); err != nil {
				log.Printf("failed to send welcome email to %s: %v", email, err)
			}
		}(user.Email, user.Name)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Account created successfully!"})
}

func (c *Controller) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		http.Error(w, `{"error":"email and password are required"}`, http.StatusBadRequest)
		return
	}

	user, err := c.Store.GetUserByEmail(req.Email)
	if err != nil {
		log.Printf("error getting user: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, `{"error":"invalid email or password"}`, http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		http.Error(w, `{"error":"invalid email or password"}`, http.StatusUnauthorized)
		return
	}

	if err := c.issueSessionCookies(w, user.ID, user.Name, user.Email); err != nil {
		log.Printf("error generating paseto token: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Logged in successfully!",
		"user": map[string]string{
			"name":  user.Name,
			"email": user.Email,
		},
	})
}

func (c *Controller) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_user",
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Logged out successfully!",
	})
}

func (c *Controller) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("auth_token")
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	v2 := paseto.NewV2()
	var claims map[string]interface{}
	err = v2.Decrypt(cookie.Value, c.PasetoKey, &claims, nil)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if claimsExpired(claims) {
		http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
		return
	}

	userID, _ := claims["user_id"].(string)
	name, _ := claims["name"].(string)
	email, _ := claims["email"].(string)

	if userID == "" {
		if sub, ok := claims["sub"].(string); ok {
			userID = sub
		}
	}

	if userID == "" {
		http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
		return
	}

	if err := c.issueSessionCookies(w, userID, name, email); err != nil {
		log.Printf("error refreshing paseto token: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user": map[string]string{
			"name":  name,
			"email": email,
		},
	})
}

// HandleMe returns the logged-in user's own name/email, for populating the
// account settings form. Reads straight from the session cookie's claims
// (already trusted, already up to date after any profile change re-issues
// the cookie) rather than a DB round trip.
func (c *Controller) HandleMe(w http.ResponseWriter, r *http.Request) {
	userID, email := c.getUserIDAndEmail(r)
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	name := ""
	if cookie, err := r.Cookie("auth_user"); err == nil {
		name = cookie.Value
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"name": name, "email": email})
}

type updateProfileRequest struct {
	Name string `json:"name"`
}

// HandleUpdateProfile changes the logged-in user's display name and
// re-issues the session cookie, since the name is baked into its claims —
// without this, the old name would keep showing up everywhere (header,
// emails) until the next login.
func (c *Controller) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	userID, email := c.getUserIDAndEmail(r)
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := c.Store.UpdateUserName(userID, req.Name); err != nil {
		log.Printf("failed to update name for user %s: %v", userID, err)
		http.Error(w, `{"error":"failed to update profile"}`, http.StatusInternalServerError)
		return
	}

	if err := c.issueSessionCookies(w, userID, req.Name, email); err != nil {
		log.Printf("error re-issuing session token after profile update: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Profile updated!",
		"user":    map[string]string{"name": req.Name, "email": email},
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// HandleChangePassword updates the logged-in user's password after
// verifying their current one — a standard safeguard against a hijacked,
// still-logged-in session being used to lock the real owner out.
func (c *Controller) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	userID := c.getUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		http.Error(w, `{"error":"current and new password are required"}`, http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < minPasswordLength {
		http.Error(w, fmt.Sprintf(`{"error":"new password must be at least %d characters"}`, minPasswordLength), http.StatusBadRequest)
		return
	}

	user, err := c.Store.GetUserByID(userID)
	if err != nil {
		log.Printf("failed to load user %s for password change: %v", userID, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		http.Error(w, `{"error":"current password is incorrect"}`, http.StatusUnauthorized)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("error hashing new password: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	if err := c.Store.UpdateUserPassword(userID, string(hash)); err != nil {
		log.Printf("failed to update password for user %s: %v", userID, err)
		http.Error(w, `{"error":"failed to update password"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Password updated!"})
}
