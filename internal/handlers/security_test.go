package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"enitime-api/internal/config"
)

func TestVerifyTelegramInitData(t *testing.T) {
	values := url.Values{
		"auth_date": {"1700000000"},
		"query_id":  {"query"},
		"user":      {`{"id":12345,"first_name":"Ana"}`},
	}
	lines := []string{
		"auth_date=1700000000",
		"query_id=query",
		`user={"id":12345,"first_name":"Ana"}`,
	}
	keyMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = keyMAC.Write([]byte("test-bot-token"))
	dataMAC := hmac.New(sha256.New, keyMAC.Sum(nil))
	_, _ = dataMAC.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(dataMAC.Sum(nil)))

	if !verifyTelegramInitData(values, "test-bot-token") {
		t.Fatal("expected signed initData to be valid")
	}
	values.Set("user", `{"id":67890,"first_name":"Malicious"}`)
	if verifyTelegramInitData(values, "test-bot-token") {
		t.Fatal("expected modified initData to be rejected")
	}
}

func TestProtectedRoutesRequireJWT(t *testing.T) {
	handler := New(nil, config.Config{JWTSecret: "test-secret"})
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/pedidos?user_id=42"},
		{http.MethodGet, "/enitime/produtos"},
		{http.MethodGet, "/enitime/produtos/1"},
		{http.MethodPost, "/enitime/produtos/bulk"},
		{http.MethodPost, "/enitime/produtos/"},
		{http.MethodPatch, "/enitime/produtos/"},
		{http.MethodPost, "/enitime/users"},
		{http.MethodPatch, "/enitime/users"},
		{http.MethodGet, "/enitime/users"},
		{http.MethodGet, "/enitime/pedidos-fornecedor"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("expected JSON content type, got %q", response.Header().Get("Content-Type"))
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("expected JSON error body: %v", err)
			}
		})
	}
}

func TestJSONResponsesFormatsRouterErrors(t *testing.T) {
	handler := New(nil, config.Config{JWTSecret: "test-secret"})
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "not found", method: http.MethodGet, path: "/not-found", status: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodDelete, path: "/enitime/login", status: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("expected status %d, got %d", test.status, response.Code)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("expected JSON content type, got %q", response.Header().Get("Content-Type"))
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("expected JSON error body: %v", err)
			}
			if body["error"] != true {
				t.Fatalf("expected standardized error object, got %#v", body)
			}
		})
	}
}

func TestParsePGStringArray(t *testing.T) {
	values, ok := parsePGStringArray(`{"modelo A","modelo, B","modelo\"C"}`)
	if !ok || len(values) != 3 || values[1] != "modelo, B" || values[2] != `modelo"C` {
		t.Fatalf("unexpected array parse: %#v, %v", values, ok)
	}
}

func TestNormalizeDBScalarPreservesTextAndConvertsNumbers(t *testing.T) {
	if got := normalizeDBScalar("TEXT", "123"); got != "123" {
		t.Fatalf("expected text to remain a string, got %#v", got)
	}
	if got := normalizeDBScalar("NUMERIC", "12.5"); got != 12.5 {
		t.Fatalf("expected numeric value, got %#v", got)
	}
	if got := normalizeDBScalar("INT8", "42"); got != int64(42) {
		t.Fatalf("expected integer value, got %#v", got)
	}
}

func TestRegisterRejectsInvalidTelegramSignature(t *testing.T) {
	handler := New(nil, config.Config{
		TelegramBotToken:    "test-bot-token",
		TelegramAdminChatID: "1",
	})
	request := httptest.NewRequest(http.MethodPost, "/enitime/registrar", strings.NewReader(`{"initData":"user=bad&hash=bad"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected response: status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
	}
	var responseBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
		t.Fatalf("expected JSON error response: %v", err)
	}
	if responseBody["error"] != true || responseBody["message"] != "autenticação invalida" {
		t.Fatalf("unexpected response body: %#v", responseBody)
	}
}
