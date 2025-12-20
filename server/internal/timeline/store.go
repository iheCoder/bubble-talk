package timeline

import (
	"context"

	"bubble-talk/server/internal/model"
)

type Store interface {
	// Append 以 append-first 的契约写入 timeline，返回本次写入的 seq。
	// 约定：同一 session 的 seq 单调递增；相同 EventID 的请求应幂等返回同一 seq。
	Append(ctx context.Context, sessionID string, evt *model.Event) (int64, error)
	// List 返回该 session 的全量事件，用于回放与验收。
	List(ctx context.Context, sessionID string) ([]model.Event, error)
}
