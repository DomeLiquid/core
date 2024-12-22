package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type (
	MixinOracleStore interface {
		UpsertMixinOrder(ctx context.Context, order *SwapOrder) error
		GetMixinOrderByOrderId(ctx context.Context, orderId string) (*SwapOrder, error)
		GetLastestMixinOrders(ctx context.Context, offset time.Time) ([]*SwapOrder, error)
	}

	MarketAssetInfo struct {
		CoinID                       string          `json:"coin_id"`
		Name                         string          `json:"name"`
		Symbol                       string          `json:"symbol"`
		IconURL                      string          `json:"icon_url"`
		CurrentPrice                 decimal.Decimal `json:"current_price"`
		MarketCap                    decimal.Decimal `json:"market_cap"`
		MarketCapRank                string          `json:"market_cap_rank"`
		TotalVolume                  decimal.Decimal `json:"total_volume"`
		High24H                      decimal.Decimal `json:"high_24h"`
		Low24H                       decimal.Decimal `json:"low_24h"`
		PriceChange24H               decimal.Decimal `json:"price_change_24h"`
		PriceChangePercentage1H      decimal.Decimal `json:"price_change_percentage_1h"`
		PriceChangePercentage24H     decimal.Decimal `json:"price_change_percentage_24h"`
		PriceChangePercentage7D      decimal.Decimal `json:"price_change_percentage_7d"`
		PriceChangePercentage30D     decimal.Decimal `json:"price_change_percentage_30d"`
		MarketCapChange24H           decimal.Decimal `json:"market_cap_change_24h"`
		MarketCapChangePercentage24H decimal.Decimal `json:"market_cap_change_percentage_24h"`
		CirculatingSupply            decimal.Decimal `json:"circulating_supply"`
		TotalSupply                  decimal.Decimal `json:"total_supply"`
		MaxSupply                    decimal.Decimal `json:"max_supply"`
		Ath                          decimal.Decimal `json:"ath"`
		AthChangePercentage          decimal.Decimal `json:"ath_change_percentage"`
		AthDate                      time.Time       `json:"ath_date"`
		Atl                          decimal.Decimal `json:"atl"`
		AtlChangePercentage          decimal.Decimal `json:"atl_change_percentage"`
		AtlDate                      time.Time       `json:"atl_date"`
		AssetIDS                     []string        `json:"asset_ids"`
		SparklineIn7D                string          `json:"sparkline_in_7d"`
		SparklineIn24H               string          `json:"sparkline_in_24h"`
		UpdatedAt                    time.Time       `json:"updated_at"`
		Key                          string          `json:"key"`
	}

	HistoricalPrice struct {
		CoinID    string                 `json:"coin_id"`
		Type      string                 `json:"type"` // 1D, 1W, 1M, YTD, ALL
		Data      []HistoricalPriceDatum `json:"data"`
		UpdatedAt time.Time              `json:"updated_at"`
	}

	HistoricalPriceDatum struct {
		Price string `json:"price"`
		Unix  int64  `json:"unix"`
	}

	TokenView struct {
		AssetId string     `json:"assetId"`
		Name    string     `json:"name"`
		Symbol  string     `json:"symbol"`
		Icon    string     `json:"icon"`
		Chain   TokenChain `json:"chain"`
	}

	TokenChain struct {
		ChainId  string `json:"chainId"`
		Symbol   string `json:"symbol"`
		Name     string `json:"name"`
		Icon     string `json:"icon"`
		Decimals int    `json:"decimals"`
	}

	QuoteRequest struct {
		InputMint  string `json:"inputMint"`
		OutputMint string `json:"outputMint"`
		Amount     string `json:"amount"`
	}

	QuoteResponseView struct {
		InputMint  string `json:"inputMint"`
		InAmount   string `json:"inAmount"`
		OutputMint string `json:"outputMint"`
		OutAmount  string `json:"outAmount"`
		Payload    string `json:"payload"`
	}

	SwapRequest struct {
		Payer       string `json:"payer"`       // mixin user id
		InputMint   string `json:"inputMint"`   // mixin asset id
		InputAmount string `json:"inputAmount"` // mixin amount
		OutputMint  string `json:"outputMint"`  // mixin asset id
		Payload     string `json:"payload"`     // QuoteResponseView.Payload
		Referral    string `json:"referral"`    // optional
	}

	SwapResponseView struct {
		Tx    string            `json:"tx"` // mixin://mixin.one/pay/...
		Quote QuoteResponseView `json:"quote"`
	}

	SwapTx struct {
		Trace   string `json:"trace"`
		Payee   string `json:"payee"`
		Asset   string `json:"asset"`
		Amount  string `json:"amount"`
		Memo    string `json:"memo"`
		OrderId string `json:"orderId"`
	}

	SwapOrder struct {
		OrderId        string          `json:"order_id"`
		UserId         string          `json:"user_id"`
		AssetId        string          `json:"asset_id"`
		ReceiveAssetId string          `json:"receive_asset_id"`
		Amount         decimal.Decimal `json:"amount"`
		ReceiveAmount  decimal.Decimal `json:"receive_amount"`
		PaymentTraceId string          `json:"payment_trace_id"`
		ReceiveTraceId string          `json:"receive_trace_id"`
		State          SwapOrderState  `json:"state"`
		CreatedAt      time.Time       `json:"created_at"`
	}

	ErrorResponse struct {
		Error struct {
			Status      int    `json:"status"`
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}

	MixinOracleAPIError struct {
		StatusCode  int
		Code        int
		Description string
		RawBody     string
	}

	SwapOrderState string
)

const (
	SwapOrderStateCreated SwapOrderState = "created"
	SwapOrderStatePending SwapOrderState = "pending"
	SwapOrderStateSuccess SwapOrderState = "success"
	SwapOrderStateFailed  SwapOrderState = "failed"
)

func (e *MixinOracleAPIError) Error() string {
	return fmt.Sprintf("API error: status=%d, code=%d, description=%s",
		e.StatusCode, e.Code, e.Description)
}

func (q QuoteRequest) ToQuery() string {
	return fmt.Sprintf("inputMint=%s&outputMint=%s&amount=%s&source=mixin", q.InputMint, q.OutputMint, q.Amount)
}

func (s SwapResponseView) DecodeTx() (*SwapTx, error) {
	// mixin://mixin.one/pay/${uid}?asset=965e5c6e-434c-3fa9-b780-c50f43cd955c&amount=0.1&memo=test&trace=74518d17-e3df-46e5-a615-07793af27d5d
	tx, err := url.Parse(s.Tx)
	if err != nil {
		return nil, err
	}

	query, err := url.ParseQuery(tx.RawQuery)
	if err != nil {
		return nil, err
	}

	// mixin://mixin.one/pay/${uid}
	uid := strings.TrimPrefix(tx.Path, "/pay/")
	if uid == "" {
		return nil, fmt.Errorf("invalid uid in path: %s", tx.Path)
	}

	return &SwapTx{
		Trace:   query.Get("trace"),
		Payee:   uid,
		Asset:   query.Get("asset"),
		Amount:  query.Get("amount"),
		Memo:    query.Get("memo"),
		OrderId: query.Get("memo"),
	}, nil
}
