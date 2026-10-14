package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
)

type idempotencyRepository struct {
	existing *domain.Purchase
}

func (r idempotencyRepository) GetPurchaseByIdempotencyKey(context.Context, string, string) (*domain.Purchase, error) {
	return r.existing, nil
}

func (idempotencyRepository) CreatePurchase(context.Context, string, string, domain.Rate, *string) (domain.Purchase, error) {
	return domain.Purchase{}, errors.New("CreatePurchase must not be called for a mismatched retry")
}

func (idempotencyRepository) GetPurchase(context.Context, string) (domain.Purchase, error) {
	return domain.Purchase{}, nil
}

func (idempotencyRepository) ListPurchases(context.Context, domain.PurchaseFilter) ([]domain.Purchase, error) {
	return nil, nil
}

type countingRateProvider struct{ calls int }

func (p *countingRateProvider) Current(context.Context) (domain.Rate, error) {
	p.calls++
	return domain.Rate{}, nil
}

func TestCreateRejectsIdempotencyKeyReusedForAnotherSkin(t *testing.T) {
	provider := &countingRateProvider{}
	repo := idempotencyRepository{existing: &domain.Purchase{SkinID: "skin-a"}}
	service := NewPurchaseService(repo, provider)

	_, err := service.Create(context.Background(), "skin-b", "buyer", "same-key")
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("Create() error = %v, want ErrIdempotencyConflict", err)
	}
	if provider.calls != 0 {
		t.Fatalf("rate provider calls = %d, want 0 for a conflicting retry", provider.calls)
	}
}
