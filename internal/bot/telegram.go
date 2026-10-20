package bot

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Service struct {
	db    DB
	token string
}

type telegramMessage struct {
	Text string `json:"text"`
	Chat struct {
		ID json.Number `json:"id"`
	} `json:"chat"`
	From struct {
		ID json.Number `json:"id"`
	} `json:"from"`
}

type telegramCallback struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	From struct {
		ID json.Number `json:"id"`
	} `json:"from"`
}

type telegramUpdate struct {
	Message      *telegramMessage `json:"message"`
	CallbackQuery *telegramCallback `json:"callback_query"`
}

func NewService(db DB, token string) *Service {
	return &Service{db: db, token: token}
}

func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBotJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": true, "message": "Método não permitido."})
		return
	}

	body, err := ioReadBody(r)
	if err != nil {
		writeBotJSON(w, http.StatusBadRequest, map[string]any{"error": true, "message": err.Error()})
		return
	}

	var update telegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		writeBotJSON(w, http.StatusBadRequest, map[string]any{"error": true, "message": "Webhook Telegram inválido."})
		return
	}

	switch {
	case update.CallbackQuery != nil:
		s.handleTelegramCallback(w, r, update.CallbackQuery)
	case update.Message != nil:
		s.handleTelegramMessage(w, r, update.Message)
	default:
		writeBotJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Telegram webhook received."})
	}
}

func (s *Service) handleTelegramMessage(w http.ResponseWriter, r *http.Request, message *telegramMessage) {
	chatID := strings.TrimSpace(message.Chat.ID.String())
	text := strings.TrimSpace(message.Text)
	if chatID == "" {
		writeBotJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}

	if s.db != nil && !s.isApprovedTelegramUser(r.Context(), chatID) {
		if !(strings.HasPrefix(text, "/registrar") || strings.HasPrefix(text, "/ajuda") || strings.HasPrefix(text, "/buscar")) {
			_ = s.sendTelegramMessage(r.Context(), chatID, "usuario não registrado, use /registrar nome pra se registrar ", "", nil)
			writeBotJSON(w, http.StatusOK, map[string]any{"success": true})
			return
		}
	}

	switch {
	case strings.HasPrefix(text, "/registrar"):
		_ = s.sendTelegramMessage(r.Context(), chatID, "Clique no botão abaixo para preencher as informações da sua loja e concluir o cadastro:", "", map[string]any{
			"inline_keyboard": [][]map[string]string{{
				{"text": "📝 Concluir Cadastro da Loja", "web_app": "https://admin.luw17.online/telegram-cadastro"},
			}},
		})
	case strings.HasPrefix(text, "/buscar"):
		_ = s.sendTelegramMessage(r.Context(), chatID, "Para fazer a busca aperte o botão abaixo", "", map[string]any{
			"inline_keyboard": [][]map[string]string{{
				{"text": "Buscar", "web_app": "https://admin.luw17.online/busca"},
			}},
		})
	case strings.HasPrefix(text, "/ajuda"):
		_ = s.sendTelegramMessage(r.Context(), chatID, "/pedido \"item\" -> realiza o pedido de um item em específico\n\n/buscar -> busca nos itens já registrados\n\n/status -> mostra o status do último pedido\n\n/vertodos -> mostra todos os pedidos da conta\n", "", nil)
	case strings.HasPrefix(text, "/status"):
		s.handleOrderStatus(r, chatID)
	case strings.HasPrefix(text, "/vertodos"):
		s.handleAllOrders(r, chatID)
	case strings.HasPrefix(text, "/pedido"):
		s.handlePurchaseRequest(r, chatID, text)
	case strings.HasPrefix(text, "/oferta"):
		s.handleOfferInsert(r, chatID, text)
	default:
		_ = s.sendTelegramMessage(r.Context(), chatID, "Comando não reconhecido. Use /ajuda para ver os comandos disponíveis.", "", nil)
	}

	writeBotJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Service) handleTelegramCallback(w http.ResponseWriter, r *http.Request, callback *telegramCallback) {
	data := strings.TrimSpace(callback.Data)
	if data == "" {
		writeBotJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}

	switch {
	case strings.HasPrefix(data, "apr_"):
		s.processUserApproval(r, callback, data)
	case strings.HasPrefix(data, "recu_"):
		s.processUserRejection(r, callback, data)
	case strings.HasPrefix(data, "cotar_"):
		instruction := "Para fazer sua oferta, copie a mensagem abaixo, coloque o seu preço no final e envie: `/oferta " + strings.TrimPrefix(data, "cotar_") + " 00.00`"
		_ = s.sendTelegramMessage(r.Context(), callback.From.ID.String(), instruction, "MarkdownV2", nil)
		s.answerCallbackQuery(callback.ID, "Instrução enviada ao vendedor.", false)
	case strings.HasPrefix(data, "aceitar_oferta_"):
		s.acceptOfferAndNotifyStore(r, callback, data)
	case strings.HasPrefix(data, "reservar_"):
		s.reserveOrderAndNotifyBuyer(r, callback, data)
	case strings.HasPrefix(data, "conf_"):
		s.handleStockConfirmation(r, callback, data)
	case strings.HasPrefix(data, "rec_"):
		s.handleStockRejection(r, callback, data)
	default:
		s.answerCallbackQuery(callback.ID, "Ação recebida.", false)
	}

	writeBotJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Service) isApprovedTelegramUser(ctx context.Context, chatID string) bool {
	if s.db == nil || chatID == "" {
		return false
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM usuarios
			WHERE telegram_chat_id = $1
			  AND status = 'aprovado'
			LIMIT 1
		)
	`, chatID).Scan(&exists)
	return err == nil && exists
}

func (s *Service) handleOrderStatus(r *http.Request, chatID string) {
	rows, err := s.queryRowsMap(r.Context(), `
		SELECT id, comprador_chat_id, descricao_peca, status, criado_em
		FROM pedidos_pecas
		WHERE comprador_chat_id = $1
		ORDER BY criado_em DESC
		LIMIT 1
	`, chatID)
	if err != nil || len(rows) == 0 {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Nenhum pedido encontrado para este usuário.", "", nil)
		return
	}
	order := rows[0]
	_ = s.sendTelegramMessage(r.Context(), chatID, fmt.Sprintf("📦 <b>Status do seu último pedido:</b>\n\n<b>Peça:</b> %v\n<b>Status:</b> %v\n<b>Criado em:</b> %v", order["descricao_peca"], order["status"], order["criado_em"]), "HTML", nil)
}

func (s *Service) handleAllOrders(r *http.Request, chatID string) {
	rows, err := s.queryRowsMap(r.Context(), `
		SELECT id, descricao_peca, status, criado_em
		FROM pedidos_pecas
		WHERE comprador_chat_id = $1
		ORDER BY criado_em DESC
	`, chatID)
	if err != nil || len(rows) == 0 {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Nenhum pedido encontrado.", "", nil)
		return
	}
	var builder strings.Builder
	builder.WriteString("📦 *Lista de Pedidos:*\n\n")
	for _, item := range rows {
		builder.WriteString(fmt.Sprintf("*Peça:* %v\nStatus: %v\nID do Pedido: %v\n\n", item["descricao_peca"], item["status"], item["id"]))
	}
	_ = s.sendTelegramMessage(r.Context(), chatID, builder.String(), "", nil)
}

func (s *Service) handlePurchaseRequest(r *http.Request, chatID, text string) {
	description := strings.TrimSpace(strings.TrimPrefix(text, "/pedido"))
	description = strings.TrimSpace(description)
	if description == "" {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Formato inválido. Use: /pedido \"descrição da peça\"", "", nil)
		return
	}
	if s.db == nil {
		_ = s.sendTelegramMessage(r.Context(), chatID, "O sistema de pedidos está indisponível no momento.", "", nil)
		return
	}

	var pedidoID int64
	if err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO pedidos_pecas (comprador_chat_id, descricao_peca, status)
		VALUES ($1, $2, 'pendente')
		RETURNING id
	`, chatID, description).Scan(&pedidoID); err != nil {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Não foi possível registrar o pedido neste momento.", "", nil)
		return
	}

	rows, err := s.queryRowsMap(r.Context(), `
		SELECT telegram_chat_id
		FROM usuarios
		WHERE pode_vender = TRUE
		  AND status = 'aprovado'
		  AND telegram_chat_id IS NOT NULL
		  AND telegram_chat_id <> $1
	`, chatID)
	if err != nil {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Pedido registrado! buscando peça", "", nil)
		return
	}
	for _, row := range rows {
		vendorID, _ := row["telegram_chat_id"].(string)
		if vendorID == "" {
			continue
		}
		_ = s.sendTelegramMessage(r.Context(), vendorID, fmt.Sprintf("🚨 Nova busca por peça!\nPedido #%d\nPeça: %s", pedidoID, description), "", map[string]any{
			"inline_keyboard": [][]map[string]string{{
				{"text": "Fazer Oferta", "callback_data": "cotar_" + strconv.FormatInt(pedidoID, 10)},
			}},
		})
	}
	_ = s.sendTelegramMessage(r.Context(), chatID, "Pedido registrado! buscando peça", "", nil)
}

func (s *Service) handleOfferInsert(r *http.Request, chatID, text string) {
	parts := strings.Fields(text)
	if len(parts) < 3 {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Formato inválido. Use: /oferta <pedido_id> <valor>", "", nil)
		return
	}
	pedidoID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || pedidoID < 1 {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Pedido inválido.", "", nil)
		return
	}
	preco, err := strconv.ParseFloat(strings.ReplaceAll(parts[2], ",", "."), 64)
	if err != nil || preco <= 0 {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Preço inválido.", "", nil)
		return
	}
	if s.db == nil {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Sistema de ofertas indisponível no momento.", "", nil)
		return
	}
	var offerID int64
	if err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO ofertas (pedido_id, loja_chat_id, preco)
		VALUES ($1, $2, $3)
		RETURNING id
	`, pedidoID, chatID, preco).Scan(&offerID); err != nil {
		_ = s.sendTelegramMessage(r.Context(), chatID, "Não foi possível registrar a oferta neste momento.", "", nil)
		return
	}

	buyerChatID := ""
	var buyerName string
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT p.comprador_chat_id, COALESCE(u.nome_loja, u.nome)
		FROM pedidos_pecas p
		LEFT JOIN usuarios u ON u.telegram_chat_id = p.comprador_chat_id
		WHERE p.id = $1
	`, pedidoID).Scan(&buyerChatID, &buyerName); err == nil && buyerChatID != "" {
		_ = s.sendTelegramMessage(r.Context(), buyerChatID, fmt.Sprintf("🔔 *Nova Oferta Recebida!*\nLoja: %s\nValor: R$ %.2f\n\nPara aceitar, clique no botão abaixo:", buyerName, preco), "MarkdownV2", map[string]any{
			"inline_keyboard": [][]map[string]string{{
				{"text": "✅ Aceitar Oferta de R$ " + strconv.FormatFloat(preco, 'f', 2, 64), "callback_data": "aceitar_oferta_" + strconv.FormatInt(offerID, 10)},
			}},
		})
	}
	_ = s.sendTelegramMessage(r.Context(), chatID, fmt.Sprintf("Oferta registrada com sucesso! ID da oferta: %d", offerID), "", nil)
}

func (s *Service) processUserApproval(r *http.Request, callback *telegramCallback, data string) {
	telegramID := strings.TrimPrefix(data, "apr_")
	if telegramID == "" {
		s.answerCallbackQuery(callback.ID, "Dados inválidos.", true)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE usuarios
		SET status = 'aprovado'
		WHERE telegram_chat_id = $1
	`, telegramID); err != nil {
		s.answerCallbackQuery(callback.ID, "Erro ao aprovar usuário.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), telegramID, "🎉 Boas notícias! Seu cadastro foi aprovado com sucesso. Você já está liberado para usar o sistema!", "", nil)
	s.answerCallbackQuery(callback.ID, "Usuário ativado com sucesso!", true)
}

func (s *Service) processUserRejection(r *http.Request, callback *telegramCallback, data string) {
	telegramID := strings.TrimPrefix(data, "recu_")
	if telegramID == "" {
		s.answerCallbackQuery(callback.ID, "Dados inválidos.", true)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE usuarios
		SET status = 'recusado'
		WHERE telegram_chat_id = $1
	`, telegramID); err != nil {
		s.answerCallbackQuery(callback.ID, "Erro ao recusar usuário.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), telegramID, "❌ Infelizmente, seu cadastro não foi aprovado no momento.\n\nSe você acredita que isso foi um engano ou deseja revisar os dados enviados, entre em contato com o suporte.", "", nil)
	s.answerCallbackQuery(callback.ID, "Usuário recusado com sucesso!", true)
}

func (s *Service) acceptOfferAndNotifyStore(r *http.Request, callback *telegramCallback, data string) {
	offerID := strings.TrimPrefix(data, "aceitar_oferta_")
	var accepted struct {
		ID         int64
		PedidoID   int64
		LojaChatID string
		Preco      float64
	}
	if err := s.db.QueryRowContext(r.Context(), `
		UPDATE ofertas
		SET status = 'aceita'
		WHERE id = $1
		RETURNING id, pedido_id, loja_chat_id, preco
	`, offerID).Scan(&accepted.ID, &accepted.PedidoID, &accepted.LojaChatID, &accepted.Preco); err != nil {
		s.answerCallbackQuery(callback.ID, "Não foi possível aceitar a oferta.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), accepted.LojaChatID, fmt.Sprintf("🎉 <b>Parabéns! Sua oferta foi aceita!</b>\n\nO comprador escolheu a sua oferta de <b>R$ %.2f</b> para o Pedido <b>#%d</b>.\n\nPor favor, separe a peça e confirme a reserva clicando no botão abaixo:", accepted.Preco, accepted.PedidoID), "HTML", map[string]any{
		"inline_keyboard": [][]map[string]string{{
			{"text": "📦 Confirmar Reserva", "callback_data": "reservar_" + strconv.FormatInt(accepted.PedidoID, 10)},
		}},
	})
	s.answerCallbackQuery(callback.ID, "Oferta escolhida! Avisamos a loja para reservar a peça.", true)
}

func (s *Service) reserveOrderAndNotifyBuyer(r *http.Request, callback *telegramCallback, data string) {
	pedidoID := strings.TrimPrefix(data, "reservar_")
	var order struct {
		ID              int64
		CompradorChatID string
		DescricaoPeca   string
		NomeLoja        string
	}
	if err := s.db.QueryRowContext(r.Context(), `
		UPDATE pedidos_pecas p
		SET status = 'atendido'
		FROM usuarios u
		WHERE p.id = $1 AND u.telegram_chat_id = $2
		RETURNING p.id, p.comprador_chat_id, p.descricao_peca, COALESCE(u.nome_loja, u.nome) AS nome_loja
	`, pedidoID, callback.From.ID.String()).Scan(&order.ID, &order.CompradorChatID, &order.DescricaoPeca, &order.NomeLoja); err != nil {
		s.answerCallbackQuery(callback.ID, "Não foi possível confirmar a reserva.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), order.CompradorChatID, fmt.Sprintf("✅ Tudo certo! A loja %s confirmou que a peça %s já está separada e reservada para você. Pode mandar alguém buscar!", order.NomeLoja, order.DescricaoPeca), "HTML", nil)
	s.answerCallbackQuery(callback.ID, "Reserva confirmada! O comprador foi avisado.", true)
}

func parseStockDecisionData(data string) (int64, string, error) {
	payload := strings.TrimPrefix(data, "conf_")
	if strings.HasPrefix(data, "rec_") {
		payload = strings.TrimPrefix(data, "rec_")
	}
	parts := strings.Split(payload, "_")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid stock decision payload")
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id < 1 {
		return 0, "", fmt.Errorf("invalid stock id")
	}
	return id, parts[1], nil
}

func (s *Service) handleStockConfirmation(r *http.Request, callback *telegramCallback, data string) {
	stockID, buyerChatID, err := parseStockDecisionData(data)
	if err != nil {
		s.answerCallbackQuery(callback.ID, "Dados da confirmação inválidos.", true)
		return
	}
	if buyerChatID == "" {
		s.answerCallbackQuery(callback.ID, "Não foi possível localizar o comprador.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), buyerChatID, "✅ O fornecedor separou a sua peça! O pedido está confirmado e você já pode mandar buscar.", "", nil)
	s.answerCallbackQuery(callback.ID, "Aguardando confirmação da loja", false)
	_ = s.sendTelegramMessage(r.Context(), callback.From.ID.String(), "✅ Pedido confirmado com sucesso.", "", nil)
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE estoque_fornecedor
		SET quantidade = quantidade - 1
		WHERE id = $1
	`, stockID); err != nil {
		s.answerCallbackQuery(callback.ID, "Houve um problema ao atualizar o estoque.", true)
	}
}

func (s *Service) handleStockRejection(r *http.Request, callback *telegramCallback, data string) {
	stockID, buyerChatID, err := parseStockDecisionData(data)
	if err != nil {
		s.answerCallbackQuery(callback.ID, "Dados da recusa inválidos.", true)
		return
	}
	if buyerChatID == "" {
		s.answerCallbackQuery(callback.ID, "Não foi possível localizar o comprador.", true)
		return
	}
	_ = s.sendTelegramMessage(r.Context(), buyerChatID, "❌ O fornecedor informou que a peça solicitada não está mais disponível no estoque imediato. Tente buscar outra opção!", "", nil)
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE estoque_fornecedor
		SET quantidade = GREATEST(quantidade, 0)
		WHERE id = $1
	`, stockID); err != nil {
		s.answerCallbackQuery(callback.ID, "Não foi possível atualizar o estoque.", true)
		return
	}
	s.answerCallbackQuery(callback.ID, "Pedido recusado pelo fornecedor", false)
}

func (s *Service) queryRowsMap(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		scanArgs := make([]any, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for i, name := range columns {
			row[name] = normalizeBotValue(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func normalizeBotValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case nil:
		return nil
	default:
		return v
	}
}

func (s *Service) sendTelegramMessage(ctx context.Context, chatID, text, parseMode string, keyboard map[string]any) error {
	if s.token == "" || chatID == "" {
		return nil
	}
	payload := map[string]any{"chat_id": chatID, "text": text}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+s.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("telegram rejected message: %d", response.StatusCode)
	}
	return nil
}

func (s *Service) answerCallbackQuery(queryID, text string, showAlert bool) {
	if s.token == "" || queryID == "" {
		return
	}
	payload := map[string]any{"callback_query_id": queryID, "text": text, "show_alert": showAlert}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.telegram.org/bot"+s.token+"/answerCallbackQuery", bytes.NewReader(body))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	_, _ = (&http.Client{Timeout: 8 * time.Second}).Do(request)
}

func writeBotJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ioReadBody(r *http.Request) ([]byte, error) {
	body, err := ioReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func ioReadAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	return ioReadAllFn(r)
}

func ioReadAllFn(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var b bytes.Buffer
	if _, err := b.ReadFrom(r); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
