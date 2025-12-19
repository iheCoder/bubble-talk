package session

import (
	"context"

	"bubble-talk/server/internal/model"
)

type Store interface {
	Get(ctx context.Context, id string) (*model.SessionState, error)
	Save(ctx context.Context, s *model.SessionState) error
}
