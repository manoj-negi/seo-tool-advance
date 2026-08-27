package controllers

import (
	"encoding/json"
	"log"
	"net/http"
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

	// Generate PASETO token
	v2 := paseto.NewV2()
	now := time.Now()
	exp := now.Add(24 * time.Hour)

	claims := map[string]interface{}{
		"sub":     user.ID,
		"user_id": user.ID,
		"name":    user.Name,
		"email":   user.Email,
		"iat":     now.Format(time.RFC3339),
		"exp":     exp.Format(time.RFC3339),
	}

	token, err := v2.Encrypt(c.PasetoKey, claims, nil)
	if err != nil {
		log.Printf("error generating paseto token: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
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
		Name:     "auth_user",
		Value:    user.Name,
		Path:     "/",
		HttpOnly: false,
		Secure:   c.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})

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

	// Issue fresh token
	now := time.Now()
	exp := now.Add(24 * time.Hour)

	newClaims := map[string]interface{}{
		"sub":     userID,
		"user_id": userID,
		"name":    name,
		"email":   email,
		"iat":     now.Format(time.RFC3339),
		"exp":     exp.Format(time.RFC3339),
	}

	token, err := v2.Encrypt(c.PasetoKey, newClaims, nil)
	if err != nil {
		log.Printf("error refreshing paseto token: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
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
		Name:     "auth_user",
		Value:    name,
		Path:     "/",
		HttpOnly: false,
		Secure:   c.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user": map[string]string{
			"name":  name,
			"email": email,
		},
	})
}
