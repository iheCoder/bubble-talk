package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"bubble-talk/server/internal/api"
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"
)

func main() {
	// 第一阶段以"本地可跑、可调试"为优先：参数用 flag，敏感信息（OpenAI API Key）用环境变量。
	// - OPENAI_API_KEY：用于签发 Realtime ephemeral key（不要放到前端）
	// - OPENAI_REALTIME_MODEL / OPENAI_REALTIME_VOICE：可选，便于你在本地快速切换模型/音色
	configPath := flag.String("config", "server/configs/config.yaml", "config file path")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 初始化存储
	store := session.NewInMemoryStore()
	timelineStore := timeline.NewInMemoryStore()

	// 创建服务器
	server, err := api.NewServer(cfg, store, timelineStore)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("bubbletalk server listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
