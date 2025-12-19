package main

import (
	"flag"
	"log"
	"net/http"

	"bubble-talk/server/internal/api"
	"bubble-talk/server/internal/session"
)

func main() {
	// 第一阶段以“本地可跑、可调试”为优先：参数用 flag，敏感信息（OpenAI API Key）用环境变量。
	// - OPENAI_API_KEY：用于签发 Realtime ephemeral key（不要放到前端）
	// - OPENAI_REALTIME_MODEL / OPENAI_REALTIME_VOICE：可选，便于你在本地快速切换模型/音色
	addr := flag.String("addr", ":8080", "http listen address")
	bubblesPath := flag.String("bubbles", "server/configs/bubbles.json", "bubbles config path")
	flag.Parse()

	store := session.NewInMemoryStore()
	server, err := api.NewServer(store, *bubblesPath)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("bubbletalk server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, server.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
