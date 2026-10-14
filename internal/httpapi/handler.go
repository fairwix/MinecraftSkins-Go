package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/fairwix/MinecraftSkins-Go/internal/service"
	"github.com/shopspring/decimal"
)

type skinService interface {
	List(context.Context, domain.SkinFilter) ([]service.CatalogSkin, int, error)
	Get(context.Context, string) (service.CatalogSkin, error)
	Create(context.Context, string, decimal.Decimal, bool) (domain.Skin, error)
	Update(context.Context, string, *string, *decimal.Decimal, *bool) (domain.Skin, error)
	Delete(context.Context, string) (bool, error)
}

type purchaseService interface {
	Create(context.Context, string, string, string) (domain.Purchase, error)
	Get(context.Context, string) (domain.Purchase, error)
	List(context.Context, domain.PurchaseFilter) ([]domain.Purchase, error)
}

type rateService interface {
	Current(context.Context) (domain.Rate, error)
}

type databaseHealth interface {
	Ping(context.Context) error
}

// jsonDecimal keeps decimal values numeric in JSON so existing JavaScript
// clients can use Number methods such as toFixed and toLocaleString.
type jsonDecimal decimal.Decimal

func (d jsonDecimal) MarshalJSON() ([]byte, error) {
	return []byte(decimal.Decimal(d).String()), nil
}

type skinResponse struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	BasePriceUSD  jsonDecimal `json:"basePriceUsd"`
	FinalPriceUSD jsonDecimal `json:"finalPriceUsd"`
	IsAvailable   bool        `json:"isAvailable"`
	CreatedAtUTC  time.Time   `json:"createdAtUtc"`
	UpdatedAtUTC  *time.Time  `json:"updatedAtUtc"`
}

func toSkinResponse(skin domain.Skin, finalPrice decimal.Decimal) skinResponse {
	return skinResponse{
		ID: skin.ID, Name: skin.Name, BasePriceUSD: jsonDecimal(skin.BasePriceUSD),
		FinalPriceUSD: jsonDecimal(finalPrice), IsAvailable: skin.IsAvailable,
		CreatedAtUTC: skin.CreatedAtUTC, UpdatedAtUTC: skin.UpdatedAtUTC,
	}
}

type purchaseResponse struct {
	ID            string      `json:"id"`
	SkinID        string      `json:"skinId"`
	SkinName      string      `json:"skinName"`
	PriceUSDFinal jsonDecimal `json:"priceUsdFinal"`
	BTCUSDRate    jsonDecimal `json:"btcUsdRate"`
	RateSource    string      `json:"rateSource"`
	PurchasedAt   time.Time   `json:"purchasedAtUtc"`
	BuyerID       string      `json:"buyerId"`
}

func toPurchaseResponse(purchase domain.Purchase) purchaseResponse {
	return purchaseResponse{
		ID: purchase.ID, SkinID: purchase.SkinID, SkinName: purchase.SkinName,
		PriceUSDFinal: jsonDecimal(purchase.PriceUSDFinal), BTCUSDRate: jsonDecimal(purchase.BTCUSDRate),
		RateSource: purchase.RateSource, PurchasedAt: purchase.PurchasedAt, BuyerID: purchase.BuyerID,
	}
}

type rateResponse struct {
	Rate       jsonDecimal `json:"rate"`
	AsOfUTC    time.Time   `json:"asOfUtc"`
	Source     string      `json:"source"`
	AgeSeconds *int        `json:"ageSeconds"`
}

type Handler struct {
	skins     skinService
	purchases purchaseService
	rates     rateService
	database  databaseHealth
	logger    *slog.Logger
	instance  string
	auth      AuthConfig
}

type AuthConfig struct {
	AdminAPIKey    string
	DemoUserAPIKey string
	DemoUserID     string
}

type principal struct {
	userID string
	role   string
}

type principalContextKey struct{}

func New(skins skinService, purchases purchaseService, rates rateService, database databaseHealth, logger *slog.Logger, auth AuthConfig) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		skins: skins, purchases: purchases, rates: rates, database: database,
		logger: logger, instance: env("INSTANCE_ID", "unknown"), auth: auth,
	}
}

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skins", h.listSkins)
	mux.HandleFunc("GET /api/skins/{id}", h.getSkin)
	mux.Handle("POST /api/skins", h.authorize(http.HandlerFunc(h.createSkin), "admin"))
	mux.Handle("PUT /api/skins/{id}", h.authorize(http.HandlerFunc(h.updateSkin), "admin"))
	mux.Handle("DELETE /api/skins/{id}", h.authorize(http.HandlerFunc(h.deleteSkin), "admin"))
	mux.HandleFunc("GET /api/rates/btc-usd", h.getRate)
	mux.Handle("POST /api/purchases", h.authorize(http.HandlerFunc(h.createPurchase), "user", "admin"))
	mux.Handle("GET /api/purchases", h.authorize(http.HandlerFunc(h.listPurchases), "user", "admin"))
	mux.Handle("GET /api/purchases/{id}", h.authorize(http.HandlerFunc(h.getPurchase), "user", "admin"))
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/health/ping", h.ping)
	mux.HandleFunc("GET /api/health/detailed", h.health)
	return h.middleware(mux)
}

func (h *Handler) authorize(next http.Handler, allowedRoles ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if len(authorization) < len(prefix) || !strings.EqualFold(authorization[:len(prefix)], prefix) {
			writeProblem(w, r, domain.ErrUnauthenticated)
			return
		}
		caller, ok := h.authenticate(strings.TrimSpace(authorization[len(prefix):]))
		if !ok {
			writeProblem(w, r, domain.ErrUnauthenticated)
			return
		}
		for _, role := range allowedRoles {
			if caller.role == role {
				ctx := context.WithValue(r.Context(), principalContextKey{}, caller)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		writeProblem(w, r, domain.ErrForbidden)
	})
}

func (h *Handler) authenticate(token string) (principal, bool) {
	if token == "" {
		return principal{}, false
	}
	if h.auth.AdminAPIKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.auth.AdminAPIKey)) == 1 {
		return principal{userID: "admin", role: "admin"}, true
	}
	if h.auth.DemoUserAPIKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.auth.DemoUserAPIKey)) == 1 {
		return principal{userID: h.auth.DemoUserID, role: "user"}, true
	}
	return principal{}, false
}

func currentPrincipal(ctx context.Context) principal {
	caller, _ := ctx.Value(principalContextKey{}).(principal)
	return caller
}

func (h *Handler) listSkins(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	available, err := parseBool(query.Get("availableOnly"), false)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"availableOnly": "Ожидалось true или false"}})
		return
	}
	skip, err := parseInt(query.Get("skip"), 0)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"skip": "Ожидалось целое число"}})
		return
	}
	take, err := parseInt(query.Get("take"), 20)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"take": "Ожидалось целое число"}})
		return
	}
	items, total, err := h.skins.List(r.Context(), domain.SkinFilter{
		AvailableOnly: available, Search: query.Get("search"), Skip: skip, Take: take,
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	response := make([]skinResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toSkinResponse(item.Skin, item.FinalPriceUSD))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) getSkin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"id": "Некорректный UUID"}})
		return
	}
	skin, err := h.skins.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSkinResponse(skin.Skin, skin.FinalPriceUSD))
}

func (h *Handler) createSkin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name         string          `json:"name"`
		BasePriceUSD decimal.Decimal `json:"basePriceUsd"`
		IsAvailable  bool            `json:"isAvailable"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	skin, err := h.skins.Create(r.Context(), request.Name, request.BasePriceUSD, request.IsAvailable)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/skins/"+skin.ID)
	writeJSON(w, http.StatusCreated, toSkinResponse(skin, decimal.Zero))
}

func (h *Handler) updateSkin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"id": "Некорректный UUID"}})
		return
	}
	var request struct {
		Name         *string          `json:"name"`
		BasePriceUSD *decimal.Decimal `json:"basePriceUsd"`
		IsAvailable  *bool            `json:"isAvailable"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	skin, err := h.skins.Update(r.Context(), id, request.Name, request.BasePriceUSD, request.IsAvailable)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSkinResponse(skin, decimal.Zero))
}

func (h *Handler) deleteSkin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"id": "Некорректный UUID"}})
		return
	}
	deleted, err := h.skins.Delete(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if !deleted {
		writeProblem(w, r, domain.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getRate(w http.ResponseWriter, r *http.Request) {
	rate, err := h.rates.Current(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rateResponse{
		Rate: jsonDecimal(rate.Rate), AsOfUTC: rate.AsOfUTC, Source: rate.Source, AgeSeconds: rate.AgeSeconds,
	})
}

func (h *Handler) createPurchase(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SkinID string `json:"skinId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if !validID(request.SkinID) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"skinId": "Некорректный UUID"}})
		return
	}
	caller := currentPrincipal(r.Context())
	purchase, err := h.purchases.Create(r.Context(), request.SkinID, caller.userID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/purchases/"+purchase.ID)
	writeJSON(w, http.StatusCreated, struct {
		ID          string      `json:"id"`
		SkinID      string      `json:"skinId"`
		FinalPrice  jsonDecimal `json:"finalPrice"`
		BTCRate     jsonDecimal `json:"btcRate"`
		RateSource  string      `json:"rateSource"`
		PurchasedAt time.Time   `json:"purchasedAt"`
	}{purchase.ID, purchase.SkinID, jsonDecimal(purchase.PriceUSDFinal), jsonDecimal(purchase.BTCUSDRate), purchase.RateSource, purchase.PurchasedAt})
}

func (h *Handler) listPurchases(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	mineOnly, err := parseBool(query.Get("mineOnly"), false)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"mineOnly": "Ожидалось true или false"}})
		return
	}
	filter := domain.PurchaseFilter{}
	caller := currentPrincipal(r.Context())
	if caller.role != "admin" || mineOnly {
		filter.BuyerID = caller.userID
	}
	filter.SkinID = query.Get("skinId")
	if filter.SkinID != "" && !validID(filter.SkinID) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"skinId": "Некорректный UUID"}})
		return
	}
	filter.From, err = parseTime(query.Get("from"))
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"from": "Ожидалась дата ISO 8601 с часовым поясом"}})
		return
	}
	filter.To, err = parseTime(query.Get("to"))
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"to": "Ожидалась дата ISO 8601 с часовым поясом"}})
		return
	}
	filter.Skip, err = parseInt(query.Get("skip"), 0)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"skip": "Ожидалось целое число"}})
		return
	}
	filter.Take, err = parseInt(query.Get("take"), 20)
	if err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"take": "Ожидалось целое число"}})
		return
	}
	purchases, err := h.purchases.List(r.Context(), filter)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	response := make([]purchaseResponse, 0, len(purchases))
	for _, purchase := range purchases {
		response = append(response, toPurchaseResponse(purchase))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) getPurchase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"id": "Некорректный UUID"}})
		return
	}
	purchase, err := h.purchases.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	caller := currentPrincipal(r.Context())
	if caller.role != "admin" && purchase.BuyerID != caller.userID {
		writeProblem(w, r, domain.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toPurchaseResponse(purchase))
}

func (h *Handler) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "timestamp": time.Now().UTC()})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	components := map[string]map[string]string{}
	status := "healthy"
	if err := h.database.Ping(ctx); err != nil {
		status = "unhealthy"
		components["database"] = map[string]string{"status": "unhealthy", "description": "Database is unavailable"}
	} else {
		components["database"] = map[string]string{"status": "healthy", "description": "Database is available"}
	}
	if _, err := h.rates.Current(ctx); err != nil {
		if status == "healthy" {
			status = "degraded"
		}
		components["btcRate"] = map[string]string{"status": "degraded", "description": "BTC rate service is unavailable"}
	} else {
		components["btcRate"] = map[string]string{"status": "healthy", "description": "BTC rate service is available"}
	}
	code := http.StatusOK
	if status != "healthy" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status": status, "totalDuration": time.Since(started).String(),
		"timestamp": time.Now().UTC(), "components": components,
	})
}

func (h *Handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", h.instance)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Error("panic while handling request", "path", r.URL.Path, "panic", fmt.Sprint(recovered))
				writeProblem(w, r, errors.New("internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"body": "Некорректный JSON: " + err.Error()}})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeProblem(w, r, &domain.ValidationError{Fields: map[string]string{"body": "Тело должно содержать один JSON-объект"}})
		return false
	}
	return true
}

func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		return
	}
	status := http.StatusInternalServerError
	title, detail := "Внутренняя ошибка сервера", "Произошла непредвиденная ошибка"
	problem := map[string]any{
		"type": "about:blank", "status": status, "title": title,
		"detail": detail, "instance": r.URL.Path,
	}
	var validation *domain.ValidationError
	var unavailable *domain.SkinUnavailableError
	var external *domain.ExternalServiceError
	switch {
	case errors.As(err, &validation):
		status, title, detail = http.StatusBadRequest, "Ошибка проверки данных", validation.Error()
		problem["errors"] = validation.Fields
	case errors.Is(err, domain.ErrNotFound):
		status, title, detail = http.StatusNotFound, "Не найдено", "Запрошенный ресурс не найден"
	case errors.As(err, &unavailable):
		status, title, detail = http.StatusConflict, "Скин недоступен", unavailable.Reason
		problem["extensions"] = map[string]string{"skinId": unavailable.SkinID, "reason": unavailable.Reason}
	case errors.Is(err, domain.ErrExternalService):
		status, title, detail = http.StatusServiceUnavailable, "Внешний сервис недоступен", "Курс BTC/USD временно недоступен"
		if errors.As(err, &external) {
			problem["service"] = external.Service
		}
	case errors.Is(err, domain.ErrUnauthenticated):
		status, title, detail = http.StatusUnauthorized, "Требуется аутентификация", "Передайте действительный Bearer API key"
	case errors.Is(err, domain.ErrForbidden):
		status, title, detail = http.StatusForbidden, "Недостаточно прав", "У этой учётной записи нет доступа к операции"
	case errors.Is(err, domain.ErrIdempotencyConflict):
		status, title, detail = http.StatusConflict, "Конфликт ключа идемпотентности", "Этот ключ уже использован для другого запроса"
	case errors.Is(err, context.Canceled):
		return
	default:
		if err != nil {
			hlog := slog.Default()
			hlog.Error("request failed", "path", r.URL.Path, "error", err)
		}
	}
	problem["status"], problem["title"], problem["detail"] = status, title, detail
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func parseBool(value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseBool(value)
}

func parseInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func parseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func validID(value string) bool { return idPattern.MatchString(value) }

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
