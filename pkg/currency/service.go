package currency

import (
	"context"
	"fmt"
	"github.com/applike/gosoline/pkg/cfg"
	"github.com/applike/gosoline/pkg/kvstore"
	"github.com/applike/gosoline/pkg/mon"
	"time"
)

//go:generate mockery -name Service
type Service interface {
	HasCurrency(ctx context.Context, currency string) (bool, error)
	ToEur(ctx context.Context, value float64, from string) (float64, error)
	ToUsd(ctx context.Context, value float64, from string) (float64, error)
	ToCurrency(ctx context.Context, to string, value float64, from string) (float64, error)

	HasCurrencyAtDate(ctx context.Context, currency string, date time.Time) (bool, error)
	ToEurAtDate(ctx context.Context, value float64, from string, date time.Time) (float64, error)
	ToUsdAtDate(ctx context.Context, value float64, from string, date time.Time) (float64, error)
	ToCurrencyAtDate(ctx context.Context, to string, value float64, from string, date time.Time) (float64, error)
}

type currencyService struct {
	store kvstore.KvStore
}

func New(config cfg.Config, logger mon.Logger) (*currencyService, error) {
	store, err := kvstore.NewConfigurableKvStore(config, logger, "currency")
	if err != nil {
		return nil, fmt.Errorf("can not create kvStore: %w", err)
	}

	return NewWithInterfaces(store), nil
}

func NewWithInterfaces(store kvstore.KvStore) *currencyService {
	return &currencyService{
		store: store,
	}
}

// returns whether we support converting a given currency or not and whether an error occurred or not
func (s *currencyService) HasCurrency(ctx context.Context, currency string) (bool, error) {
	if currency == "EUR" {
		return true, nil
	}

	return s.store.Contains(ctx, currency)
}

// returns the euro value for a given value and currency and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToEur(ctx context.Context, value float64, from string) (float64, error) {
	if from == Eur {
		return value, nil
	}

	exchangeRate, err := s.getExchangeRate(ctx, from)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error parsing exchange rate: %w", err)
	}

	return value / exchangeRate, nil
}

// returns the us dollar value for a given value and currency and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToUsd(ctx context.Context, value float64, from string) (float64, error) {
	if from == Usd {
		return value, nil
	}

	return s.ToCurrency(ctx, Usd, value, from)
}

// returns the value in the currency given in the to parameter for a given value and currency given in the from parameter and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToCurrency(ctx context.Context, to string, value float64, from string) (float64, error) {
	if from == to {
		return value, nil
	}

	exchangeRate, err := s.getExchangeRate(ctx, to)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error parsing exchange rate: %w", err)
	}

	eur, err := s.ToEur(ctx, value, from)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error converting to eur: %w", err)
	}

	return eur * exchangeRate, nil
}

func (s *currencyService) getExchangeRate(ctx context.Context, to string) (float64, error) {
	var exchangeRate float64
	exists, err := s.store.Get(ctx, to, &exchangeRate)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error getting exchange rate: %w", err)
	} else if !exists {
		return 0, fmt.Errorf("CurrencyService: currency not found: %w", err)
	}

	return exchangeRate, nil
}

// returns whether we support converting a given currency at the given time or not and whether an error occurred or not
func (s *currencyService) HasCurrencyAtDate(ctx context.Context, currency string, date time.Time) (bool, error) {
	if currency == "EUR" {
		return true, nil
	}

	key := historicalRateKey(date, currency)
	return s.store.Contains(ctx, key)
}

// returns the euro value for a given value and currency at the given time and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToEurAtDate(ctx context.Context, value float64, from string, date time.Time) (float64, error) {
	if from == Eur {
		return value, nil
	}

	key := historicalRateKey(date, from)
	exchangeRate, err := s.getExchangeRate(ctx, key)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error parsing exchange rate historically: %w", err)
	}

	return value / exchangeRate, nil
}

// returns the us dollar value for a given value and currency at the given time and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToUsdAtDate(ctx context.Context, value float64, from string, date time.Time) (float64, error) {
	if from == Usd {
		return value, nil
	}

	return s.ToCurrencyAtDate(ctx, Usd, value, from, date)
}

// returns the value in the currency given in the to parameter for a given value and currency given in the from parameter and nil if not error occurred. returns 0 and an error object otherwise.
func (s *currencyService) ToCurrencyAtDate(ctx context.Context, to string, value float64, from string, date time.Time) (float64, error) {
	if from == to {
		return value, nil
	}

	key := historicalRateKey(date, to)
	exchangeRate, err := s.getExchangeRate(ctx, key)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error parsing exchange rate historically: %w", err)
	}

	eur, err := s.ToEurAtDate(ctx, value, from, date)

	if err != nil {
		return 0, fmt.Errorf("CurrencyService: error converting to eur historically: %w", err)
	}

	return eur * exchangeRate, nil
}
