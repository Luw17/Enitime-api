package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"enitime-api/internal/middlewares"
	"enitime-api/internal/models"
)

func (api *API) searchProducts(w http.ResponseWriter, r *http.Request) {
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	chatID, err := requestChatID(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador do Telegram (telegram_chat_id ou user_id) é obrigatório.")
		return
	}

	filters := models.SearchFilters{}
	if filters.TipoComponente, err = optionalString(payload, "tipo_componente", 100); err != nil {
		writeError(w, http.StatusBadRequest, "tipo_componente inválido.")
		return
	}
	if filters.ModeloCompativel, err = optionalString(payload, "modelo_compativel", 200); err != nil {
		writeError(w, http.StatusBadRequest, "modelo_compativel inválido.")
		return
	}
	if filters.CodigoPeca, err = optionalString(payload, "codigo_peca", 100); err != nil {
		writeError(w, http.StatusBadRequest, "codigo_peca inválido.")
		return
	}
	if filters.TipoComponente == nil && filters.ModeloCompativel == nil && filters.CodigoPeca == nil {
		writeError(w, http.StatusBadRequest, "Informe ao menos um parâmetro da peça (tipo_componente, modelo_compativel ou codigo_peca).")
		return
	}

	radius := 50.0
	if raw, exists := payload["raio_km"]; exists && string(raw) != "null" {
		radius, err = rawFloat(raw)
		if err != nil || math.IsNaN(radius) || math.IsInf(radius, 0) {
			writeError(w, http.StatusBadRequest, "raio_km deve ser numérico.")
			return
		}
		if radius <= 0 {
			radius = 50
		}
		if radius > 20000 {
			writeError(w, http.StatusBadRequest, "raio_km excede o limite permitido.")
			return
		}
	}

	rows, err := queryRows(r, api.db, `
		WITH comprador AS (
			SELECT latitude, longitude
			FROM usuarios
			WHERE telegram_chat_id = $1 AND status = 'aprovado'
			LIMIT 1
		)
		SELECT
			ef.id AS produto_id,
			ef.sku,
			ef.part_number,
			ef.modelos_compativeis,
			ef.tipo_componente,
			ef.marca_qualidade,
			ef.preco,
			ef.quantidade AS quantidade_estoque,
			u.id AS loja_id,
			u.nome_loja,
			u.nome AS vendedor_nome,
			u.telegram_chat_id AS loja_telegram_chat_id,
			u.whatsapp_id AS loja_whatsapp_id,
			u.telefone AS loja_telefone,
			u.latitude AS loja_latitude,
			u.longitude AS loja_longitude,
			CASE
				WHEN u.latitude IS NOT NULL AND u.longitude IS NOT NULL AND c.latitude IS NOT NULL AND c.longitude IS NOT NULL THEN
					ROUND((earth_distance(
						ll_to_earth(u.latitude::double precision, u.longitude::double precision),
						ll_to_earth(c.latitude::double precision, c.longitude::double precision)
					) / 1000)::numeric, 2)
				ELSE 0
			END AS distancia_km
		FROM comprador c
		CROSS JOIN usuarios u
		JOIN estoque_fornecedor ef ON (
			ef.fornecedor_id = u.id::text
			OR (u.telegram_chat_id IS NOT NULL AND u.telegram_chat_id != '' AND ef.fornecedor_id = u.telegram_chat_id)
		)
		WHERE u.status = 'aprovado'
			AND u.pode_vender = TRUE
			AND ef.quantidade > 0
			AND (
				c.latitude IS NULL OR u.latitude IS NULL OR
				earth_distance(
					ll_to_earth(u.latitude::double precision, u.longitude::double precision),
					ll_to_earth(c.latitude::double precision, c.longitude::double precision)
				) <= $2 * 1000
			)
			AND ($3::text IS NULL OR ef.tipo_componente ILIKE '%' || $3 || '%')
			AND (
				$4::text IS NULL OR EXISTS (
					SELECT 1 FROM unnest(ef.modelos_compativeis) AS modelo
					WHERE modelo ILIKE '%' || $4 || '%'
				)
			)
			AND ($5::text IS NULL OR ef.part_number ILIKE '%' || $5 || '%')
		ORDER BY distancia_km ASC, ef.preco ASC
	`, chatID, radius, ptrValue(filters.TipoComponente), ptrValue(filters.ModeloCompativel), ptrValue(filters.CodigoPeca))
	if err != nil {
		log.Printf("search nearby products: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar produtos.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sucesso":           true,
		"comprador_chat_id": chatID,
		"filtros_busca": map[string]any{
			"tipo_componente":   filters.TipoComponente,
			"modelo_compativel": filters.ModeloCompativel,
			"codigo_peca":       filters.CodigoPeca,
		},
		"raio_buscado_km":            radius,
		"total_produtos_encontrados": len(rows),
		"produtos":                   rows,
	})
}

func (api *API) checkout(w http.ResponseWriter, r *http.Request) {
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	chatID, err := requestChatID(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "O campo telegram_chat_id é obrigatório.")
		return
	}
	rawItems, exists := payload["itens"]
	if !exists {
		writeError(w, http.StatusBadRequest, "O carrinho deve conter ao menos um item válido.")
		return
	}
	var rawItemList []json.RawMessage
	if err := json.Unmarshal(rawItems, &rawItemList); err != nil || len(rawItemList) == 0 || len(rawItemList) > 100 {
		writeError(w, http.StatusBadRequest, "O carrinho deve conter ao menos um item válido.")
		return
	}

	items := make([]models.CheckoutItem, 0, len(rawItemList))
	for i, rawItem := range rawItemList {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil || item == nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Item %d inválido.", i+1))
			return
		}
		productID, err := requiredInt(item, "produto_id")
		if err != nil || productID < 1 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("ID do produto inválido no item %d.", i+1))
			return
		}
		quantity, err := requiredInt(item, "quantidade")
		if err != nil || quantity < 1 || quantity > 1000000 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Quantidade inválida no item %d.", i+1))
			return
		}
		observation, err := optionalString(item, "observacao", 1000)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Observação inválida no item %d.", i+1))
			return
		}
		items = append(items, models.CheckoutItem{
			ProdutoID: productID, Quantidade: quantity, Observacao: observation,
		})
	}
	generalObservation, err := optionalString(payload, "observacao_geral", 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Observação geral inválida.")
		return
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao processar checkout.")
		return
	}

	rows, err := queryRows(r, api.db, `
		WITH comprador AS (
			SELECT latitude, longitude
			FROM usuarios
			WHERE (telegram_chat_id = $1 OR id::text = $1)
				AND status = 'aprovado'
			LIMIT 1
		),
		input_itens AS (
			SELECT
				(elem->>'produto_id')::BIGINT AS produto_id,
				(elem->>'quantidade')::INT AS quantidade,
				elem->>'observacao' AS observacao
			FROM jsonb_array_elements($2::jsonb) AS elem
		)
		INSERT INTO pedidos_pecas (
			comprador_chat_id, fornecedor_id, estoque_id, quantidade, observacao,
			distancia_km, descricao_peca, status
		)
		SELECT
			$1 AS comprador_chat_id,
			ef.fornecedor_id,
			ef.id AS estoque_id,
			i.quantidade,
			COALESCE(i.observacao, $3::text) AS observacao,
			CASE
				WHEN u.latitude IS NOT NULL AND u.longitude IS NOT NULL AND c.latitude IS NOT NULL AND c.longitude IS NOT NULL THEN
					ROUND((earth_distance(
						ll_to_earth(u.latitude::double precision, u.longitude::double precision),
						ll_to_earth(c.latitude::double precision, c.longitude::double precision)
					) / 1000)::numeric, 2)
				ELSE 0
			END AS distancia_km,
			CONCAT(ef.tipo_componente, ' - ', array_to_string(ef.modelos_compativeis, ', ')) AS descricao_peca,
			'pendente' AS status
		FROM input_itens i
		JOIN estoque_fornecedor ef ON ef.id = i.produto_id
		JOIN usuarios u ON (
			u.id::text = ef.fornecedor_id
			OR (u.telegram_chat_id IS NOT NULL AND u.telegram_chat_id != '' AND u.telegram_chat_id = ef.fornecedor_id)
		)
		LEFT JOIN comprador c ON TRUE
		WHERE ef.quantidade >= i.quantidade
		RETURNING id, comprador_chat_id, fornecedor_id, estoque_id, quantidade,
			observacao, distancia_km, descricao_peca, status, criado_em
	`, chatID, string(itemsJSON), ptrValue(generalObservation))
	if err != nil {
		log.Printf("insert checkout orders: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao processar checkout.")
		return
	}
	unprocessed := len(items) - len(rows)
	writeJSON(w, http.StatusOK, map[string]any{
		"sucesso":                              true,
		"mensagem":                             "Processamento de checkout concluído.",
		"total_itens_solicitados":              len(items),
		"total_pedidos_gerados":                len(rows),
		"itens_nao_processados_ou_sem_estoque": unprocessed,
		"pedidos":                              rows,
	})
}

func (api *API) getSupplierOrders(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token de autorização inválido.")
		return
	}
	query := r.URL.Query()
	supplierID := strings.TrimSpace(query.Get("fornecedor_id"))
	if supplierID == "" {
		supplierID = strings.TrimSpace(query.Get("telegram_chat_id"))
	}
	if supplierID == "" {
		supplierID = principal.ID
	}
	if !principal.IsAdmin && !principalOwnsID(principal.ID, deref(principal.TelegramChatID), supplierID) {
		writeError(w, http.StatusForbidden, "Acesso negado.")
		return
	}
	statusFilter := any(nil)
	if rawStatus := strings.TrimSpace(query.Get("status")); rawStatus != "" {
		if len(rawStatus) > 40 {
			writeError(w, http.StatusBadRequest, "Filtro de status inválido.")
			return
		}
		statusFilter = rawStatus
	}
	page, limit := pagination(query.Get("page"), query.Get("limit"))
	offset := (page - 1) * limit

	rows, err := queryRows(r, api.db, `
		WITH dados_pedidos AS (
			SELECT
				p.id, p.comprador_chat_id, p.fornecedor_id, p.estoque_id,
				p.descricao_peca, p.quantidade, p.observacao, p.distancia_km,
				p.status, p.criado_em, u.nome AS comprador_nome,
				u.telefone AS comprador_telefone
			FROM pedidos_pecas p
			LEFT JOIN usuarios u ON u.telegram_chat_id = p.comprador_chat_id
			WHERE p.fornecedor_id = $1 AND ($2::text IS NULL OR p.status = $2)
		),
		total AS (
			SELECT COUNT(*) AS total_count FROM dados_pedidos
		)
		SELECT
			(SELECT total_count FROM total) AS total_registros,
			COALESCE(json_agg(dp ORDER BY dp.criado_em DESC), '[]'::json) AS registros
		FROM (
			SELECT * FROM dados_pedidos
			ORDER BY criado_em DESC
			LIMIT $3 OFFSET $4
		) dp
	`, supplierID, statusFilter, limit, offset)
	if err != nil {
		log.Printf("query supplier orders: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar pedidos.")
		return
	}
	var total int64
	var records any = []any{}
	if len(rows) > 0 {
		switch value := rows[0]["total_registros"].(type) {
		case int64:
			total = value
		case float64:
			total = int64(value)
		case []byte:
			total, _ = strconv.ParseInt(string(value), 10, 64)
		case string:
			total, _ = strconv.ParseInt(value, 10, 64)
		}
		if rows[0]["registros"] != nil {
			records = rows[0]["registros"]
		}
	}
	pages := int64(math.Ceil(float64(total) / float64(limit)))
	if pages < 1 {
		pages = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sucesso":       true,
		"fornecedor_id": supplierID,
		"paginacao": map[string]any{
			"pagina_atual": page, "itens_por_pagina": limit,
			"total_registros": total, "total_paginas": pages,
		},
		"pedidos": records,
	})
}

func requestChatID(payload map[string]json.RawMessage) (string, error) {
	for _, key := range []string{"telegram_chat_id"} {
		if value, ok := payload[key]; ok {
			if text, err := rawIdentifier(value); err == nil {
				return text, nil
			}
		}
	}
	if raw, ok := payload["initDataUnsafe"]; ok {
		var unsafe struct {
			Chat *struct {
				ID json.RawMessage `json:"id"`
			} `json:"chat"`
		}
		if json.Unmarshal(raw, &unsafe) == nil && unsafe.Chat != nil && len(unsafe.Chat.ID) > 0 {
			if id, err := rawIdentifier(unsafe.Chat.ID); err == nil {
				return id, nil
			}
		}
	}
	if value, ok := payload["user_id"]; ok {
		if text, err := rawIdentifier(value); err == nil {
			return text, nil
		}
	}
	return "", fmt.Errorf("Telegram chat ID is missing")
}

func rawIdentifier(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if isNumericIdentifier(text) && len(text) <= 100 {
			return text, nil
		}
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		if isNumericIdentifier(number.String()) {
			return number.String(), nil
		}
	}
	return "", fmt.Errorf("invalid identifier")
}

func isNumericIdentifier(value string) bool {
	number, err := strconv.ParseInt(value, 10, 64)
	return err == nil && number != 0
}

func pagination(rawPage, rawLimit string) (int64, int64) {
	page, err := strconv.ParseInt(rawPage, 10, 64)
	if err != nil || page < 1 || page > math.MaxInt32 {
		page = 1
	}
	limit, err := strconv.ParseInt(rawLimit, 10, 64)
	if err != nil || limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
