package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"enitime-api/internal/bot"
	"enitime-api/internal/config"
	"enitime-api/internal/middlewares"
)

type API struct {
	db          databaseExecutor
	config      config.Config
	telegramBot *bot.Service
}

type databaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func New(db databaseExecutor, cfg config.Config) http.Handler {
	api := &API{db: db, config: cfg, telegramBot: bot.NewService(db, cfg.TelegramBotToken)}
	mux := http.NewServeMux()

	mux.Handle("GET /pedidos", api.protected(api.getOrders))
	mux.Handle("GET /enitime/produtos", api.protected(api.getProducts))
	mux.Handle("GET /enitime/produtos/{id}", api.protected(api.getProduct))
	mux.Handle("POST /enitime/produtos/bulk", api.protected(api.bulkProducts))
	mux.Handle("POST /enitime/produtos", api.protected(api.createProduct))
	mux.Handle("POST /enitime/produtos/", api.protected(api.createProduct))
	mux.Handle("PATCH /enitime/produtos", api.protected(api.updateProduct))
	mux.Handle("PATCH /enitime/produtos/", api.protected(api.updateProduct))
	mux.Handle("POST /enitime/users", api.protected(api.createUser))
	mux.Handle("PATCH /enitime/users", api.protected(api.updateUser))
	mux.Handle("GET /enitime/users", api.protected(api.getUsers))
	mux.HandleFunc("POST /enitime/registrar", api.registerTelegramUser)
	mux.HandleFunc("POST /enitime/telegram/webhook", api.telegramWebhook)
	mux.HandleFunc("POST /enitime/login", api.login)
	mux.HandleFunc("POST /enitime/busca", api.searchProducts)
	mux.HandleFunc("POST /enitime/checkout-carrinho", api.checkout)
	mux.Handle("GET /enitime/pedidos-fornecedor", api.protected(api.getSupplierOrders))

	return JSONResponses(mux)
}

func (api *API) protected(handler http.HandlerFunc) http.Handler {
	return middlewares.RequireJWT(api.config.JWTSecret, handler)
}
