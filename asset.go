package core

import (
	"context"

	"github.com/fox-one/mixin-sdk-go/v2"
	"github.com/shopspring/decimal"
)

type (
	MixinSafeAssetStore interface {
		GetAsset(ctx context.Context, assetId string) (*MixinSafeAsset, error)
		ListAllAssets(ctx context.Context) ([]*MixinSafeAsset, error)
		UpsertAsset(ctx context.Context, asset *MixinSafeAsset) error
	}

	MixinSafeAsset struct {
		AssetID       string          `json:"assetId,omitempty"`
		ChainID       string          `json:"chainId,omitempty"`
		KernelAssetID string          `json:"kernelAssetId,omitempty"`
		Symbol        string          `json:"symbol,omitempty"`
		Name          string          `json:"name,omitempty"`
		IconURL       string          `json:"iconUrl,omitempty"`
		AssetKey      string          `json:"assetKey,omitempty"`
		Precision     int32           `json:"precision,omitempty"`
		Dust          decimal.Decimal `json:"dust,omitempty"`
	}
)

func NewMixinSafeAssetFromMixin(asset *mixin.SafeAsset) *MixinSafeAsset {
	return &MixinSafeAsset{
		AssetID:       asset.AssetID,
		ChainID:       asset.ChainID,
		KernelAssetID: asset.KernelAssetID,
		Symbol:        asset.Symbol,
		Name:          asset.Name,
		IconURL:       asset.IconURL,
		AssetKey:      asset.AssetKey,
		Precision:     asset.Precision,
		Dust:          asset.Dust,
	}
}
