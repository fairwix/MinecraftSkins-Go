package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/fairwix/MinecraftSkins-Go/internal/service"
	"github.com/shopspring/decimal"
)

type fakeSkins struct {
	items  []service.CatalogSkin
	create domain.Skin
}

func (f fakeSkins) List(context.Context, domain.SkinFilter) ([]service.CatalogSkin, int, error) {
	return f.items, len(f.items), nil
}
func (f fakeSkins) Get(context.Context, string) (service.CatalogSkin, error) {
	return f.items[0], nil
}
func (f fakeSkins) Create(context.Context, string, decimal.Decimal, bool) (domain.Skin, error) {
	return f.create, nil
}
func (f fakeSkins) Update(context.Context, string, *string, *decimal.Decimal, *bool) (domain.Skin, error) {
	return f.create, nil
}
func (f fakeSkins) Delete(context.Context, string) (bool, error) { return true, nil }

type fakePurchases struct {
	item  domain.Purchase
	calls int
}

func (f *fakePurchases) Create(context.Context, string, string, string) (domain.Purchase, error) {
	f.calls++
	return f.item, nil
}
func (f *fakePurchases) Get(context.Context, string) (domain.Purchase, error) { return f.item, nil }
func (f *fakePurchases) List(context.Context, domain.PurchaseFilter) ([]domain.Purchase, error) {
	return []domain.Purchase{f.item}, nil
}

type fakeRates struct{}

func (fakeRates) Current(context.Context) (domain.Rate, error) {
	return domain.Rate{Rate: decimal.NewFromInt(50000), AsOfUTC: time.Now().UTC(), Source: "External"}, nil
}

type fakeDatabase struct{}

func (fakeDatabase) Ping(context.Context) error { return nil }

func TestListSkinsKeepsFrontendJSONContract(t *testing.T) {
	item := service.CatalogSkin{
		Skin: domain.Skin{
			ID: "11111111-1111-1111-1111-111111111111", Name: "Creeper Classic",
			BasePriceUSD: decimal.RequireFromString("9.99"), IsAvailable: true,
			CreatedAtUTC: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		FinalPriceUSD: decimal.RequireFromString("19.98"),
	}
	h := New(fakeSkins{items: []service.CatalogSkin{item}}, &fakePurchases{}, fakeRates{}, fakeDatabase{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/skins?availableOnly=true", nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("X-Total-Count") != "1" {
		t.Fatalf("X-Total-Count = %q, want 1", response.Header().Get("X-Total-Count"))
	}
	var body []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "name", "basePriceUsd", "finalPriceUsd", "isAvailable", "createdAtUtc"} {
		if _, ok := body[0][field]; !ok {
			t.Errorf("frontend field %q missing from response: %s", field, response.Body.String())
		}
	}
	if got, ok := body[0]["basePriceUsd"].(float64); !ok || got != 9.99 {
		t.Errorf("basePriceUsd = %#v, want numeric 9.99", body[0]["basePriceUsd"])
	}
	if got, ok := body[0]["finalPriceUsd"].(float64); !ok || got != 19.98 {
		t.Errorf("finalPriceUsd = %#v, want numeric 19.98", body[0]["finalPriceUsd"])
	}
}

func TestMineOnlyRequiresUserHeader(t *testing.T) {
	purchases := &fakePurchases{}
	h := New(fakeSkins{}, purchases, fakeRates{}, fakeDatabase{}, nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/purchases?mineOnly=true", nil))
	if response.Code != http.StatusBadRequest || purchases.calls != 0 {
		t.Fatalf("status=%d service calls=%d body=%s", response.Code, purchases.calls, response.Body.String())
	}
}

func TestCreatePurchaseKeepsFrontendResponseContract(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	purchase := domain.Purchase{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", SkinID: "11111111-1111-1111-1111-111111111111",
		PriceUSDFinal: decimal.RequireFromString("19.98"), BTCUSDRate: decimal.NewFromInt(50000),
		RateSource: "External", PurchasedAt: createdAt, BuyerID: "user123",
	}
	purchases := &fakePurchases{item: purchase}
	h := New(fakeSkins{}, purchases, fakeRates{}, fakeDatabase{}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/purchases", strings.NewReader(`{"skinId":"11111111-1111-1111-1111-111111111111"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-User-Id", "user123")
	request.Header.Set("Idempotency-Key", "retry-123")
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Location") != "/api/purchases/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "skinId", "finalPrice", "btcRate", "rateSource", "purchasedAt"} {
		if _, ok := body[field]; !ok {
			t.Errorf("field %q missing: %s", field, response.Body.String())
		}
	}
	if got, ok := body["finalPrice"].(float64); !ok || got != 19.98 {
		t.Errorf("finalPrice = %#v, want numeric 19.98", body["finalPrice"])
	}
	if got, ok := body["btcRate"].(float64); !ok || got != 50000 {
		t.Errorf("btcRate = %#v, want numeric 50000", body["btcRate"])
	}
	if purchases.calls != 1 {
		t.Fatalf("purchase service calls = %d, want 1", purchases.calls)
	}
}

func TestUnknownJSONFieldIsRejected(t *testing.T) {
	h := New(fakeSkins{}, &fakePurchases{}, fakeRates{}, fakeDatabase{}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/purchases", strings.NewReader(`{"skinId":"11111111-1111-1111-1111-111111111111","admin":true}`))
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}
