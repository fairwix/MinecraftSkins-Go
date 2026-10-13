package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/fairwix/MinecraftSkins-Go/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func TestPostgresPurchaseIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated PostgreSQL database to run integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewRepository(pool)

	skinID, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	userID := "integration-" + skinID
	skin, err := repo.CreateSkin(ctx, domain.Skin{
		ID: skinID, Name: "Integration test skin", BasePriceUSD: decimal.NewFromInt(10),
		IsAvailable: true, CreatedAtUTC: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM purchases WHERE buyer_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM skins WHERE id = $1`, skin.ID)
	})

	key := "test-" + skinID
	first, err := repo.CreatePurchase(ctx, skin.ID, userID, domain.Rate{
		Rate: decimal.NewFromInt(50000), AsOfUTC: time.Now().UTC(), Source: "External",
	}, &key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreatePurchase(ctx, skin.ID, userID, domain.Rate{
		Rate: decimal.NewFromInt(100000), AsOfUTC: time.Now().UTC(), Source: "External",
	}, &key)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.PriceUSDFinal.StringFixed(2) != second.PriceUSDFinal.StringFixed(2) {
		t.Fatalf("retry changed the purchase: first=%+v second=%+v", first, second)
	}
}
