package handlers

import (
	"reflect"
	"testing"
)

func TestNormalizePhoneDigits(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "celular BR sem DDI recebe 55", raw: "(11) 99999-9999", want: "5511999999999"},
		{name: "fixo BR sem DDI recebe 55", raw: "11 3333-4444", want: "551133334444"},
		{name: "ja com DDI", raw: "5511999999999", want: "5511999999999"},
		{name: "sufixo JID do WhatsApp", raw: "5511999999999@s.whatsapp.net", want: "5511999999999"},
		{name: "sufixo JID com dispositivo", raw: "5511999999999:12@s.whatsapp.net", want: "5511999999999"},
		{name: "zero a esquerda do DDD", raw: "011999999999", want: "5511999999999"},
		{name: "internacional com + nao recebe 55", raw: "+1 415 555 2671", want: ""},
		{name: "internacional longo com +", raw: "+351 912 345 678", want: "351912345678"},
		{name: "sem DDD e invalido", raw: "99999-9999", want: ""},
		{name: "vazio", raw: "", want: ""},
		{name: "texto", raw: "sem telefone", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizePhoneDigits(test.raw); got != test.want {
				t.Fatalf("normalizePhoneDigits(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestContactLinks(t *testing.T) {
	t.Run("todos os canais", func(t *testing.T) {
		links := contactLinks("11 3333-4444", "5511999999999", "loja@example.com", "123456789")
		want := map[string]any{
			"whatsapp":           "https://wa.me/5511999999999",
			"telegram":           "tg://user?id=123456789",
			"telefone":           "tel:+551133334444",
			"email":              "mailto:loja@example.com",
			"disponivel":         true,
			"canal_preferencial": "whatsapp",
		}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("contactLinks() = %#v, want %#v", links, want)
		}
	})

	t.Run("sem whatsapp prefere telegram", func(t *testing.T) {
		links := contactLinks("", "", "", "123456789")
		if links["canal_preferencial"] != "telegram" || links["telegram"] != "tg://user?id=123456789" {
			t.Fatalf("contactLinks() = %#v, want telegram preferred", links)
		}
		if _, exists := links["whatsapp"]; exists {
			t.Fatalf("contactLinks() = %#v, whatsapp nao deveria existir", links)
		}
	})

	t.Run("somente telefone", func(t *testing.T) {
		links := contactLinks("(11) 98888-7777", "", "", "")
		if links["canal_preferencial"] != "telefone" || links["telefone"] != "tel:+5511988887777" {
			t.Fatalf("contactLinks() = %#v, want telefone only", links)
		}
	})

	t.Run("somente email", func(t *testing.T) {
		links := contactLinks("", "", "  loja+pecas@example.com ", "")
		if links["canal_preferencial"] != "email" || links["email"] != "mailto:loja+pecas@example.com" {
			t.Fatalf("contactLinks() = %#v, want email only", links)
		}
	})

	t.Run("sem nenhum canal", func(t *testing.T) {
		links := contactLinks("", "", "", "")
		if links["disponivel"] != false || links["canal_preferencial"] != nil || len(links) != 2 {
			t.Fatalf("contactLinks() = %#v, want disponivel=false", links)
		}
	})

	t.Run("valores invalidos sao ignorados", func(t *testing.T) {
		links := contactLinks("abc", "12", "nao-e-email", "-100123")
		if links["disponivel"] != false {
			t.Fatalf("contactLinks() = %#v, want disponivel=false", links)
		}
	})
}

func TestEnrichOrdersWithContactsAddsLinks(t *testing.T) {
	rows := []map[string]any{
		{
			"fornecedor_nome":             "Loja Alpha",
			"fornecedor_telefone":         "11999999999",
			"fornecedor_whatsapp_id":      "5511999999999",
			"fornecedor_email":            "alpha@example.com",
			"fornecedor_telegram_chat_id": "42",
			"fornecedor_contato": map[string]any{
				"nome": "Loja Alpha", "telefone": "11999999999",
				"whatsapp_id": "5511999999999", "email": "alpha@example.com",
			},
		},
		{
			"fornecedor_nome": "Loja Beta", "fornecedor_telefone": nil, "fornecedor_whatsapp_id": nil,
			"fornecedor_email": nil, "fornecedor_telegram_chat_id": nil, "fornecedor_contato": nil,
		},
	}
	enrichOrdersWithContacts(rows, "fornecedor_nome", "fornecedor_telefone", "fornecedor_whatsapp_id", "fornecedor_email", "fornecedor_telegram_chat_id", "fornecedor_contato")

	alpha, ok := rows[0]["fornecedor_contato"].(map[string]any)
	if !ok {
		t.Fatalf("contato Alpha = %#v, want map", rows[0]["fornecedor_contato"])
	}
	if alpha["telegram_chat_id"] != "42" {
		t.Fatalf("telegram_chat_id = %#v, want 42", alpha["telegram_chat_id"])
	}
	links, _ := alpha["links"].(map[string]any)
	if links["whatsapp"] != "https://wa.me/5511999999999" || links["telegram"] != "tg://user?id=42" || links["telefone"] != "tel:+5511999999999" {
		t.Fatalf("links Alpha = %#v", links)
	}

	// Loja sem nenhum dado de contato nao pode quebrar nem inventar links.
	beta, ok := rows[1]["fornecedor_contato"].(map[string]any)
	if ok {
		links, _ := beta["links"].(map[string]any)
		if links["disponivel"] != false {
			t.Fatalf("links Beta = %#v, want disponivel=false", links)
		}
	}
}

func TestGroupOrdersBySupplier(t *testing.T) {
	alphaContact := map[string]any{"nome": "Loja Alpha", "links": map[string]any{"whatsapp": "https://wa.me/5511999999999"}}
	betaContact := map[string]any{"nome": "Loja Beta", "links": map[string]any{"disponivel": false}}
	rows := []map[string]any{
		{
			"id": int64(1), "loja_id": "10", "fornecedor_id": "10", "fornecedor_nome": "Loja Alpha", "fornecedor_contato": alphaContact,
			"estoque_id": int64(100), "descricao_peca": "Tela - iPhone 11", "quantidade": int64(2), "produto_preco": 100.5,
			"produto_sku": "SKU-1", "produto_part_number": "PN-1",
		},
		{
			"id": int64(2), "loja_id": "20", "fornecedor_id": "999", "fornecedor_nome": "Loja Beta", "fornecedor_contato": betaContact,
			"estoque_id": int64(200), "descricao_peca": "Bateria - Galaxy S10", "quantidade": int64(1), "produto_preco": 50.0,
		},
		// mesma loja Alpha, mas o estoque aponta para o chat id em vez do id do usuario
		{
			"id": int64(3), "loja_id": "10", "fornecedor_id": "555", "fornecedor_nome": "Loja Alpha", "fornecedor_contato": alphaContact,
			"estoque_id": int64(101), "descricao_peca": "Bateria - iPhone 11", "quantidade": int64(1), "produto_preco": 29.9,
		},
		nil,
	}

	groups := groupOrdersBySupplier(rows)
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2 (uma por loja)", len(groups))
	}

	alpha := groups[0]
	if alpha["nome_loja"] != "Loja Alpha" || alpha["loja_id"] != "10" {
		t.Fatalf("grupo 0 = %#v, want Loja Alpha", alpha)
	}
	if !reflect.DeepEqual(alpha["pedido_ids"], []any{int64(1), int64(3)}) {
		t.Fatalf("pedido_ids Alpha = %#v, want [1 3]", alpha["pedido_ids"])
	}
	if alpha["total_pedidos"] != 2 {
		t.Fatalf("total_pedidos Alpha = %#v, want 2", alpha["total_pedidos"])
	}
	if alpha["valor_total"] != 230.9 { // 2*100.5 + 29.9
		t.Fatalf("valor_total Alpha = %#v, want 230.9", alpha["valor_total"])
	}
	items := alpha["itens"].([]map[string]any)
	if items[0]["descricao"] != "Tela - iPhone 11" || items[0]["subtotal"] != 201.0 || items[0]["sku"] != "SKU-1" {
		t.Fatalf("item 0 Alpha = %#v", items[0])
	}
	if !reflect.DeepEqual(alpha["contato"], alphaContact) {
		t.Fatalf("contato Alpha = %#v", alpha["contato"])
	}

	beta := groups[1]
	if beta["nome_loja"] != "Loja Beta" || beta["valor_total"] != 50.0 {
		t.Fatalf("grupo 1 = %#v, want Loja Beta com total 50", beta)
	}
}

func TestGroupOrdersBySupplierEdgeCases(t *testing.T) {
	if got := groupOrdersBySupplier(nil); len(got) != 0 {
		t.Fatalf("groupOrdersBySupplier(nil) = %#v, want vazio", got)
	}

	t.Run("sem loja_id usa fornecedor_id", func(t *testing.T) {
		groups := groupOrdersBySupplier([]map[string]any{
			{"id": int64(1), "fornecedor_id": "77", "quantidade": int64(1)},
			{"id": int64(2), "fornecedor_id": "77", "quantidade": int64(1)},
		})
		if len(groups) != 1 || groups[0]["loja_id"] != "77" {
			t.Fatalf("groups = %#v, want 1 grupo 77", groups)
		}
	})

	t.Run("item sem preco anula o total da loja", func(t *testing.T) {
		groups := groupOrdersBySupplier([]map[string]any{
			{"id": int64(1), "loja_id": "1", "quantidade": int64(1), "produto_preco": 10.0},
			{"id": int64(2), "loja_id": "1", "quantidade": int64(1), "produto_preco": nil},
			{"id": int64(3), "loja_id": "1", "quantidade": int64(1), "produto_preco": 5.0},
		})
		if groups[0]["valor_total"] != nil {
			t.Fatalf("valor_total = %#v, want nil", groups[0]["valor_total"])
		}
		items := groups[0]["itens"].([]map[string]any)
		if items[1]["preco_unitario"] != nil || items[1]["subtotal"] != nil {
			t.Fatalf("item sem preco = %#v", items[1])
		}
	})
}
