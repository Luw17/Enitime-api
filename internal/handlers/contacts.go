package handlers

import (
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// normalizePhoneDigits devolve apenas os dígitos de um telefone/WhatsApp em formato
// internacional (DDI + DDD + número). Aceita IDs do WhatsApp com sufixo (ex.: "5511999999999@s.whatsapp.net").
// Números com 10 ou 11 dígitos, sem "+", são tratados como brasileiros e recebem o DDI 55.
// Retorna "" quando o valor não parece um telefone válido.
func normalizePhoneDigits(raw string) string {
	raw = strings.TrimSpace(raw)
	international := strings.HasPrefix(raw, "+")
	if at := strings.Index(raw, "@"); at >= 0 {
		raw = raw[:at]
	}
	if colon := strings.Index(raw, ":"); colon >= 0 {
		raw = raw[:colon]
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	digits = strings.TrimLeft(digits, "0")
	if !international && (len(digits) == 10 || len(digits) == 11) {
		digits = "55" + digits
	}
	if len(digits) < 12 || len(digits) > 15 {
		return ""
	}
	return digits
}

// contactLinks monta os links de contato direto (WhatsApp, Telegram, telefone e e-mail)
// somente para os canais que possuem dado válido. canal_preferencial segue a ordem
// whatsapp > telegram > telefone > email.
func contactLinks(phone, whatsapp, email, telegramChatID string) map[string]any {
	links := map[string]any{}
	channels := make([]string, 0, 4)

	if digits := normalizePhoneDigits(whatsapp); digits != "" {
		links["whatsapp"] = "https://wa.me/" + digits
		channels = append(channels, "whatsapp")
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(telegramChatID), 10, 64); err == nil && id > 0 {
		links["telegram"] = "tg://user?id=" + strconv.FormatInt(id, 10)
		channels = append(channels, "telegram")
	}
	if digits := normalizePhoneDigits(phone); digits != "" {
		links["telefone"] = "tel:+" + digits
		channels = append(channels, "telefone")
	}
	if address, err := mail.ParseAddress(strings.TrimSpace(email)); err == nil {
		links["email"] = "mailto:" + url.PathEscape(address.Address)
		channels = append(channels, "email")
	}

	links["disponivel"] = len(channels) > 0
	if len(channels) > 0 {
		links["canal_preferencial"] = channels[0]
	} else {
		links["canal_preferencial"] = nil
	}
	return links
}

// groupOrdersBySupplier agrupa os pedidos gerados no checkout por loja, devolvendo uma
// entrada por loja com nome, contato (com links) e o que foi comprado nela.
func groupOrdersBySupplier(rows []map[string]any) []map[string]any {
	groups := make([]map[string]any, 0)
	index := make(map[string]int)

	for _, row := range rows {
		if row == nil {
			continue
		}
		key := asString(row["loja_id"])
		if key == "" {
			key = asString(row["fornecedor_id"])
		}

		position, exists := index[key]
		if !exists {
			position = len(groups)
			index[key] = position
			groups = append(groups, map[string]any{
				"loja_id":     key,
				"nome_loja":   asString(row["fornecedor_nome"]),
				"contato":     row["fornecedor_contato"],
				"pedido_ids":  []any{},
				"itens":       []map[string]any{},
				"valor_total": float64(0),
			})
		}
		group := groups[position]

		quantity, _ := numberValue(row["quantidade"])
		unitPrice, hasPrice := numberValue(row["produto_preco"])
		item := map[string]any{
			"pedido_id":       row["id"],
			"produto_id":      row["estoque_id"],
			"descricao":       row["descricao_peca"],
			"sku":             row["produto_sku"],
			"part_number":     row["produto_part_number"],
			"tipo_componente": row["produto_tipo_componente"],
			"marca_qualidade": row["produto_marca_qualidade"],
			"quantidade":      row["quantidade"],
			"observacao":      row["observacao"],
			"preco_unitario":  nil,
			"subtotal":        nil,
		}
		if hasPrice {
			subtotal := roundMoney(unitPrice * quantity)
			item["preco_unitario"] = unitPrice
			item["subtotal"] = subtotal
			if total, ok := group["valor_total"].(float64); ok {
				group["valor_total"] = roundMoney(total + subtotal)
			}
		} else {
			// sem preço em algum item o total da loja seria enganoso
			group["valor_total"] = nil
		}

		group["pedido_ids"] = append(group["pedido_ids"].([]any), row["id"])
		group["itens"] = append(group["itens"].([]map[string]any), item)
	}

	for _, group := range groups {
		group["total_pedidos"] = len(group["itens"].([]map[string]any))
	}
	return groups
}

func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

func roundMoney(value float64) float64 {
	return math.Round(value*100) / 100
}
