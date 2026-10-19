package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"enitime-api/internal/config"
	"enitime-api/internal/database"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

func TestProductsPostgresIntegration(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Logf("local .env not loaded: %v", err)
	}
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		t.Skip("DATABASE_URL or DB_HOST is not configured")
	}

	db, err := database.ConnectDB()
	if err != nil {
		t.Fatalf("connect PostgreSQL integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close integration database: %v", err)
		}
	})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin integration transaction: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			t.Errorf("rollback integration transaction: %v", err)
		}
	})

	randomSuffix := make([]byte, 12)
	if _, err := rand.Read(randomSuffix); err != nil {
		t.Fatalf("generate unique integration identifiers: %v", err)
	}
	uniqueID := hex.EncodeToString(randomSuffix)
	sku := "go-test-" + uniqueID
	testEmail := "golang-" + uniqueID + "@integration.test"
	testPassword := "integration-test-password"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash integration user password: %v", err)
	}

	var supplierID string
	err = tx.QueryRow(`
		INSERT INTO usuarios (
			nome, nome_loja, pode_comprar, pode_vender, status,
			telegram_chat_id, whatsapp_id, email, telefone, cep, numero,
			latitude, longitude, origem_cadastro, senha_hash
		) VALUES (
			$1, $2, FALSE, TRUE, 'aprovado',
			NULL, NULL, $3, $4, NULL, NULL, NULL, NULL,
			'go_test', $5
		)
		RETURNING id::text
	`, "Go integration "+uniqueID, "Go integration "+uniqueID,
		testEmail, "integration-"+uniqueID, string(passwordHash)).Scan(&supplierID)
	if err != nil {
		t.Fatalf("insert temporary supplier account: %v", err)
	}
	for index := 1; index <= 2; index++ {
		name := fmt.Sprintf("Go integration user %d %s", index, uniqueID)
		_, err := tx.Exec(`
			INSERT INTO usuarios (
				nome, nome_loja, pode_comprar, pode_vender, status,
				telegram_chat_id, whatsapp_id, email, telefone, cep, numero,
				latitude, longitude, origem_cadastro, senha_hash
			) VALUES (
				$1, NULL, TRUE, FALSE, 'aprovado',
				NULL, NULL, $2, $3, NULL, NULL, NULL, NULL,
				'go_test', $4
			)
		`, name, fmt.Sprintf("golang-user-%d-%s@integration.test", index, uniqueID),
			fmt.Sprintf("integration-user-%d-%s", index, uniqueID), string(passwordHash))
		if err != nil {
			t.Fatalf("insert temporary user %d: %v", index, err)
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id":          supplierID,
		"nome":        "Integration test",
		"pode_vender": true,
	})
	rawToken, err := token.SignedString([]byte("integration-test-secret"))
	if err != nil {
		t.Fatalf("sign integration JWT: %v", err)
	}

	handler := New(tx, config.Config{JWTSecret: "integration-test-secret"})
	type paginatedUsersResult struct {
		Sucesso   bool `json:"sucesso"`
		Paginacao struct {
			TotalRegistros int64 `json:"total_registros"`
			TotalPaginas   int64 `json:"total_paginas"`
			PaginaAtual    int64 `json:"pagina_atual"`
			PorPagina      int64 `json:"por_pagina"`
		} `json:"paginacao"`
		Dados []map[string]any `json:"dados"`
	}
	getUsersPage := func(page int) paginatedUsersResult {
		request := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/enitime/users?page=%d&limit=2", page), nil)
		request.Header.Set("Authorization", "Bearer "+rawToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK ||
			response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("list users page %d returned status %d and content type %q: %s",
				page, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
		var result paginatedUsersResult
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode users page %d response: %v", page, err)
		}
		return result
	}

	firstUsersPage := getUsersPage(1)
	secondUsersPage := getUsersPage(2)
	if !firstUsersPage.Sucesso || firstUsersPage.Paginacao.TotalRegistros < 3 ||
		firstUsersPage.Paginacao.TotalPaginas < 2 || firstUsersPage.Paginacao.PaginaAtual != 1 ||
		firstUsersPage.Paginacao.PorPagina != 2 || len(firstUsersPage.Dados) != 2 {
		t.Fatalf("unexpected first users page: %#v", firstUsersPage)
	}
	expectedNames := map[string]bool{
		"Go integration " + uniqueID:        false,
		"Go integration user 1 " + uniqueID: false,
		"Go integration user 2 " + uniqueID: false,
	}
	allUsers := append(firstUsersPage.Dados, secondUsersPage.Dados...)
	for _, user := range allUsers {
		if name, ok := user["nome"].(string); ok {
			if _, expected := expectedNames[name]; expected {
				expectedNames[name] = true
			}
		}
	}
	if !secondUsersPage.Sucesso || secondUsersPage.Paginacao.TotalRegistros != firstUsersPage.Paginacao.TotalRegistros ||
		secondUsersPage.Paginacao.TotalPaginas != firstUsersPage.Paginacao.TotalPaginas ||
		secondUsersPage.Paginacao.PaginaAtual != 2 || secondUsersPage.Paginacao.PorPagina != 2 {
		t.Fatalf("unexpected second users page: %#v", secondUsersPage)
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("paginated users response did not return user %q with its name", name)
		}
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/enitime/login", strings.NewReader(fmt.Sprintf(
		`{"username":%q,"password":%q}`,
		"Go integration "+uniqueID, testPassword,
	)))
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK || loginResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("login returned status %d and content type %q: %s",
			loginResponse.Code, loginResponse.Header().Get("Content-Type"), loginResponse.Body.String())
	}
	var loginResult map[string]any
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &loginResult); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResult["success"] != true || loginResult["token"] == "" {
		t.Fatalf("unexpected login response: %#v", loginResult)
	}

	createBody := fmt.Sprintf(`{
		"sku": %q,
		"part_number": "integration-test",
		"modelos_compativeis": ["integration-test"],
		"tipo_componente": "integration-test",
		"marca_qualidade": "integration-test",
		"preco": 0.01,
		"quantidade": 1
	}`, sku)
	createRequest := httptest.NewRequest(http.MethodPost, "/enitime/produtos/", strings.NewReader(createBody))
	createRequest.Header.Set("Authorization", "Bearer "+rawToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("create product returned %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if createResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("create product returned content type %q", createResponse.Header().Get("Content-Type"))
	}

	var inserted []map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &inserted); err != nil {
		t.Fatalf("decode product insert response: %v", err)
	}
	if len(inserted) != 1 || inserted[0]["sku"] != sku {
		t.Fatalf("unexpected insert response: %#v", inserted)
	}

	bulkRequest := httptest.NewRequest(http.MethodPost, "/enitime/produtos/bulk", strings.NewReader(fmt.Sprintf(`{
		"produtos": [{
			"sku": %q,
			"part_number": "bulk-integration-test",
			"modelos_compativeis": ["bulk-test"],
			"tipo_componente": "bulk-test",
			"marca_qualidade": "bulk-test",
			"preco": 0.02,
			"quantidade": 2
		}]
	}`, sku)))
	bulkRequest.Header.Set("Authorization", "Bearer "+rawToken)
	bulkResponse := httptest.NewRecorder()
	handler.ServeHTTP(bulkResponse, bulkRequest)
	if bulkResponse.Code != http.StatusOK {
		t.Fatalf("bulk upsert returned %d: %s", bulkResponse.Code, bulkResponse.Body.String())
	}
	if bulkResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("bulk upsert returned content type %q", bulkResponse.Header().Get("Content-Type"))
	}
	var bulkResult map[string]any
	if err := json.Unmarshal(bulkResponse.Body.Bytes(), &bulkResult); err != nil {
		t.Fatalf("decode bulk upsert response: %v", err)
	}
	if bulkResult["success"] != true || bulkResult["total_processado"] != float64(1) {
		t.Fatalf("unexpected bulk upsert response: %#v", bulkResult)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/enitime/produtos", nil)
	listRequest.Header.Set("Authorization", "Bearer "+rawToken)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list products returned %d: %s", listResponse.Code, listResponse.Body.String())
	}
	if listResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("list products returned content type %q", listResponse.Header().Get("Content-Type"))
	}

	var products []map[string]any
	if err := json.Unmarshal(listResponse.Body.Bytes(), &products); err != nil {
		t.Fatalf("decode product list response: %v", err)
	}
	found := false
	for _, product := range products {
		if product["sku"] == sku && product["part_number"] == "bulk-integration-test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("inserted product %q was not returned by list query", sku)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback integration transaction: %v", err)
	}
	var productCount, supplierCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM estoque_fornecedor WHERE sku = $1`, sku).Scan(&productCount); err != nil {
		t.Fatalf("verify product transaction rollback: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usuarios WHERE email = $1`, testEmail).Scan(&supplierCount); err != nil {
		t.Fatalf("verify supplier transaction rollback: %v", err)
	}
	if productCount != 0 || supplierCount != 0 {
		t.Fatalf("integration records remained after rollback: products=%d suppliers=%d", productCount, supplierCount)
	}
}
