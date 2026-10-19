package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"enitime-api/internal/middlewares"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var cepPattern = regexp.MustCompile(`^\d{8}$`)

func (api *API) createUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := middlewares.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Token JWT inválido ou malformatado.")
		return
	}
	if !principal.IsAdmin {
		writeError(w, http.StatusForbidden, "Acesso negado.")
		return
	}
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := requiredString(payload, "nome", 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, `O campo "nome" é obrigatório.`)
		return
	}
	password, err := requiredString(payload, "senha", 72)
	if err != nil || len(password) < 4 {
		writeError(w, http.StatusBadRequest, "A senha é obrigatória e deve ter pelo menos 4 caracteres.")
		return
	}
	if len([]byte(password)) > 72 {
		writeError(w, http.StatusBadRequest, "A senha deve ter no máximo 72 bytes.")
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		log.Printf("hash new user password: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao criar usuário.")
		return
	}

	storeName, err := optionalString(payload, "nome_loja", 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	buy, err := optionalBool(payload, "pode_comprar")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sell, err := optionalBool(payload, "pode_vender")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, err := optionalString(payload, "status", 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	statusValue := "aprovado"
	if status != nil {
		statusValue = *status
	}
	chatID, err := optionalString(payload, "telegram_chat_id", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	whatsappID, err := optionalString(payload, "whatsapp_id", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	email, err := optionalString(payload, "email", 320)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	phone, err := optionalString(payload, "telefone", 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cep, err := optionalString(payload, "cep", 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	number, err := optionalString(payload, "numero", 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	latitude, err := optionalFloat(payload, "latitude")
	if err != nil || (latitude != nil && (*latitude < -90 || *latitude > 90)) {
		writeError(w, http.StatusBadRequest, "latitude inválida.")
		return
	}
	longitude, err := optionalFloat(payload, "longitude")
	if err != nil || (longitude != nil && (*longitude < -180 || *longitude > 180)) {
		writeError(w, http.StatusBadRequest, "longitude inválida.")
		return
	}
	origin, err := optionalString(payload, "origem_cadastro", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	originValue := "painel"
	if origin != nil {
		originValue = *origin
	}
	if cep != nil {
		coordinates, err := api.lookupCEP(r.Context(), *cep)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CEP inválido ou não encontrado.")
			return
		}
		latitude = coordinates.Latitude
		longitude = coordinates.Longitude
	}

	rows, err := queryRows(r, api.db, `
		INSERT INTO usuarios (
			nome, nome_loja, pode_comprar, pode_vender, status,
			telegram_chat_id, whatsapp_id, email, telefone, cep, numero,
			latitude, longitude, origem_cadastro, senha_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, nome, nome_loja, pode_comprar, pode_vender, status,
			telegram_chat_id, whatsapp_id, email, telefone, cep, numero,
			latitude, longitude, origem_cadastro, criado_em
	`, name, ptrValue(storeName), boolValue(buy), boolValue(sell), statusValue,
		ptrValue(chatID), ptrValue(whatsappID), ptrValue(email), ptrValue(phone),
		cepDigits(cep), ptrValue(number), ptrValue(latitude), ptrValue(longitude),
		originValue, string(passwordHash))
	if err != nil {
		log.Printf("insert user: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao criar usuário.")
		return
	}
	var user any
	if len(rows) > 0 {
		user = rows[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Usuário criado com sucesso.",
		"user":    user,
	})
}

func (api *API) updateUser(w http.ResponseWriter, r *http.Request) {
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
	id, err := requiredInt(payload, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "ID do usuário inválido ou não fornecido no corpo da requisição.")
		return
	}
	targetID := strconv.FormatInt(id, 10)
	if !principal.IsAdmin && principal.ID != targetID {
		writeError(w, http.StatusForbidden, "Acesso negado: Sem permissão para atualizar este usuário.")
		return
	}

	sets := make([]string, 0, 16)
	values := make([]any, 0, 16)
	add := func(column string, value any) {
		values = append(values, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(values)))
	}
	for _, field := range []string{
		"nome", "nome_loja", "email", "telefone", "telegram_chat_id",
		"whatsapp_id", "cep", "numero", "origem_cadastro",
	} {
		if _, exists := payload[field]; !exists {
			continue
		}
		value, err := optionalString(payload, field, 320)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		add(field, ptrValue(value))
	}
	if _, exists := payload["status"]; exists {
		if !principal.IsAdmin {
			writeError(w, http.StatusForbidden, "Apenas administradores podem alterar o status.")
			return
		}
		value, err := requiredString(payload, "status", 40)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		add("status", value)
	}
	for _, field := range []string{"pode_comprar", "pode_vender"} {
		if _, exists := payload[field]; !exists {
			continue
		}
		if !principal.IsAdmin {
			writeError(w, http.StatusForbidden, "Apenas administradores podem alterar permissões.")
			return
		}
		value, err := optionalBool(payload, field)
		if err != nil || value == nil {
			writeError(w, http.StatusBadRequest, field+" deve ser booleano.")
			return
		}
		add(field, *value)
	}
	latitudeRaw, hasLatitude := payload["latitude"]
	longitudeRaw, hasLongitude := payload["longitude"]
	if hasLatitude {
		value, err := optionalFloat(payload, "latitude")
		if err != nil || (value != nil && (*value < -90 || *value > 90)) {
			writeError(w, http.StatusBadRequest, "latitude inválida.")
			return
		}
		if strings.TrimSpace(string(latitudeRaw)) != "null" {
			add("latitude", ptrValue(value))
		}
	}
	if hasLongitude {
		value, err := optionalFloat(payload, "longitude")
		if err != nil || (value != nil && (*value < -180 || *value > 180)) {
			writeError(w, http.StatusBadRequest, "longitude inválida.")
			return
		}
		if strings.TrimSpace(string(longitudeRaw)) != "null" {
			add("longitude", ptrValue(value))
		}
	}
	if len(sets) == 0 {
		writeError(w, http.StatusBadRequest, "Nenhum campo válido para atualização foi fornecido.")
		return
	}

	values = append(values, id)
	query := fmt.Sprintf(`
		UPDATE usuarios SET %s WHERE id = $%d
		RETURNING id, nome, nome_loja, pode_comprar, pode_vender, status,
			telegram_chat_id, whatsapp_id, email, telefone, cep, numero,
			latitude, longitude, origem_cadastro, criado_em
	`, strings.Join(sets, ", "), len(values))
	rows, err := queryRows(r, api.db, query, values...)
	if err != nil {
		log.Printf("update user: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao atualizar usuário.")
		return
	}
	var data any
	if len(rows) > 0 {
		data = rows[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Usuário atualizado com sucesso.",
		"data":    data,
	})
}

func (api *API) getUsers(w http.ResponseWriter, r *http.Request) {
	page, limit := pagination(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))
	offset := (page - 1) * limit

	rows, err := queryRows(r, api.db, `
		WITH total AS (
			SELECT COUNT(*)::INT AS total_count FROM usuarios
		),
		dados AS (
			SELECT
				id,
				nome,
				telegram_chat_id,
				whatsapp_id,
				criado_em,
				pode_comprar,
				pode_vender,
				status,
				nome_loja
			FROM usuarios
			ORDER BY id DESC
			LIMIT $1 OFFSET $2
		)
		SELECT
			t.total_count,
			COALESCE(json_agg(d) FILTER (WHERE d.id IS NOT NULL), '[]'::json) AS usuarios
		FROM total t
		LEFT JOIN dados d ON true
		GROUP BY t.total_count
	`, limit, offset)
	if err != nil {
		log.Printf("query paginated users: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao buscar usuários.")
		return
	}

	var total int64
	users := make([]any, 0)
	if len(rows) > 0 {
		switch value := rows[0]["total_count"].(type) {
		case int64:
			total = value
		case int32:
			total = int64(value)
		case float64:
			total = int64(value)
		case string:
			total, _ = strconv.ParseInt(value, 10, 64)
		case []byte:
			total, _ = strconv.ParseInt(string(value), 10, 64)
		}
		if result, ok := rows[0]["usuarios"].([]any); ok {
			users = result
		}
	}

	totalPages := int64(math.Ceil(float64(total) / float64(limit)))
	if totalPages < 1 {
		totalPages = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sucesso": true,
		"paginacao": map[string]any{
			"total_registros": total,
			"total_paginas":   totalPages,
			"pagina_atual":    page,
			"por_pagina":      limit,
		},
		"dados": users,
	})
}

func (api *API) registerTelegramUser(w http.ResponseWriter, r *http.Request) {
	if api.config.TelegramBotToken == "" || api.config.TelegramAdminChatID == "" {
		writeError(w, http.StatusServiceUnavailable, "Cadastro Telegram não configurado.")
		return
	}
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "autenticação invalida")
		return
	}
	initData, err := requiredString(payload, "initData", 16384)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "autenticação invalida")
		return
	}
	query, err := url.ParseQuery(initData)
	if err != nil || !verifyTelegramInitData(query, api.config.TelegramBotToken) {
		writeError(w, http.StatusUnauthorized, "autenticação invalida")
		return
	}
	userValue := query.Get("user")
	var telegramUser struct {
		ID        json.Number `json:"id"`
		FirstName string      `json:"first_name"`
		Username  string      `json:"username"`
	}
	userDecoder := json.NewDecoder(strings.NewReader(userValue))
	userDecoder.UseNumber()
	if userValue == "" || userDecoder.Decode(&telegramUser) != nil || telegramUser.ID.String() == "" {
		writeError(w, http.StatusBadRequest, "Dados do usuário Telegram inválidos.")
		return
	}
	telegramID, err := strconv.ParseInt(telegramUser.ID.String(), 10, 64)
	if err != nil || telegramID < 1 {
		writeError(w, http.StatusBadRequest, "Identificador Telegram inválido.")
		return
	}
	name := strings.TrimSpace(telegramUser.FirstName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Nome do usuário Telegram obrigatório.")
		return
	}

	accountType, err := requiredString(payload, "type", 20)
	if err != nil || (accountType != "comprador" && accountType != "fornecedor" && accountType != "ambos") {
		writeError(w, http.StatusBadRequest, "Tipo de cadastro inválido.")
		return
	}
	storeName, err := optionalString(payload, "storeName", 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	email, err := optionalString(payload, "email", 320)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	phone, err := optionalString(payload, "phone", 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cep, err := optionalString(payload, "cep", 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	number, err := optionalString(payload, "numero", 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	password, err := optionalString(payload, "password", 72)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var passwordHash any
	if password != nil {
		if len(*password) < 4 {
			writeError(w, http.StatusBadRequest, "A senha deve ter pelo menos 4 caracteres.")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*password), 10)
		if err != nil {
			log.Printf("hash Telegram user password: %v", err)
			writeError(w, http.StatusInternalServerError, "Erro ao processar cadastro.")
			return
		}
		passwordHash = string(hash)
	}
	var latitude, longitude any
	if cep != nil {
		coordinates, err := api.lookupCEP(r.Context(), *cep)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CEP inválido ou não encontrado.")
			return
		}
		latitude = ptrValue(coordinates.Latitude)
		longitude = ptrValue(coordinates.Longitude)
	}

	canBuy := accountType == "comprador" || accountType == "ambos"
	canSell := accountType == "fornecedor" || accountType == "ambos"
	err = api.db.QueryRowContext(r.Context(), `
		INSERT INTO usuarios (
			telegram_chat_id, nome, nome_loja, pode_comprar, pode_vender, email,
			telefone, cep, numero, latitude, longitude, senha_hash, status, origem_cadastro
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'pendente', 'telegram_webapp')
		ON CONFLICT (telegram_chat_id) WHERE telegram_chat_id IS NOT NULL DO UPDATE SET
			nome = EXCLUDED.nome,
			nome_loja = EXCLUDED.nome_loja,
			pode_comprar = EXCLUDED.pode_comprar,
			pode_vender = EXCLUDED.pode_vender,
			email = EXCLUDED.email,
			telefone = EXCLUDED.telefone,
			cep = EXCLUDED.cep,
			numero = EXCLUDED.numero,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			senha_hash = EXCLUDED.senha_hash,
			status = 'pendente'
		RETURNING id, pode_comprar, pode_vender
	`, strconv.FormatInt(telegramID, 10), name, ptrValue(storeName), canBuy, canSell,
		ptrValue(email), ptrValue(phone), cepDigits(cep), ptrValue(number), latitude,
		longitude, passwordHash).Scan(new(int64), new(bool), new(bool))
	if err != nil {
		log.Printf("upsert Telegram user: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao processar cadastro.")
		return
	}
	adminMessage := fmt.Sprintf(
		"🚨 <b>Novo Solicitante de Cadastro</b>\n\n"+
			"<b>Loja:</b> %s\n<b>Tipo:</b> %s\n<b>E-mail:</b> %s\n"+
			"<b>Telefone:</b> %s\n<b>CEP:</b> %s, Nº %s\n<b>Telegram ID:</b> %s",
		html.EscapeString(deref(storeName)),
		html.EscapeString(accountType),
		html.EscapeString(deref(email)),
		html.EscapeString(deref(phone)),
		html.EscapeString(deref(cep)),
		html.EscapeString(deref(number)),
		strconv.FormatInt(telegramID, 10),
	)
	keyboard := map[string]any{
		"inline_keyboard": [][]map[string]string{{
			{"text": "Aprovar", "callback_data": "apr_" + strconv.FormatInt(telegramID, 10)},
			{"text": "Recusar", "callback_data": "recu_" + strconv.FormatInt(telegramID, 10)},
		}},
	}
	if err := api.sendTelegramMessage(r.Context(), api.config.TelegramAdminChatID, adminMessage, "HTML", keyboard); err != nil {
		log.Printf("send Telegram registration approval: %v", err)
		writeError(w, http.StatusBadGateway, "Erro ao enviar notificações do cadastro.")
		return
	}
	if err := api.sendTelegramMessage(r.Context(), strconv.FormatInt(telegramID, 10), "pedido de registro enviado com sucesso!", "", nil); err != nil {
		log.Printf("send Telegram registration confirmation: %v", err)
		writeError(w, http.StatusBadGateway, "Erro ao enviar notificações do cadastro.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Cadastro enviado com sucesso. Aguarde a aprovação do administrador.",
	})
}

func (api *API) sendTelegramMessage(ctx context.Context, chatID, text, parseMode string, keyboard map[string]any) error {
	payload := map[string]any{"chat_id": chatID, "text": text}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Telegram message")
	}
	endpoint := "https://api.telegram.org/bot" + api.config.TelegramBotToken + "/sendMessage"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram message request")
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("Telegram message request failed")
	}
	defer response.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("invalid Telegram message response")
	}
	if response.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("Telegram rejected message with status %d", response.StatusCode)
	}
	return nil
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	if api.config.JWTSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "Autenticação não configurada.")
		return
	}
	payload, err := decodeObject(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	username, usernameErr := requiredString(payload, "username", 320)
	password, passwordErr := requiredString(payload, "password", 72)
	if usernameErr != nil || passwordErr != nil {
		writeError(w, http.StatusUnauthorized, "Usuário e senha são obrigatórios.")
		return
	}

	var (
		id             int64
		name           string
		storeName      sql.NullString
		canBuy         bool
		canSell        bool
		status         string
		telegramChatID sql.NullString
		passwordHash   sql.NullString
	)
	err = api.db.QueryRowContext(r.Context(), `
		SELECT id, nome, nome_loja, pode_comprar, pode_vender, status,
			telegram_chat_id, senha_hash
		FROM usuarios
		WHERE (LOWER(nome) = LOWER($1) OR (email IS NOT NULL AND LOWER(email) = LOWER($1)))
			AND status = 'aprovado'
		LIMIT 1
	`, username).Scan(&id, &name, &storeName, &canBuy, &canSell, &status, &telegramChatID, &passwordHash)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("query user login: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao autenticar usuário.")
		return
	}
	normalizedHash := strings.TrimSpace(strings.ReplaceAll(passwordHash.String, "\n", ""))
	if err == sql.ErrNoRows || !passwordHash.Valid ||
		bcrypt.CompareHashAndPassword([]byte(normalizedHash), []byte(strings.TrimSpace(password))) != nil {
		writeError(w, http.StatusUnauthorized, "Credenciais inválidas.")
		return
	}

	isAdmin := id == 1 || (!canBuy && !canSell)
	claims := jwt.MapClaims{
		"id":               strconv.FormatInt(id, 10),
		"nome":             name,
		"nome_loja":        nullableString(storeName),
		"pode_comprar":     canBuy,
		"pode_vender":      canSell,
		"is_admin":         isAdmin,
		"telegram_chat_id": nullableString(telegramChatID),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(api.config.JWTSecret))
	if err != nil {
		log.Printf("sign login token: %v", err)
		writeError(w, http.StatusInternalServerError, "Erro ao autenticar usuário.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   signed,
	})
}

type coordinates struct {
	Latitude  *float64
	Longitude *float64
}

func (api *API) lookupCEP(ctx context.Context, rawCEP string) (coordinates, error) {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(rawCEP, "")
	if !cepPattern.MatchString(digits) {
		return coordinates{}, fmt.Errorf("invalid postal code")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://brasilapi.com.br/api/cep/v2/"+digits, nil)
	if err != nil {
		return coordinates{}, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return coordinates{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return coordinates{}, fmt.Errorf("postal code lookup failed with status %d", response.StatusCode)
	}

	var result struct {
		Location struct {
			Coordinates struct {
				Latitude  json.RawMessage `json:"latitude"`
				Longitude json.RawMessage `json:"longitude"`
			} `json:"coordinates"`
		} `json:"location"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return coordinates{}, err
	}
	latitude, err := rawFloat(result.Location.Coordinates.Latitude)
	if err != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
		return coordinates{}, fmt.Errorf("postal code latitude is invalid")
	}
	longitude, err := rawFloat(result.Location.Coordinates.Longitude)
	if err != nil || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return coordinates{}, fmt.Errorf("postal code location is unavailable")
	}
	return coordinates{
		Latitude:  &latitude,
		Longitude: &longitude,
	}, nil
}

func verifyTelegramInitData(values url.Values, botToken string) bool {
	provided, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	lines := make([]string, 0, len(values))
	for key, entries := range values {
		if key == "hash" {
			continue
		}
		for _, value := range entries {
			lines = append(lines, key+"="+value)
		}
	}
	if len(lines) == 0 {
		return false
	}
	sort.Strings(lines)
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	secretKey := secretMAC.Sum(nil)
	dataMAC := hmac.New(sha256.New, secretKey)
	_, _ = dataMAC.Write([]byte(strings.Join(lines, "\n")))
	return subtle.ConstantTimeCompare(dataMAC.Sum(nil), provided) == 1
}

func ptrValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func cepDigits(value *string) any {
	if value == nil {
		return nil
	}
	digits := regexp.MustCompile(`\D`).ReplaceAllString(*value, "")
	if digits == "" {
		return nil
	}
	return digits
}
