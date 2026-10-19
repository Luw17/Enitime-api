package middlewares

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"enitime-api/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

func RequireBearerToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			header := r.Header.Get("Authorization")
			providedToken := strings.TrimPrefix(header, prefix)

			if token == "" || !strings.HasPrefix(header, prefix) ||
				subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) != 1 {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequireJWT(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if secret == "" {
			writeAuthError(w, http.StatusServiceUnavailable, "Autenticação não configurada.")
			return
		}

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeAuthError(w, http.StatusUnauthorized, "Token de autorização obrigatório.")
			return
		}

		rawToken := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected JWT signing method")
			}
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || token == nil || !token.Valid {
			writeAuthError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
			return
		}

		principal, err := principalFromClaims(claims)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
			return
		}

		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
	})
}

func PrincipalFromRequest(r *http.Request) (models.Principal, bool) {
	principal, ok := r.Context().Value(principalContextKey{}).(models.Principal)
	return principal, ok
}

type principalContextKey struct{}

func withPrincipal(ctx context.Context, principal models.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func principalFromClaims(claims jwt.MapClaims) (models.Principal, error) {
	id := claimString(claims["id"])
	if id == "" {
		return models.Principal{}, fmt.Errorf("missing user id claim")
	}

	principal := models.Principal{
		ID:          id,
		Nome:        claimString(claims["nome"]),
		PodeComprar: claimBool(claims["pode_comprar"]),
		PodeVender:  claimBool(claims["pode_vender"]),
		IsAdmin:     claimBool(claims["is_admin"]),
	}
	if value := claimString(claims["nome_loja"]); value != "" {
		principal.NomeLoja = &value
	}
	if value := claimString(claims["telegram_chat_id"]); value != "" {
		principal.TelegramChatID = &value
	}

	return principal, nil
}

func claimString(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatInt(int64(value), 10)
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return ""
	}
}

func claimBool(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case float64:
		return value != 0
	default:
		return false
	}
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   true,
		"message": message,
	})
}
