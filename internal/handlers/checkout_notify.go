package handlers

import (
	"context"
	"fmt"
	"html"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	maxCheckoutItemLines   = 15
	maxCheckoutDescription = 120
	checkoutNotifyTimeout  = 30 * time.Second
)

// takeBuyerChatID lê (e remove das linhas) o telegram_chat_id do comprador vindo do banco.
// Só retorna um ID numérico válido; caso contrário o comprador não é notificado.
func takeBuyerChatID(rows []map[string]any) string {
	chatID := ""
	for _, row := range rows {
		if row == nil {
			continue
		}
		if chatID == "" {
			chatID = asString(row["comprador_telegram_chat_id"])
		}
		delete(row, "comprador_telegram_chat_id")
	}
	if !isNumericIdentifier(chatID) {
		return ""
	}
	return chatID
}

// notifyBuyerCheckout envia ao comprador, no Telegram, uma mensagem por loja com os contatos.
// É pensado para rodar em goroutine: usa um contexto desvinculado da requisição (o mini-app costuma
// fechar logo após o sucesso, o que cancelaria a requisição) e nunca falha o checkout.
func (api *API) notifyBuyerCheckout(ctx context.Context, buyerChatID string, messages []string) {
	if api.config.TelegramBotToken == "" || buyerChatID == "" || len(messages) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkoutNotifyTimeout)
	defer cancel()
	for index, message := range messages {
		if err := api.sendTelegramMessage(ctx, buyerChatID, message, "HTML", nil); err != nil {
			log.Printf("send checkout contacts to buyer (message %d/%d): %v", index+1, len(messages), err)
		}
	}
}

// buildCheckoutMessages monta as mensagens (HTML do Telegram) enviadas ao comprador: uma por loja,
// com itens comprados e formas de contato. A primeira mensagem leva o cabeçalho do pedido.
func buildCheckoutMessages(groups []map[string]any, unprocessed int) []string {
	if len(groups) == 0 {
		return nil
	}

	header := "✅ <b>Pedido enviado!</b> Fale com a loja pelos contatos abaixo:"
	if len(groups) > 1 {
		header = fmt.Sprintf("✅ <b>Pedidos enviados para %d lojas!</b> Fale com cada uma pelos contatos abaixo:", len(groups))
	}
	if unprocessed == 1 {
		header += "\n⚠️ 1 item não foi incluído por falta de estoque."
	} else if unprocessed > 1 {
		header += fmt.Sprintf("\n⚠️ %d itens não foram incluídos por falta de estoque.", unprocessed)
	}

	messages := make([]string, 0, len(groups))
	for index, group := range groups {
		message := buildSupplierMessage(group)
		if index == 0 {
			message = header + "\n\n" + message
		}
		messages = append(messages, message)
	}
	return messages
}

func buildSupplierMessage(group map[string]any) string {
	var builder strings.Builder
	name := asString(group["nome_loja"])
	if name == "" {
		name = "Loja"
	}
	builder.WriteString("🏪 <b>" + html.EscapeString(name) + "</b>\n")

	items, _ := group["itens"].([]map[string]any)
	for index, item := range items {
		if index == maxCheckoutItemLines {
			builder.WriteString(fmt.Sprintf("… e mais %d item(ns)\n", len(items)-maxCheckoutItemLines))
			break
		}
		builder.WriteString("• " + buildItemLine(item) + "\n")
	}
	if total, ok := numberValue(group["valor_total"]); ok {
		builder.WriteString("<b>Total:</b> " + formatBRL(total) + "\n")
	}

	contactLines := buildContactLines(group["contato"])
	if len(contactLines) == 0 {
		builder.WriteString("\nℹ️ Esta loja ainda não cadastrou um contato. Aguarde o retorno dela.")
	} else {
		builder.WriteString("\n" + strings.Join(contactLines, "\n"))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func buildItemLine(item map[string]any) string {
	description := truncateRunes(asString(item["descricao"]), maxCheckoutDescription)
	if description == "" {
		description = "Item"
	}
	line := html.EscapeString(description)
	if quantity, ok := numberValue(item["quantidade"]); ok {
		line = fmt.Sprintf("%dx %s", int64(quantity), line)
	}
	if subtotal, ok := numberValue(item["subtotal"]); ok {
		line += " — " + formatBRL(subtotal)
	}
	return line
}

// buildContactLines usa links em texto (e não botões): o Telegram recusa a mensagem inteira se um
// botão tg://user?id= apontar para alguém com privacidade restrita, e recusa tel:/mailto: em botões.
func buildContactLines(rawContact any) []string {
	contact, _ := rawContact.(map[string]any)
	if contact == nil {
		return nil
	}
	links, _ := contact["links"].(map[string]any)

	lines := make([]string, 0, 4)
	if phone := asString(contact["telefone"]); phone != "" {
		lines = append(lines, "📞 Telefone: "+html.EscapeString(phone))
	}
	if email := asString(contact["email"]); email != "" {
		lines = append(lines, "✉️ E-mail: "+html.EscapeString(email))
	}
	if link := asString(links["whatsapp"]); link != "" {
		lines = append(lines, `💬 <a href="`+html.EscapeString(link)+`">Abrir conversa no WhatsApp</a>`)
	}
	if link := asString(links["telegram"]); link != "" {
		lines = append(lines, `✈️ <a href="`+html.EscapeString(link)+`">Abrir chat no Telegram</a>`)
	}
	return lines
}

// formatBRL formata um valor em reais no padrão brasileiro (R$ 1.234,56).
func formatBRL(value float64) string {
	cents := int64(math.Round(math.Abs(value) * 100))
	reais := strconv.FormatInt(cents/100, 10)

	var grouped strings.Builder
	for index, digit := range reais {
		if index > 0 && (len(reais)-index)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}

	sign := ""
	if value < 0 && cents > 0 {
		sign = "-"
	}
	return fmt.Sprintf("%sR$ %s,%02d", sign, grouped.String(), cents%100)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
