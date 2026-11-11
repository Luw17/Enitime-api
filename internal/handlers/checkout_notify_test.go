package handlers

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatBRL(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{0, "R$ 0,00"},
		{5, "R$ 5,00"},
		{29.9, "R$ 29,90"},
		{230.9, "R$ 230,90"},
		{999.99, "R$ 999,99"},
		{1000, "R$ 1.000,00"},
		{1234567.891, "R$ 1.234.567,89"},
		{0.005, "R$ 0,01"},
		{-12.5, "-R$ 12,50"},
	}
	for _, test := range tests {
		if got := formatBRL(test.value); got != test.want {
			t.Errorf("formatBRL(%v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func fullContactGroup() map[string]any {
	return map[string]any{
		"nome_loja": "Loja Alpha",
		"contato": map[string]any{
			"nome": "Loja Alpha", "telefone": "(11) 3333-4444", "email": "alpha@example.com",
			"links": map[string]any{
				"whatsapp": "https://wa.me/5511999999999",
				"telegram": "tg://user?id=222",
			},
		},
		"valor_total": 230.9,
		"itens": []map[string]any{
			{"descricao": "Tela - iPhone 11", "quantidade": int64(2), "subtotal": 201.0},
			{"descricao": "Bateria - iPhone 11", "quantidade": int64(1), "subtotal": 29.9},
		},
	}
}

func TestBuildCheckoutMessagesSingleSupplier(t *testing.T) {
	messages := buildCheckoutMessages([]map[string]any{fullContactGroup()}, 0)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]
	for _, want := range []string{
		"✅ <b>Pedido enviado!</b>",
		"🏪 <b>Loja Alpha</b>",
		"• 2x Tela - iPhone 11 — R$ 201,00",
		"• 1x Bateria - iPhone 11 — R$ 29,90",
		"<b>Total:</b> R$ 230,90",
		"📞 Telefone: (11) 3333-4444",
		"✉️ E-mail: alpha@example.com",
		`<a href="https://wa.me/5511999999999">Abrir conversa no WhatsApp</a>`,
		`<a href="tg://user?id=222">Abrir chat no Telegram</a>`,
	} {
		if !strings.Contains(message, want) {
			t.Errorf("mensagem sem %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "falta de estoque") {
		t.Errorf("mensagem não deveria avisar falta de estoque:\n%s", message)
	}
	if strings.HasSuffix(message, "\n") {
		t.Errorf("mensagem termina com quebra de linha: %q", message)
	}
}

func TestBuildCheckoutMessagesMultipleSuppliers(t *testing.T) {
	beta := map[string]any{
		"nome_loja": "Loja Beta", "valor_total": nil,
		"contato": map[string]any{"nome": "Loja Beta", "links": map[string]any{"disponivel": false}},
		"itens":   []map[string]any{{"descricao": "Cabo - Moto G", "quantidade": int64(3), "subtotal": nil}},
	}
	messages := buildCheckoutMessages([]map[string]any{fullContactGroup(), beta}, 0)
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 (uma por loja)", len(messages))
	}
	if !strings.Contains(messages[0], "Pedidos enviados para 2 lojas!") || !strings.Contains(messages[0], "Loja Alpha") {
		t.Errorf("primeira mensagem deveria ter o cabeçalho e a Loja Alpha:\n%s", messages[0])
	}
	if strings.Contains(messages[1], "Pedidos enviados") || !strings.Contains(messages[1], "🏪 <b>Loja Beta</b>") {
		t.Errorf("segunda mensagem não deveria repetir o cabeçalho:\n%s", messages[1])
	}
	if !strings.Contains(messages[1], "• 3x Cabo - Moto G\n") {
		t.Errorf("item sem preço deveria sair sem valor:\n%s", messages[1])
	}
	if strings.Contains(messages[1], "R$") || strings.Contains(messages[1], "Total") {
		t.Errorf("loja sem preço não deveria mostrar valores:\n%s", messages[1])
	}
	if !strings.Contains(messages[1], "ainda não cadastrou um contato") {
		t.Errorf("loja sem contato deveria avisar:\n%s", messages[1])
	}
}

func TestBuildCheckoutMessagesEscapesHTML(t *testing.T) {
	group := map[string]any{
		"nome_loja": `<b>Loja & "Cia"</b>`,
		"contato": map[string]any{
			"telefone": "<script>1</script>", "email": "a&b@example.com",
			"links": map[string]any{"whatsapp": `https://wa.me/55?x="><b>`},
		},
		"itens": []map[string]any{{"descricao": "Tela <i>OLED</i>", "quantidade": int64(1)}},
	}
	message := buildCheckoutMessages([]map[string]any{group}, 0)[0]
	for _, forbidden := range []string{"<script>", "<i>OLED", `"><b>`, "<b>Loja &"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("mensagem contém HTML não escapado %q:\n%s", forbidden, message)
		}
	}
	for _, want := range []string{"&lt;b&gt;Loja &amp; &#34;Cia&#34;&lt;/b&gt;", "&lt;script&gt;", "a&amp;b@example.com", "Tela &lt;i&gt;OLED&lt;/i&gt;"} {
		if !strings.Contains(message, want) {
			t.Errorf("mensagem sem %q:\n%s", want, message)
		}
	}
}

func TestBuildCheckoutMessagesUnprocessedWarning(t *testing.T) {
	groups := []map[string]any{fullContactGroup()}
	if got := buildCheckoutMessages(groups, 1)[0]; !strings.Contains(got, "⚠️ 1 item não foi incluído por falta de estoque.") {
		t.Errorf("aviso singular ausente:\n%s", got)
	}
	if got := buildCheckoutMessages(groups, 3)[0]; !strings.Contains(got, "⚠️ 3 itens não foram incluídos por falta de estoque.") {
		t.Errorf("aviso plural ausente:\n%s", got)
	}
}

func TestBuildCheckoutMessagesLimitsItemsAndLength(t *testing.T) {
	items := make([]map[string]any, 0, 40)
	for i := 0; i < 40; i++ {
		items = append(items, map[string]any{
			"descricao": strings.Repeat("Peça muito longa ", 40), "quantidade": int64(1), "subtotal": 10.0,
		})
	}
	group := fullContactGroup()
	group["itens"] = items
	message := buildCheckoutMessages([]map[string]any{group}, 0)[0]

	if strings.Count(message, "• ") != maxCheckoutItemLines {
		t.Errorf("itens listados = %d, want %d", strings.Count(message, "• "), maxCheckoutItemLines)
	}
	if !strings.Contains(message, "… e mais 25 item(ns)") {
		t.Errorf("faltou o resumo dos itens omitidos:\n%s", message)
	}
	if utf8.RuneCountInString(message) > 4096 {
		t.Errorf("mensagem com %d caracteres excede o limite do Telegram (4096)", utf8.RuneCountInString(message))
	}
	if !strings.Contains(message, "📞 Telefone: (11) 3333-4444") || !strings.Contains(message, "Abrir conversa no WhatsApp") {
		t.Errorf("contato deveria continuar na mensagem mesmo com muitos itens:\n%s", message)
	}
}

func TestBuildCheckoutMessagesEmpty(t *testing.T) {
	if got := buildCheckoutMessages(nil, 2); got != nil {
		t.Fatalf("buildCheckoutMessages(nil) = %#v, want nil", got)
	}
}

func TestBuildCheckoutMessagesFromGroupedOrders(t *testing.T) {
	rows := []map[string]any{
		{
			"id": int64(1), "loja_id": "2", "fornecedor_nome": "Loja Alpha", "descricao_peca": "Tela - iPhone 11",
			"quantidade": int64(2), "produto_preco": 100.5,
			"fornecedor_telefone": "11999999999", "fornecedor_whatsapp_id": "5511999999999@s.whatsapp.net",
			"fornecedor_email": nil, "fornecedor_telegram_chat_id": "222",
			"fornecedor_contato": map[string]any{"nome": "Loja Alpha", "telefone": "11999999999", "whatsapp_id": "5511999999999@s.whatsapp.net", "email": nil},
		},
	}
	enrichOrdersWithContacts(rows, "fornecedor_nome", "fornecedor_telefone", "fornecedor_whatsapp_id", "fornecedor_email", "fornecedor_telegram_chat_id", "fornecedor_contato")
	messages := buildCheckoutMessages(groupOrdersBySupplier(rows), 0)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	for _, want := range []string{"• 2x Tela - iPhone 11 — R$ 201,00", "📞 Telefone: 11999999999", "https://wa.me/5511999999999", "tg://user?id=222"} {
		if !strings.Contains(messages[0], want) {
			t.Errorf("mensagem sem %q:\n%s", want, messages[0])
		}
	}
	if strings.Contains(messages[0], "E-mail") {
		t.Errorf("não deveria exibir e-mail ausente:\n%s", messages[0])
	}
}

func TestTakeBuyerChatID(t *testing.T) {
	t.Run("retorna o ID e remove a chave das linhas", func(t *testing.T) {
		rows := []map[string]any{
			nil,
			{"id": int64(1), "comprador_telegram_chat_id": "111"},
			{"id": int64(2), "comprador_telegram_chat_id": "111"},
		}
		if got := takeBuyerChatID(rows); got != "111" {
			t.Fatalf("takeBuyerChatID() = %q, want 111", got)
		}
		for _, row := range rows {
			if _, exists := row["comprador_telegram_chat_id"]; exists {
				t.Fatalf("chave comprador_telegram_chat_id deveria ser removida: %#v", row)
			}
		}
	})

	for name, value := range map[string]any{
		"nulo":         nil,
		"vazio":        "",
		"nao numerico": "abc",
		"zero":         "0",
	} {
		t.Run("ignora "+name, func(t *testing.T) {
			rows := []map[string]any{{"comprador_telegram_chat_id": value}}
			if got := takeBuyerChatID(rows); got != "" {
				t.Fatalf("takeBuyerChatID() = %q, want vazio", got)
			}
		})
	}

	if got := takeBuyerChatID(nil); got != "" {
		t.Fatalf("takeBuyerChatID(nil) = %q, want vazio", got)
	}
}

func TestNotifyBuyerCheckoutSkipsWithoutTokenOrChat(t *testing.T) {
	// Sem token ou sem chat não pode haver chamada de rede nem pânico (api.db é nil de propósito).
	api := &API{}
	api.notifyBuyerCheckout(context.Background(), "111", []string{"msg"})

	api.config.TelegramBotToken = "token-de-teste"
	api.notifyBuyerCheckout(context.Background(), "", []string{"msg"})
	api.notifyBuyerCheckout(context.Background(), "111", nil)
}
