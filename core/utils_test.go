package core

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCalcValue(t *testing.T) {
	tests := []struct {
		name     string
		amount   decimal.Decimal
		price    decimal.Decimal
		weight   *decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name:     "normal",
			amount:   decimal.NewFromFloat(100),
			price:    decimal.NewFromFloat(2),
			weight:   decimalPtr(decimal.NewFromFloat(0.5)),
			expected: decimal.NewFromFloat(100),
		},
		{
			name:     "zero",
			amount:   decimal.Zero,
			price:    decimal.NewFromFloat(2),
			weight:   decimalPtr(decimal.NewFromFloat(0.5)),
			expected: decimal.Zero,
		},
		{
			name:     "nil",
			amount:   decimal.NewFromFloat(100),
			price:    decimal.NewFromFloat(2),
			weight:   nil,
			expected: decimal.NewFromFloat(200),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalcValue(tt.amount, tt.price, tt.weight)
			assert.NoError(t, err)
			assert.True(t, result.Equal(tt.expected), "期望 %s，得到 %s", tt.expected, result)
		})
	}
}

func TestCalcAmount(t *testing.T) {
	tests := []struct {
		name     string
		value    decimal.Decimal
		price    decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name:     "normal",
			value:    decimal.NewFromFloat(200),
			price:    decimal.NewFromFloat(2),
			expected: decimal.NewFromFloat(100),
		},
		{
			name:     "zero price",
			value:    decimal.NewFromFloat(200),
			price:    decimal.Zero,
			expected: decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalcAmount(tt.value, tt.price)
			if tt.price.IsZero() {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.True(t, result.Equal(tt.expected), "expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCalculatePreFeeSplDepositAmount(t *testing.T) {
	amount := decimal.NewFromFloat(100)
	result, err := CalculatePreFeeSplDepositAmount(amount)
	assert.NoError(t, err)
	assert.True(t, result.Equal(amount), "expected %s, got %s", amount, result)
}

func TestCalculatePostFeeSplDepositAmount(t *testing.T) {
	amount := decimal.NewFromFloat(100)
	result, err := CalculatePostFeeSplDepositAmount(amount)
	assert.NoError(t, err)
	assert.True(t, result.Equal(amount), "expected %s, got %s", amount, result)
}

func decimalPtr(d decimal.Decimal) *decimal.Decimal {
	return &d
}
