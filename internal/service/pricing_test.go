package service

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestFinalPrice(t *testing.T) {
	tests := []struct {
		name string
		base string
		rate string
		want string
	}{
		{name: "zero rate", base: "9.99", rate: "0", want: "9.99"},
		{name: "bitcoin at reference price doubles base", base: "10", rate: "50000", want: "20.00"},
		{name: "half reference price adds half", base: "20", rate: "25000", want: "30.00"},
		{name: "round midpoint to even", base: "1", rate: "250", want: "1.00"},
		{name: "round midpoint to next even cent", base: "1", rate: "750", want: "1.02"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FinalPrice(decimal.RequireFromString(tt.base), decimal.RequireFromString(tt.rate))
			if got.StringFixed(2) != tt.want {
				t.Fatalf("FinalPrice(%s, %s) = %s, want %s", tt.base, tt.rate, got.StringFixed(2), tt.want)
			}
		})
	}
}
