package core

import (
	"context"

	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
)

type (
	GroupStore interface {
		CreateGroup(ctx context.Context, group *Group) error
		GetGroupById(ctx context.Context, id uuid.UUID) (*Group, error)
		GetGroupByName(ctx context.Context, name string) (*Group, error)
		DeleteGroup(ctx context.Context, name string) error
		UpdateGroup(ctx context.Context, name string, group *Group) error
		GetAllGroups(ctx context.Context) ([]*Group, error)
		ListTradeGroups(ctx context.Context) ([]*Group, error)
		GetTradeGroupsMap(ctx context.Context) (map[uuid.UUID]*Group, error)
	}

	Group struct {
		Id       uuid.UUID `json:"id"`
		AdminKey string    `json:"adminKey"`

		Name        string `json:"name"`
		CreatedAt   int64  `json:"createdAt"`
		UpdatedAt   int64  `json:"updatedAt"`
		Description string `json:"description"`
	}
)

func NewGroup(clk clock.Clock, adminKey string, name string, description string) *Group {
	return &Group{
		Id:          uuid.Must(uuid.NewV4()),
		AdminKey:    adminKey,
		Name:        name,
		CreatedAt:   clk.Now().Unix(),
		UpdatedAt:   clk.Now().Unix(),
		Description: description,
	}
}

func (g *Group) Update(clk clock.Clock, adminKey string, name string, description string) {
	g.AdminKey = adminKey
	g.Name = name
	g.Description = description
	g.UpdatedAt = clk.Now().Unix()
}
