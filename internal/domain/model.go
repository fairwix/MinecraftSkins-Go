package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

type Skin struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	BasePriceUSD decimal.Decimal `json:"basePriceUsd"`
	IsAvailable  bool            `json:"isAvailable"`
	CreatedAtUTC time.Time       `json:"createdAtUtc"`
	UpdatedAtUTC *time.Time      `json:"updatedAtUtc"`
}

type Purchase struct {
	ID             string          `json:"id"`
	SkinID         string          `json:"skinId"`
	SkinName       string          `json:"skinName"`
	PriceUSDFinal  decimal.Decimal `json:"priceUsdFinal"`
	BTCUSDRate     decimal.Decimal `json:"btcUsdRate"`
	RateSource     string          `json:"rateSource"`
	PurchasedAt    time.Time       `json:"purchasedAtUtc"`
	BuyerID        string          `json:"buyerId"`
	IdempotencyKey *string         `json:"-"`
}

type Rate struct {
	Rate       decimal.Decimal `json:"rate"`
	AsOfUTC    time.Time       `json:"asOfUtc"`
	Source     string          `json:"source"`
	AgeSeconds *int            `json:"ageSeconds"`
}

type SkinFilter struct {
	AvailableOnly bool
	Search        string
	Skip          int
	Take          int
}

type PurchaseFilter struct {
	BuyerID string
	SkinID  string
	From    *time.Time
	To      *time.Time
	Skip    int
	Take    int
}

func FinalPrice(basePriceUSD, btcUSD decimal.Decimal) decimal.Decimal {
	multiplier := decimal.NewFromInt(1).Add(btcUSD.Div(decimal.NewFromInt(50_000)))
	return basePriceUSD.Mul(multiplier).RoundBank(2)
}
