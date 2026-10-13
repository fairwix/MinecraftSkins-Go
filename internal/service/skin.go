package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/shopspring/decimal"
)

type SkinRepository interface {
	ListSkins(context.Context, domain.SkinFilter) ([]domain.Skin, int, error)
	GetSkin(context.Context, string) (domain.Skin, error)
	CreateSkin(context.Context, domain.Skin) (domain.Skin, error)
	UpdateSkin(context.Context, string, *string, *decimal.Decimal, *bool) (domain.Skin, error)
	SoftDeleteSkin(context.Context, string) (bool, error)
}

type RateProvider interface {
	Current(context.Context) (domain.Rate, error)
}

type CatalogSkin struct {
	domain.Skin
	FinalPriceUSD decimal.Decimal `json:"finalPriceUsd"`
}

type SkinService struct {
	repo  SkinRepository
	rates RateProvider
}

func NewSkinService(repo SkinRepository, rates RateProvider) *SkinService {
	return &SkinService{repo: repo, rates: rates}
}

func (s *SkinService) List(ctx context.Context, filter domain.SkinFilter) ([]CatalogSkin, int, error) {
	if err := validatePage(filter.Skip, filter.Take); err != nil {
		return nil, 0, err
	}
	skins, total, err := s.repo.ListSkins(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	rate, err := s.rates.Current(ctx)
	if err != nil {
		return nil, 0, err
	}
	result := make([]CatalogSkin, 0, len(skins))
	for _, skin := range skins {
		result = append(result, CatalogSkin{Skin: skin, FinalPriceUSD: FinalPrice(skin.BasePriceUSD, rate.Rate)})
	}
	return result, total, nil
}

func (s *SkinService) Get(ctx context.Context, id string) (CatalogSkin, error) {
	skin, err := s.repo.GetSkin(ctx, id)
	if err != nil {
		return CatalogSkin{}, err
	}
	rate, err := s.rates.Current(ctx)
	if err != nil {
		return CatalogSkin{}, err
	}
	return CatalogSkin{Skin: skin, FinalPriceUSD: FinalPrice(skin.BasePriceUSD, rate.Rate)}, nil
}

func (s *SkinService) Create(ctx context.Context, name string, price decimal.Decimal, available bool) (domain.Skin, error) {
	if err := validateSkin(name, &price); err != nil {
		return domain.Skin{}, err
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.Skin{}, fmt.Errorf("create skin id: %w", err)
	}
	now := time.Now().UTC()
	return s.repo.CreateSkin(ctx, domain.Skin{
		ID: id, Name: strings.TrimSpace(name), BasePriceUSD: price,
		IsAvailable: available, CreatedAtUTC: now,
	})
}

func (s *SkinService) Update(ctx context.Context, id string, name *string, price *decimal.Decimal, available *bool) (domain.Skin, error) {
	if name != nil || price != nil {
		if err := validateSkinPointer(name, price); err != nil {
			return domain.Skin{}, err
		}
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		name = &trimmed
	}
	return s.repo.UpdateSkin(ctx, id, name, price, available)
}

func (s *SkinService) Delete(ctx context.Context, id string) (bool, error) {
	return s.repo.SoftDeleteSkin(ctx, id)
}

func validateSkin(name string, price *decimal.Decimal) error {
	fields := make(map[string]string)
	if strings.TrimSpace(name) == "" {
		fields["name"] = "Имя скина обязательно"
	} else if len([]rune(strings.TrimSpace(name))) > 255 {
		fields["name"] = "Имя не должно превышать 255 символов"
	}
	if price == nil || !price.GreaterThan(decimal.Zero) {
		fields["basePriceUsd"] = "Цена должна быть больше нуля"
	} else if price.Exponent() < -2 || len(price.Coefficient().String()) > 18 {
		fields["basePriceUsd"] = "Цена должна соответствовать формату numeric(18,2)"
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func validateSkinPointer(name *string, price *decimal.Decimal) error {
	fields := make(map[string]string)
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			fields["name"] = "Имя скина не может быть пустым"
		} else if len([]rune(trimmed)) > 255 {
			fields["name"] = "Имя не должно превышать 255 символов"
		}
	}
	if price != nil {
		if !price.GreaterThan(decimal.Zero) {
			fields["basePriceUsd"] = "Цена должна быть больше нуля"
		} else if price.Exponent() < -2 || len(price.Coefficient().String()) > 18 {
			fields["basePriceUsd"] = "Цена должна соответствовать формату numeric(18,2)"
		}
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func validatePage(skip, take int) error {
	fields := make(map[string]string)
	if skip < 0 {
		fields["skip"] = "Значение skip не может быть отрицательным"
	}
	if take < 1 || take > 100 {
		fields["take"] = "Значение take должно быть от 1 до 100"
	}
	if len(fields) != 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}
