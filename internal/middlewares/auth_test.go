package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestRequireJWTValidatesTokenAndExposesClaims(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id":               "42",
		"nome":             "Fornecedor",
		"pode_vender":      "true",
		"pode_comprar":     false,
		"is_admin":         false,
		"telegram_chat_id": "9001",
	})
	rawToken, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	handler := RequireJWT("test-secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromRequest(r)
		if !ok {
			t.Fatal("principal was not added to request context")
		}
		if principal.ID != "42" || !principal.PodeVender || principal.TelegramChatID == nil ||
			*principal.TelegramChatID != "9001" {
			t.Fatalf("unexpected principal: %#v", principal)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
}

func TestRequireJWTRejectsInvalidTokens(t *testing.T) {
	handler := RequireJWT("test-secret", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called")
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestRequireJWTRejectsExpiredTokens(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id":  "42",
		"exp": float64(1),
	})
	rawToken, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	handler := RequireJWT("test-secret", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}
