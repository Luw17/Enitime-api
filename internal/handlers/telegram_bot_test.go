package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"enitime-api/internal/config"
)

func TestTelegramWebhookRouteExists(t *testing.T) {
	handler := New(nil, config.Config{TelegramBotToken: "test-bot-token"})
	request := httptest.NewRequest(http.MethodPost, "/enitime/telegram/webhook", strings.NewReader(`{"message":{"chat":{"id":12345},"text":"/ajuda"}}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code == http.StatusNotFound {
		t.Fatalf("expected telegram webhook route to be registered, got 404")
	}
}
