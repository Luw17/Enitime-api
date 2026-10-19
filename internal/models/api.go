package models

type Principal struct {
	ID             string
	Nome           string
	NomeLoja       *string
	PodeComprar    bool
	PodeVender     bool
	IsAdmin        bool
	TelegramChatID *string
}

type SearchFilters struct {
	TipoComponente   *string `json:"tipo_componente"`
	ModeloCompativel *string `json:"modelo_compativel"`
	CodigoPeca       *string `json:"codigo_peca"`
}

type CheckoutItem struct {
	ProdutoID  int64   `json:"produto_id"`
	Quantidade int64   `json:"quantidade"`
	Observacao *string `json:"observacao"`
}

type CheckoutRequest struct {
	TelegramChatID  string         `json:"telegram_chat_id"`
	ObservacaoGeral *string        `json:"observacao_geral"`
	Itens           []CheckoutItem `json:"itens"`
}
