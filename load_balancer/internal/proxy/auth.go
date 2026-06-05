package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("super-secret-key")

// -------------------- JWT --------------------

func validateJWT(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {

		// enforce HMAC only
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}

		return jwtSecret, nil
	})
}

// -------------------- CONTEXT KEY --------------------

type contextKey string

const userKey = contextKey("user")

// -------------------- SAFE BODY READER --------------------

func readBody(r *http.Request) ([]byte, *http.Request) {
	if r.Body == nil {
		return nil, r
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	return body, r
}

// -------------------- MIDDLEWARE --------------------

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// allow login without auth
		if r.URL.Path == "/login" {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "invalid authorization format", http.StatusUnauthorized)
			return
		}

		token, err := validateJWT(parts[1])
		if err != nil || token == nil || !token.Valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "invalid token claims", http.StatusUnauthorized)
			return
		}

		user, ok := claims["sub"].(string)
		if !ok || user == "" {
			http.Error(w, "invalid subject claim", http.StatusUnauthorized)
			return
		}

		// attach user to context
		ctx := context.WithValue(r.Context(), userKey, user)

		next(w, r.WithContext(ctx))
	}
}

// -------------------- LOGIN --------------------

func LoginHandler(w http.ResponseWriter, r *http.Request) {

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.Unmarshal(body, &creds); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if creds.Username != "admin" || creds.Password != "password" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": creds.Username,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		http.Error(w, "could not create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": tokenString,
	})
}

// -------------------- CONTEXT HELPER --------------------

func GetUser(r *http.Request) (string, bool) {
	user, ok := r.Context().Value(userKey).(string)
	return user, ok
}