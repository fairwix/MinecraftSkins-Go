package service

import (
	"context"
	"strings"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
)

type PurchaseRepository interface {
	GetPurchaseByIdempotencyKey(context.Context, string, string) (*domain.Purchase, error)
	CreatePurchase(context.Context, string, string, domain.Rate, *string) (domain.Purchase, error)
	GetPurchase(context.Context, string) (domain.Purchase, error)
	ListPurchases(context.Context, domain.PurchaseFilter) ([]domain.Purchase, error)
}

type PurchaseService struct {
	repo  PurchaseRepository
	rates RateProvider
}

func NewPurchaseService(repo PurchaseRepository, rates RateProvider) *PurchaseService {
	return &PurchaseService{repo: repo, rates: rates}
}

func (s *PurchaseService) Create(ctx context.Context, skinID, buyerID, idempotencyKey string) (domain.Purchase, error) {
	buyerID = strings.TrimSpace(buyerID)
	if buyerID == "" || len(buyerID) > 100 {
		return domain.Purchase{}, &domain.ValidationError{Fields: map[string]string{"X-User-Id": "Заголовок X-User-Id обязателен и не должен превышать 100 символов"}}
	}
	if len(idempotencyKey) > 100 {
		return domain.Purchase{}, &domain.ValidationError{Fields: map[string]string{"Idempotency-Key": "Ключ не должен превышать 100 символов"}}
	}
	var key *string
	if idempotencyKey != "" {
		key = &idempotencyKey
		// A retry returns the original receipt even if CoinGecko is currently unavailable.
		existing, err := s.repo.GetPurchaseByIdempotencyKey(ctx, buyerID, idempotencyKey)
		if err != nil {
			return domain.Purchase{}, err
		}
		if existing != nil {
			return *existing, nil
		}
	}

	rate, err := s.rates.Current(ctx)
	if err != nil {
		return domain.Purchase{}, err
	}
	return s.repo.CreatePurchase(ctx, skinID, buyerID, rate, key)
}

func (s *PurchaseService) Get(ctx context.Context, id string) (domain.Purchase, error) {
	return s.repo.GetPurchase(ctx, id)
}

func (s *PurchaseService) List(ctx context.Context, filter domain.PurchaseFilter) ([]domain.Purchase, error) {
	if err := validatePage(filter.Skip, filter.Take); err != nil {
		return nil, err
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return nil, &domain.ValidationError{Fields: map[string]string{"from": "Дата from должна быть раньше to"}}
	}
	return s.repo.ListPurchases(ctx, filter)
}
