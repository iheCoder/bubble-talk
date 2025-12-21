package director

import (
	"bubble-talk/server/internal/model"
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// SegmentRunner 片段执行器
// 负责执行 SegmentPlan，管理片段内的对话流程
type SegmentRunner struct {
	// Actor Engine 的接口（假设存在）
	// actorEngine ActorEngine
}

// NewSegmentRunner 创建片段执行器
func NewSegmentRunner() *SegmentRunner {
	return &SegmentRunner{}
}

// RunSegment 执行一个 Segment
// 输入：SegmentPlan（导演的指令）
// 输出：角色的实际对话内容
func (r *SegmentRunner) RunSegment(
	ctx context.Context,
	plan *model.SegmentPlan,
	state *model.SessionState,
) ([]model.Turn, error) {

	log.Printf("🎬 开始执行 Segment: %s (角色: %s)", plan.SegmentID, plan.RoleID)

	// 创建执行快照
	snapshot := &model.SegmentSnapshot{
		SegmentID:  plan.SegmentID,
		RoleID:     plan.RoleID,
		StartedAt:  time.Now(),
		ElapsedSec: 0,
		Status:     "RUNNING",
	}

	// 更新会话状态
	state.CurrentSegment = snapshot

	turns := make([]model.Turn, 0)
	startTime := time.Now()

	// 执行循环：让角色按照 scene_direction 演出
	for {
		// 检查是否超时
		elapsed := int(time.Since(startTime).Seconds())
		if elapsed >= plan.MaxDurationSec {
			log.Printf("⏰ Segment 超时：%d秒", elapsed)
			break
		}

		// TODO: 调用 Actor Engine，让角色生成对话
		// turn := r.actorEngine.GenerateTurn(ctx, plan, state)

		// 临时模拟：生成一轮对话
		turn := model.Turn{
			Role: plan.RoleID,
			Text: fmt.Sprintf("[模拟] 按照分镜演出：%s", truncateForLog(plan.SceneDirection, 50)),
			TS:   time.Now(),
		}

		turns = append(turns, turn)
		state.Turns = append(state.Turns, turn)

		// 更新快照
		snapshot.ElapsedSec = int(time.Since(startTime).Seconds())

		// 检查是否需要用户参与
		// 如果 scene_direction 中提到"等用户"或"问用户"，就停下来
		if r.shouldWaitForUser(plan.SceneDirection) {
			log.Printf("⏸️ 等待用户参与")
			break
		}

		// 简化实现：只生成一轮对话就结束
		break
	}

	// 标记完成
	snapshot.Status = "COMPLETED"
	snapshot.ElapsedSec = int(time.Since(startTime).Seconds())

	log.Printf("✅ Segment 完成：生成 %d 轮对话，用时 %d 秒", len(turns), snapshot.ElapsedSec)

	return turns, nil
}

// shouldWaitForUser 判断是否应该等待用户参与
func (r *SegmentRunner) shouldWaitForUser(sceneDirection string) bool {
	// 简化实现：检查关键词
	keywords := []string{"等用户", "问用户", "等待", "停下来", "观察用户"}
	for _, kw := range keywords {
		if strings.Contains(sceneDirection, kw) {
			return true
		}
	}
	return false
}

func truncateForLog(text string, maxRunes int) string {
	if maxRunes <= 0 || text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}

	return string(runes[:maxRunes]) + "..."
}
