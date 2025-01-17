package currency

import (
	"context"
	"encoding/xml"
	"fmt"
	"github.com/applike/gosoline/pkg/cfg"
	"github.com/applike/gosoline/pkg/http"
	"github.com/applike/gosoline/pkg/kvstore"
	"github.com/applike/gosoline/pkg/mon"
	"time"
)

const (
	ExchangeRateRefresh       = 8 * time.Hour
	ExchangeRateUrl           = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
	HistoricalExchangeRateUrl = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist-90d.xml"
	ExchangeRateDateKey       = "currency_exchange_last_refresh"
)

const YMDLayout = "2006-01-02"

//go:generate mockery -name UpdaterService
type UpdaterService interface {
	EnsureRecentExchangeRates(ctx context.Context) error
	ImportHistoricalExchangeRates(ctx context.Context) error
}

type updaterService struct {
	logger mon.Logger
	http   http.Client
	store  kvstore.KvStore
}

func NewUpdater(config cfg.Config, logger mon.Logger) (UpdaterService, error) {
	logger = logger.WithChannel("currency_updater_service")

	store, err := kvstore.NewConfigurableKvStore(config, logger, "currency")
	if err != nil {
		return nil, fmt.Errorf("can not create kvStore: %w", err)
	}

	httpClient := http.NewHttpClient(config, logger)

	return NewUpdaterWithInterfaces(logger, store, httpClient), nil
}

func NewUpdaterWithInterfaces(logger mon.Logger, store kvstore.KvStore, httpClient http.Client) UpdaterService {
	return &updaterService{
		logger: logger,
		store:  store,
		http:   httpClient,
	}
}

func (s *updaterService) EnsureRecentExchangeRates(ctx context.Context) error {
	if !s.needsRefresh(ctx) {
		return nil
	}

	s.logger.Info("requesting exchange rates")
	rates, err := s.getCurrencyRates(ctx)

	if err != nil {
		return fmt.Errorf("error getting currency exchange rates: %w", err)
	}

	now := time.Now()
	for _, rate := range rates {
		err := s.store.Put(ctx, rate.Currency, rate.Rate)

		if err != nil {
			return fmt.Errorf("error setting exchange rate: %w", err)
		}

		s.logger.Infof("currency: %s, rate: %f", rate.Currency, rate.Rate)

		historicalRateKey := historicalRateKey(now, rate.Currency)
		err = s.store.Put(ctx, historicalRateKey, rate.Rate)
		if err != nil {
			return fmt.Errorf("error setting historical exchange rate, key: %s %w", historicalRateKey, err)
		}
	}

	newTime := time.Now()
	err = s.store.Put(ctx, ExchangeRateDateKey, newTime)

	if err != nil {
		return fmt.Errorf("error setting refresh date %w", err)
	}

	s.logger.Info("new exchange rates are set")
	return nil
}

func (s *updaterService) needsRefresh(ctx context.Context) bool {
	var date time.Time
	exists, err := s.store.Get(ctx, ExchangeRateDateKey, &date)

	if err != nil {
		s.logger.Info("error fetching date")

		return true
	}

	if !exists {
		s.logger.Info("date doesn't exist")

		return true
	}

	comparisonDate := time.Now().Add(-ExchangeRateRefresh)

	if date.Before(comparisonDate) {
		s.logger.Info("comparison date was more than 8 hours ago")

		return true
	}

	return false
}

func (s *updaterService) getCurrencyRates(ctx context.Context) ([]Rate, error) {
	request := s.http.NewRequest().WithUrl(ExchangeRateUrl)

	response, err := s.http.Get(ctx, request)

	if err != nil {
		return nil, fmt.Errorf("error requesting exchange rates: %w", err)
	}

	exchangeRateResult := ExchangeResponse{}
	err = xml.Unmarshal(response.Body, &exchangeRateResult)

	if err != nil {
		return nil, fmt.Errorf("error unmarshalling exchange rates: %w", err)
	}

	return exchangeRateResult.Body.Content.Rates, nil
}

func (s *updaterService) ImportHistoricalExchangeRates(ctx context.Context) error {
	s.logger.Info("requesting historical exchange rates")
	rates, err := s.getCurrencyRatesForLast3Months(ctx)

	if err != nil {
		return fmt.Errorf("error getting historical currency exchange rates: %w", err)
	}

	// the API doesn't return rates for weekends and public holidays at the time of writing this,
	// so we fill in the missing values using values from previously available days
	rates, err = fillInGapDays(rates)
	if err != nil {
		return fmt.Errorf("error filling in gaps: %w", err)
	}

	keyValues := make(map[string]float64)
	for _, dayRates := range rates {
		date, err := dayRates.GetTime()
		if err != nil {
			return fmt.Errorf("error parsing time in historical exchange rates: %w", err)
		}

		for _, rate := range dayRates.Rates {
			key := historicalRateKey(date, rate.Currency)
			keyValues[key] = rate.Rate
		}
	}

	err = s.store.PutBatch(ctx, keyValues)
	if err != nil {
		return fmt.Errorf("error setting historical exchange rates: %w", err)
	}

	s.logger.Infof("stored %d days of historical exchange rates", len(rates))
	return nil
}

func (s *updaterService) getCurrencyRatesForLast3Months(ctx context.Context) ([]Content, error) {
	request := s.http.NewRequest().WithUrl(HistoricalExchangeRateUrl)

	response, err := s.http.Get(ctx, request)

	if err != nil {
		return nil, fmt.Errorf("error requesting historical exchange rates: %w", err)
	}

	exchangeRateResult := HistoricalExchangeResponse{}
	err = xml.Unmarshal(response.Body, &exchangeRateResult)

	if err != nil {
		return nil, fmt.Errorf("error unmarshalling historical exchange rates: %w", err)
	}

	return exchangeRateResult.Body.Content, nil
}

func historicalRateKey(time time.Time, currency string) string {
	return time.Format("2006-01-02") + "-" + currency
}

func fillInGapDays(historicalContent []Content) ([]Content, error) {
	var startDate time.Time
	var endDate time.Time
	var dailyRates = make(map[string]Content)

	for _, dayRates := range historicalContent {
		date, err := dayRates.GetTime()
		if err != nil {
			return nil, fmt.Errorf("fillInGapDays error: %w", err)
		}
		if startDate.IsZero() || startDate.After(date) {
			startDate = date
		}
		if endDate.IsZero() || endDate.Before(date) {
			endDate = date
		}
		dailyRates[date.Format(YMDLayout)] = dayRates
	}

	var date = startDate
	var lastDay = date
	counter := 0
	for {
		counter++
		if counter > 180 {
			break
		}

		if date.Equal(endDate) || date.After(endDate) {
			return historicalContent, nil
		}
		if _, ok := dailyRates[date.Format(YMDLayout)]; !ok {
			gapContent := dailyRates[lastDay.Format(YMDLayout)]
			gapContent.Time = date.Format(YMDLayout)
			historicalContent = append(historicalContent, gapContent)
		} else {
			lastDay = date
		}

		date = date.AddDate(0, 0, 1)
	}

	return historicalContent, nil
}
