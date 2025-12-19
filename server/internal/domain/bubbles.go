package domain

import (
	"encoding/json"
	"fmt"
	"os"

	"bubble-talk/server/internal/model"
)

// LoadBubbles 从指定路径加载泡泡数据。
func LoadBubbles(path string) ([]model.Bubble, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bubbles: %w", err)
	}

	var bubbles []model.Bubble
	if err := json.Unmarshal(data, &bubbles); err != nil {
		return nil, fmt.Errorf("parse bubbles: %w", err)
	}

	return bubbles, nil
}
