package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeObject(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid object", body: `{"nome":"Ana"}`},
		{name: "invalid JSON", body: `{"nome":`, wantErr: true},
		{name: "array is not an object", body: `[]`, wantErr: true},
		{name: "null is not an object", body: `null`, wantErr: true},
		{name: "trailing JSON value", body: `{} {}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			_, err := decodeObject(request)
			if (err != nil) != test.wantErr {
				t.Fatalf("decodeObject() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDecodeObjectRejectsOversizedBody(t *testing.T) {
	request := httptest.NewRequest("POST", "/", strings.NewReader(
		`{"value":"`+strings.Repeat("x", maxRequestBody)+`"}`,
	))
	if _, err := decodeObject(request); err == nil {
		t.Fatal("expected oversized request body to be rejected")
	}
}

func TestRawProductList(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "array", raw: `[{"sku":"A"},{"sku":"B"}]`, want: 2},
		{name: "empty array", raw: `[]`, want: 0},
		{name: "null", raw: `null`, wantErr: true},
		{name: "object", raw: `{"sku":"A"}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := rawProductList(json.RawMessage(test.raw))
			if (err != nil) != test.wantErr {
				t.Fatalf("rawProductList() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && len(got) != test.want {
				t.Fatalf("rawProductList() returned %d products, want %d", len(got), test.want)
			}
		})
	}
}

func TestRequiredString(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]json.RawMessage
		want    string
		wantErr bool
	}{
		{name: "trims valid value", payload: map[string]json.RawMessage{"name": json.RawMessage(`" Ana "`)}, want: "Ana"},
		{name: "missing", payload: map[string]json.RawMessage{}, wantErr: true},
		{name: "empty", payload: map[string]json.RawMessage{"name": json.RawMessage(`"  "`)}, wantErr: true},
		{name: "wrong type", payload: map[string]json.RawMessage{"name": json.RawMessage(`42`)}, wantErr: true},
		{name: "too long", payload: map[string]json.RawMessage{"name": json.RawMessage(`"abcd"`)}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := requiredString(test.payload, "name", 3)
			if (err != nil) != test.wantErr {
				t.Fatalf("requiredString() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("requiredString() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOptionalBool(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *bool
		wantErr bool
	}{
		{name: "boolean true", raw: `true`, want: boolPointer(true)},
		{name: "boolean string false", raw: `"false"`, want: boolPointer(false)},
		{name: "null", raw: `null`},
		{name: "invalid text", raw: `"yes"`, wantErr: true},
		{name: "number", raw: `1`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := optionalBool(map[string]json.RawMessage{"enabled": json.RawMessage(test.raw)}, "enabled")
			if (err != nil) != test.wantErr {
				t.Fatalf("optionalBool() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.want != nil && (got == nil || *got != *test.want) {
				t.Fatalf("optionalBool() = %v, want %v", got, *test.want)
			}
			if test.want == nil && !test.wantErr && got != nil {
				t.Fatalf("optionalBool() = %v, want nil", *got)
			}
		})
	}
}

func TestNumericValidation(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{name: "positive integer", raw: `9`, want: 9},
		{name: "numeric string", raw: `"9"`, want: 9},
		{name: "fractional integer", raw: `9.5`, wantErr: true},
		{name: "infinity string", raw: `"Inf"`, wantErr: true},
		{name: "null integer", raw: `null`, wantErr: true},
		{name: "boolean integer", raw: `true`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := requiredInt(map[string]json.RawMessage{"value": json.RawMessage(test.raw)}, "value")
			if (err != nil) != test.wantErr {
				t.Fatalf("requiredInt() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("requiredInt() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestParseCompatibleModels(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "trims array values", raw: `[" modelo A ","","modelo B"]`, want: []string{"modelo A", "modelo B"}},
		{name: "accepts single value", raw: `" modelo A "`, want: []string{"modelo A"}},
		{name: "empty array", raw: `[]`, want: []string{}},
		{name: "null", raw: `null`, want: []string{}},
		{name: "invalid type", raw: `42`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseCompatibleModels(json.RawMessage(test.raw))
			if (err != nil) != test.wantErr {
				t.Fatalf("parseCompatibleModels() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && !equalStrings(got, test.want) {
				t.Fatalf("parseCompatibleModels() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestPagination(t *testing.T) {
	tests := []struct {
		name      string
		page      string
		limit     string
		wantPage  int64
		wantLimit int64
	}{
		{name: "defaults", wantPage: 1, wantLimit: 20},
		{name: "clamps maximum page size", page: "2", limit: "500", wantPage: 2, wantLimit: 100},
		{name: "replaces invalid values", page: "-1", limit: "invalid", wantPage: 1, wantLimit: 20},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, limit := pagination(test.page, test.limit)
			if page != test.wantPage || limit != test.wantLimit {
				t.Fatalf("pagination() = (%d, %d), want (%d, %d)", page, limit, test.wantPage, test.wantLimit)
			}
		})
	}
}

func TestRequestChatID(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{name: "telegram chat id", payload: `{"telegram_chat_id":"123"}`, want: "123"},
		{name: "numeric user id", payload: `{"user_id":456}`, want: "456"},
		{name: "nested Telegram chat id", payload: `{"initDataUnsafe":{"chat":{"id":789}}}`, want: "789"},
		{name: "rejects text ID", payload: `{"user_id":"not-numeric"}`, wantErr: true},
		{name: "missing", payload: `{}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(test.payload), &payload); err != nil {
				t.Fatal(err)
			}
			got, err := requestChatID(payload)
			if (err != nil) != test.wantErr {
				t.Fatalf("requestChatID() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("requestChatID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPrincipalOwnsID(t *testing.T) {
	tests := []struct {
		name       string
		principal  string
		telegramID string
		target     string
		want       bool
	}{
		{name: "owns database ID", principal: "12", target: "12", want: true},
		{name: "owns Telegram ID", principal: "12", telegramID: "900", target: "900", want: true},
		{name: "does not own another user", principal: "12", telegramID: "900", target: "13"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := principalOwnsID(test.principal, test.telegramID, test.target); got != test.want {
				t.Fatalf("principalOwnsID() = %v, want %v", got, test.want)
			}
		})
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
