package service

import (
	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/shopspring/decimal"
)

// FinalPrice applies the catalog's BTC-linked multiplier and rounds cents to even,
// matching decimal rounding used by the original C# implementation.
func FinalPrice(basePriceUSD, btcUSD decimal.Decimal) decimal.Decimal {
	return domain.FinalPrice(basePriceUSD, btcUSD)
}
