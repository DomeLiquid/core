package core

import (
	"context"
	"strconv"

	"github.com/DomeLiquid/core/utils"
	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type (
	AccountStore interface {
		GetAccountById(ctx context.Context, accountId uuid.UUID) (*Account, error)
		ListAccountByPubkey(ctx context.Context, groupId uuid.UUID, pubkey string) ([]*Account, error)
		GetAccountByPubkey(ctx context.Context, groupId uuid.UUID, pubkey string, index uint8) (*Account, error)
		CreateAccount(ctx context.Context, account *Account) error
		UpsertAccount(ctx context.Context, account *Account) error
	}

	Account struct {
		Id           uuid.UUID    `json:"id"`
		GroupId      uuid.UUID    `json:"groupId"`
		PubKey       string       `json:"pubKey"`
		AccountFlags AccountFlags `json:"accountFlags"`
		Index        uint8        `json:"index"`

		CreatedAt int64 `json:"createdAt"`
		UpdatedAt int64 `json:"updatedAt"`
	}
)

type AccountFlags uint8

const (
	DisabledFlag                 AccountFlags = 1 << 0
	InFlashloanFlag              AccountFlags = 1 << 1
	FlashloanEnabledFlag         AccountFlags = 1 << 2
	TransferAuthorityAllowedFlag AccountFlags = 1 << 3
)

func (a *Account) SetFlag(flag AccountFlags) {
	a.AccountFlags |= flag
}

func (a *Account) UnsetFlag(flag AccountFlags) {
	a.AccountFlags &= ^flag
}

func (a *Account) GetFlag(flag AccountFlags) bool {
	return a.AccountFlags&flag != 0
}

func NewAccount(clk clock.Clock, groupId uuid.UUID, pubKey string, index uint8) *Account {
	return &Account{
		Id:        uuid.Must(uuid.FromString(utils.GenUuidFromStrings(groupId.String(), pubKey, strconv.Itoa(int(index))))),
		GroupId:   groupId,
		PubKey:    pubKey,
		Index:     index,
		CreatedAt: clk.Now().Unix(),
		UpdatedAt: clk.Now().Unix(),
	}
}

func GetAccountHealth(totalAssets, totalLiabilities decimal.Decimal) decimal.Decimal {
	health := ONE

	if totalLiabilities.IsZero() {
		return health
	}

	if totalAssets.IsPositive() {
		health = (totalAssets.Sub(totalLiabilities)).Div(totalAssets)
	}
	return health
}
