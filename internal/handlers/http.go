package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRequestBody = 1 << 20

func decodeObject(r *http.Request) (map[string]json.RawMessage, error) {
	body, err := readRequestBody(r)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid JSON body")
	}
	if payload == nil {
		return nil, fmt.Errorf("JSON body must be an object")
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, err
	}

	return payload, nil
}

func readRequestBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return nil, fmt.Errorf("unable to read request body")
	}
	if len(body) > maxRequestBody {
		return nil, fmt.Errorf("request body is too large")
	}
	return body, nil
}

func decodeValue(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return fmt.Errorf("unable to read request body")
	}
	if len(body) > maxRequestBody {
		return fmt.Errorf("request body is too large")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body")
	}
	return ensureJSONEnd(decoder)
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}

func requiredString(payload map[string]json.RawMessage, key string, maxLength int) (string, error) {
	value, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("%s é obrigatório.", key)
	}
	text, err := rawString(value)
	if err != nil || text == "" {
		return "", fmt.Errorf("%s deve ser um texto não vazio.", key)
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > maxLength {
		return "", fmt.Errorf("%s deve ter entre 1 e %d caracteres.", key, maxLength)
	}
	return text, nil
}

func optionalString(payload map[string]json.RawMessage, key string, maxLength int) (*string, error) {
	value, ok := payload[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, nil
	}
	text, err := rawString(value)
	if err != nil {
		return nil, fmt.Errorf("%s deve ser um texto.", key)
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.EqualFold(text, "null") || strings.EqualFold(text, "undefined") {
		return nil, nil
	}
	if len([]rune(text)) > maxLength {
		return nil, fmt.Errorf("%s excede o limite de caracteres.", key)
	}
	return &text, nil
}

func rawString(value json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", err
	}
	return text, nil
}

func optionalBool(payload map[string]json.RawMessage, key string) (*bool, error) {
	value, ok := payload[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, nil
	}
	var flag bool
	if err := json.Unmarshal(value, &flag); err == nil {
		return &flag, nil
	}
	text, err := rawString(value)
	if err == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true":
			flag = true
		case "false":
			flag = false
		default:
			return nil, fmt.Errorf("%s deve ser booleano.", key)
		}
		return &flag, nil
	}
	return nil, fmt.Errorf("%s deve ser booleano.", key)
}

func optionalFloat(payload map[string]json.RawMessage, key string) (*float64, error) {
	value, ok := payload[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, nil
	}
	number, err := rawFloat(value)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, fmt.Errorf("%s deve ser numérico.", key)
	}
	return &number, nil
}

func requiredFloat(payload map[string]json.RawMessage, key string) (float64, error) {
	number, err := optionalFloat(payload, key)
	if err != nil || number == nil {
		return 0, fmt.Errorf("%s deve ser numérico.", key)
	}
	return *number, nil
}

func rawFloat(value json.RawMessage) (float64, error) {
	var number json.Number
	if err := json.Unmarshal(value, &number); err == nil {
		return number.Float64()
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(text), 64)
}

func optionalInt(payload map[string]json.RawMessage, key string) (*int64, error) {
	value, ok := payload[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, nil
	}
	number, err := rawFloat(value)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		number >= float64(math.MaxInt64) || number < float64(math.MinInt64) {
		return nil, fmt.Errorf("%s deve ser um número inteiro.", key)
	}
	result := int64(number)
	return &result, nil
}

func requiredInt(payload map[string]json.RawMessage, key string) (int64, error) {
	number, err := optionalInt(payload, key)
	if err != nil || number == nil {
		return 0, fmt.Errorf("%s deve ser um número inteiro.", key)
	}
	return *number, nil
}

func queryRows(r *http.Request, db databaseExecutor, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}

		record := make(map[string]any, len(columns))
		for i, column := range columns {
			record[column] = normalizeDBValue(column, columnTypes[i].DatabaseTypeName(), values[i])
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeDBValue(column, databaseType string, value any) any {
	databaseType = strings.ToUpper(databaseType)
	switch value := value.(type) {
	case []byte:
		if column == "modelos_compativeis" || strings.HasSuffix(databaseType, "[]") {
			if array, ok := parsePGStringArray(string(value)); ok {
				return array
			}
		}
		if databaseType == "JSON" || databaseType == "JSONB" ||
			column == "registros" || column == "usuarios" {
			var decoded any
			if json.Unmarshal(value, &decoded) == nil {
				return decoded
			}
		}
		return normalizeDBScalar(databaseType, string(value))
	case string:
		if column == "modelos_compativeis" || strings.HasSuffix(databaseType, "[]") {
			if array, ok := parsePGStringArray(value); ok {
				return array
			}
		}
		if databaseType == "JSON" || databaseType == "JSONB" ||
			column == "registros" || column == "usuarios" {
			var decoded any
			if json.Unmarshal([]byte(value), &decoded) == nil {
				return decoded
			}
		}
		return normalizeDBScalar(databaseType, value)
	case time.Time:
		return value
	default:
		return value
	}
}

func normalizeDBScalar(databaseType, value string) any {
	if strings.Contains(databaseType, "INT") {
		if number, err := strconv.ParseInt(value, 10, 64); err == nil {
			return number
		}
	}
	if databaseType == "NUMERIC" || databaseType == "DECIMAL" ||
		databaseType == "FLOAT4" || databaseType == "FLOAT8" {
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return number
		}
	}
	if databaseType == "BOOL" {
		if value == "t" || strings.EqualFold(value, "true") {
			return true
		}
		if value == "f" || strings.EqualFold(value, "false") {
			return false
		}
	}
	return value
}

func parsePGStringArray(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '{' || value[len(value)-1] != '}' {
		return nil, false
	}
	if value == "{}" {
		return []string{}, true
	}

	values := make([]string, 0)
	var current strings.Builder
	quoted := false
	escaped := false
	for _, char := range value[1 : len(value)-1] {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			quoted = !quoted
			continue
		}
		if char == ',' && !quoted {
			values = append(values, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(char)
	}
	if escaped || quoted {
		return nil, false
	}
	values = append(values, current.String())
	return values, true
}

func rawProductList(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("product list is missing")
	}
	var products []json.RawMessage
	if err := json.Unmarshal(raw, &products); err != nil || products == nil {
		return nil, fmt.Errorf("product list must be an array")
	}
	return products, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": true, "message": message})
}

func JSONResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := &jsonResponseWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
			body:           make([]byte, 0),
			header:         w.Header().Clone(),
		}
		response.header.Set("Content-Type", "application/json")
		next.ServeHTTP(response, r)
		response.finish()
	})
}

type jsonResponseWriter struct {
	http.ResponseWriter
	status        int
	headerWritten bool
	body          []byte
	header        http.Header
}

func (w *jsonResponseWriter) Header() http.Header {
	return w.header
}

func (w *jsonResponseWriter) WriteHeader(status int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true
	w.status = status
	if status < http.StatusBadRequest {
		w.commitHeaders()
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *jsonResponseWriter) Write(body []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= http.StatusBadRequest {
		w.body = append(w.body, body...)
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func (w *jsonResponseWriter) finish() {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.status < http.StatusBadRequest {
		return
	}
	w.commitHeaders()
	if !json.Valid(bytes.TrimSpace(w.body)) {
		w.body, _ = json.Marshal(map[string]any{
			"error":   true,
			"message": http.StatusText(w.status),
		})
		w.body = append(w.body, '\n')
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(w.body)
}

func (w *jsonResponseWriter) commitHeaders() {
	for key, values := range w.header {
		if strings.EqualFold(key, "Content-Type") {
			continue
		}
		w.ResponseWriter.Header()[key] = append([]string(nil), values...)
	}
	w.ResponseWriter.Header().Set("Content-Type", "application/json")
}

func principalOwnsID(principalID, telegramID, targetID string) bool {
	return targetID == principalID || (telegramID != "" && targetID == telegramID)
}
