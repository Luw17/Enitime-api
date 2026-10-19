package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"enitime-api/internal/middlewares"
)

func (api *API) getOrders(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" || !isNumericIdentifier(userID) {
		writeError(w, http.StatusBadRequest, "O parâmetro user_id é obrigatório.")
		return
	}
	if !principal.IsAdmin && !principalOwnsID(principal.ID, deref(principal.TelegramChatID), userID) {
		writeError(w, http.StatusForbidden, "Acesso negado.")
		return
	}

	rows, err := queryRows(r, api.db, `
		SELECT * FROM pedidos_pecas
		WHERE comprador_chat_id = $1
		ORDER BY id DESC
	`, userID)
	if err != nil {
		log.Printf("query purchaser orders: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar pedidos.")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (api *API) getProducts(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}

	rows, err := queryRows(r, api.db, `
		SELECT * FROM estoque_fornecedor
		WHERE fornecedor_id = $1
		ORDER BY criado_em DESC
	`, principal.ID)
	if err != nil {
		log.Printf("query supplier products: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar produtos.")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (api *API) getProduct(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}
	productID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || productID < 1 {
		writeError(w, http.StatusBadRequest, "ID do produto inválido.")
		return
	}

	rows, err := queryRows(r, api.db, `
		SELECT * FROM estoque_fornecedor
		WHERE fornecedor_id = $1 AND id = $2
		ORDER BY criado_em DESC
	`, principal.ID, productID)
	if err != nil {
		log.Printf("query supplier product: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar produto.")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (api *API) createProduct(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/enitime/produtos" && r.URL.Path != "/enitime/produtos/" {
		http.NotFound(w, r)
		return
	}
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sku, err := requiredString(payload, "sku", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	price, err := requiredFloat(payload, "preco")
	if err != nil || price < 0 {
		writeError(w, http.StatusBadRequest, "preco deve ser um número maior ou igual a zero.")
		return
	}

	columns := []string{"fornecedor_id", "sku", "preco"}
	values := []any{principal.ID, sku, price}
	optionalFields := []string{"part_number", "tipo_componente", "marca_qualidade"}
	for _, field := range optionalFields {
		if _, exists := payload[field]; !exists {
			continue
		}
		value, err := optionalString(payload, field, 200)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		columns = append(columns, field)
		if value == nil {
			values = append(values, nil)
		} else {
			values = append(values, *value)
		}
	}
	if _, exists := payload["modelos_compativeis"]; exists {
		models, err := parseCompatibleModels(payload["modelos_compativeis"])
		if err != nil {
			writeError(w, http.StatusBadRequest, "modelos_compativeis deve ser um texto ou uma lista de textos.")
			return
		}
		columns = append(columns, "modelos_compativeis")
		values = append(values, models)
	}
	if _, exists := payload["quantidade"]; exists {
		quantity, err := optionalInt(payload, "quantidade")
		if err != nil || (quantity != nil && *quantity < 0) {
			writeError(w, http.StatusBadRequest, "quantidade deve ser um número inteiro maior ou igual a zero.")
			return
		}
		columns = append(columns, "quantidade")
		if quantity == nil {
			values = append(values, nil)
		} else {
			values = append(values, *quantity)
		}
	}

	query := "INSERT INTO estoque_fornecedor (" + strings.Join(columns, ", ") + ") VALUES ("
	placeholders := make([]string, len(values))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query += strings.Join(placeholders, ", ") + ") RETURNING *"

	rows, err := queryRows(r, api.db, query, values...)
	if err != nil {
		log.Printf("insert supplier product: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao inserir produto.")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (api *API) bulkProducts(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}

	body, err := readRequestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var rawProducts []json.RawMessage
	if err := json.Unmarshal(body, &rawProducts); err != nil {
		var payload map[string]json.RawMessage
		if objectErr := json.Unmarshal(body, &payload); objectErr != nil || payload == nil {
			writeError(w, http.StatusBadRequest, "O corpo deve conter uma lista de produtos.")
			return
		}
		rawProducts, err = rawProductList(payload["produtos"])
		if err != nil {
			writeError(w, http.StatusBadRequest, "O corpo deve conter uma lista de produtos.")
			return
		}
	}
	if len(rawProducts) == 0 || len(rawProducts) > 500 {
		writeError(w, http.StatusBadRequest, "Lista de produtos vazia ou acima do limite.")
		return
	}

	type productInput struct {
		FornecedorID       string   `json:"fornecedor_id"`
		SKU                string   `json:"sku"`
		PartNumber         *string  `json:"part_number"`
		ModelosCompativeis []string `json:"modelos_compativeis"`
		TipoComponente     *string  `json:"tipo_componente"`
		MarcaQualidade     *string  `json:"marca_qualidade"`
		Preco              float64  `json:"preco"`
		Quantidade         int64    `json:"quantidade"`
	}
	products := make([]productInput, 0, len(rawProducts))
	for index, raw := range rawProducts {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Produto %d inválido.", index+1))
			return
		}
		sku, err := requiredString(payload, "sku", 100)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("SKU obrigatório no produto %d.", index+1))
			return
		}
		price := float64(0)
		if _, exists := payload["preco"]; exists {
			price, err = requiredFloat(payload, "preco")
			if err != nil || price < 0 {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Preço inválido no produto %d.", index+1))
				return
			}
		}
		quantity := int64(0)
		if _, exists := payload["quantidade"]; exists {
			parsed, err := optionalInt(payload, "quantidade")
			if err != nil || (parsed != nil && *parsed < 0) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Quantidade inválida no produto %d.", index+1))
				return
			}
			if parsed != nil {
				quantity = *parsed
			}
		}
		partNumber, err := optionalString(payload, "part_number", 100)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("part_number inválido no produto %d.", index+1))
			return
		}
		if partNumber == nil {
			partNumber, err = optionalString(payload, "codigo_peca", 100)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("codigo_peca inválido no produto %d.", index+1))
				return
			}
		}
		compatibleModels, err := parseCompatibleModels(payload["modelos_compativeis"])
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("modelos_compativeis inválido no produto %d.", index+1))
			return
		}
		componentType, err := optionalString(payload, "tipo_componente", 100)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("tipo_componente inválido no produto %d.", index+1))
			return
		}
		quality, err := optionalString(payload, "marca_qualidade", 100)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("marca_qualidade inválida no produto %d.", index+1))
			return
		}
		products = append(products, productInput{
			FornecedorID: principal.ID, SKU: sku, PartNumber: partNumber,
			ModelosCompativeis: compatibleModels, TipoComponente: componentType,
			MarcaQualidade: quality, Preco: price, Quantidade: quantity,
		})
	}
	productsJSON, err := json.Marshal(products)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao processar produtos.")
		return
	}

	rows, err := queryRows(r, api.db, `
		INSERT INTO estoque_fornecedor (
			fornecedor_id, sku, part_number, modelos_compativeis,
			tipo_componente, marca_qualidade, preco, quantidade
		)
		SELECT
			item.fornecedor_id, item.sku, item.part_number,
			ARRAY(
				SELECT json_array_elements_text(
					CASE WHEN json_typeof(item.modelos_compativeis) = 'array'
						THEN item.modelos_compativeis ELSE '[]'::json END
				)
			),
			item.tipo_componente, item.marca_qualidade, item.preco, item.quantidade
		FROM json_to_recordset($1::json) AS item(
			fornecedor_id VARCHAR(100), sku VARCHAR(100), part_number VARCHAR(100),
			modelos_compativeis JSON, tipo_componente VARCHAR(100),
			marca_qualidade VARCHAR(100), preco NUMERIC(10,2), quantidade INT
		)
		ON CONFLICT (fornecedor_id, sku) DO UPDATE SET
			part_number = EXCLUDED.part_number,
			modelos_compativeis = EXCLUDED.modelos_compativeis,
			tipo_componente = EXCLUDED.tipo_componente,
			marca_qualidade = EXCLUDED.marca_qualidade,
			preco = EXCLUDED.preco,
			quantidade = EXCLUDED.quantidade,
			criado_em = CURRENT_TIMESTAMP
		RETURNING id, sku
	`, string(productsJSON))
	if err != nil {
		log.Printf("bulk upsert supplier products: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar estoque.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"message":          "Estoque atualizado com sucesso.",
		"total_processado": len(rows),
	})
}

func (api *API) updateProduct(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/enitime/produtos" && r.URL.Path != "/enitime/produtos/" {
		http.NotFound(w, r)
		return
	}
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawID, exists := payload["id"]
	if !exists {
		rawID = json.RawMessage(strconv.Quote(r.URL.Query().Get("id")))
	}
	productID, err := rawInt(rawID)
	if err != nil || productID < 1 {
		writeError(w, http.StatusBadRequest, "ID do produto inválido ou não fornecido.")
		return
	}

	sets := make([]string, 0, 8)
	values := make([]any, 0, 9)
	add := func(column string, value any) {
		values = append(values, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(values)))
	}
	for _, field := range []string{"sku", "part_number", "tipo_componente", "marca_qualidade"} {
		if _, exists := payload[field]; !exists {
			continue
		}
		value, err := optionalString(payload, field, 200)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if field == "sku" && value == nil {
			writeError(w, http.StatusBadRequest, "sku não pode ser vazio.")
			return
		}
		add(field, ptrValue(value))
	}
	if raw, exists := payload["modelos_compativeis"]; exists && strings.TrimSpace(string(raw)) != "null" {
		compatibleModels, err := parseCompatibleModels(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "modelos_compativeis inválido.")
			return
		}
		modelsJSON, _ := json.Marshal(compatibleModels)
		values = append(values, string(modelsJSON))
		sets = append(sets, fmt.Sprintf("modelos_compativeis = ARRAY(SELECT json_array_elements_text($%d::json))", len(values)))
	}
	if _, exists := payload["preco"]; exists {
		price, err := optionalFloat(payload, "preco")
		if err != nil || (price != nil && *price < 0) {
			writeError(w, http.StatusBadRequest, "preco inválido.")
			return
		}
		if price != nil {
			add("preco", *price)
		}
	}
	if _, exists := payload["quantidade"]; exists {
		quantity, err := optionalInt(payload, "quantidade")
		if err != nil || (quantity != nil && *quantity < 0) {
			writeError(w, http.StatusBadRequest, "quantidade inválida.")
			return
		}
		if quantity != nil {
			add("quantidade", *quantity)
		}
	}
	if len(sets) == 0 {
		writeError(w, http.StatusBadRequest, "Nenhum campo válido fornecido para atualização.")
		return
	}

	values = append(values, productID)
	idPlaceholder := len(values)
	query := fmt.Sprintf("UPDATE estoque_fornecedor SET %s, criado_em = CURRENT_TIMESTAMP WHERE id = $%d", strings.Join(sets, ", "), idPlaceholder)
	if !principal.IsAdmin {
		values = append(values, principal.ID)
		query += fmt.Sprintf(" AND fornecedor_id = $%d", len(values))
	}
	query += " RETURNING *"

	rows, err := queryRows(r, api.db, query, values...)
	if err != nil {
		log.Printf("update supplier product: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar produto.")
		return
	}
	var data any
	if len(rows) > 0 {
		data = rows[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Produto atualizado com sucesso.",
		"data":    data,
	})
}

func parseCompatibleModels(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		result := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" && len([]rune(value)) <= 200 {
				result = append(result, value)
			}
		}
		return result, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	if len([]rune(value)) > 200 {
		return nil, fmt.Errorf("model name is too long")
	}
	return []string{value}, nil
}

func rawInt(raw json.RawMessage) (int64, error) {
	var payload map[string]json.RawMessage
	encoded, _ := json.Marshal(map[string]json.RawMessage{"value": raw})
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return 0, err
	}
	value, err := requiredInt(payload, "value")
	if err != nil {
		return 0, err
	}
	return value, nil
}

func deref[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}
