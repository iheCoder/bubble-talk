package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig           `yaml:"server"`
	OpenAI   OpenAIConfig           `yaml:"openai"`
	LLM      LLMConfig              `yaml:"llm"`
	Gateway  GatewayConfig          `yaml:"gateway"`
	Director DirectorConfig         `yaml:"director"`
	Actor    ActorConfig            `yaml:"actor"`
	Session  SessionConfig          `yaml:"session"`
	Learning LearningConfig         `yaml:"learning"`
	Logging  LoggingConfig          `yaml:"logging"`
	Paths    PathsConfig            `yaml:"paths"`
	Roles    map[string]RoleProfile `yaml:"roles"`
}

type ServerConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type OpenAIConfig struct {
	APIKey                  string  `yaml:"api_key"`
	RealtimeURL             string  `yaml:"realtime_url"`
	Model                   string  `yaml:"model"`
	Voice                   string  `yaml:"voice"`
	Temperature             float64 `yaml:"temperature"`
	MaxResponseOutputTokens int     `yaml:"max_response_output_tokens"`
}

// LLMConfig LLM 决策配置（用于导演引擎）
type LLMConfig struct {
	Provider  string            `yaml:"provider"` // "openai", "anthropic" or "talopenai"
	OpenAI    LLMProviderConfig `yaml:"openai"`
	Anthropic LLMProviderConfig `yaml:"anthropic"`
	TalOpenAI LLMProviderConfig `yaml:"talopenai"`
}

// LLMProviderConfig LLM 提供商配置
type LLMProviderConfig struct {
	APIKey      string  `yaml:"api_key"`
	APIURL      string  `yaml:"api_url"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

type RoleProfile struct {
	Voice  string `yaml:"voice"`
	Avatar string `yaml:"avatar"`
}

type GatewayConfig struct {
	DefaultInstructions          string        `yaml:"default_instructions"`
	InputAudioFormat             string        `yaml:"input_audio_format"`
	OutputAudioFormat            string        `yaml:"output_audio_format"`
	InputAudioTranscriptionModel string        `yaml:"input_audio_transcription_model"`
	PingInterval                 time.Duration `yaml:"ping_interval"`
}

type DirectorConfig struct {
	// Type 决定导演实现：beat | segment
	Type                   string   `yaml:"type"`
	EnableLLM              bool     `yaml:"enable_llm"`
	AvailableRoles         []string `yaml:"available_roles"`
	AvailableBeats         []string `yaml:"available_beats"`
	DefaultTalkBurstLimit  int      `yaml:"default_talk_burst_limit"`
	HighLoadTalkBurstLimit int      `yaml:"high_load_talk_burst_limit"`
	OutputClockThreshold   int      `yaml:"output_clock_threshold"`
}

type ActorConfig struct {
	PromptsDir      string `yaml:"prompts_dir"`
	MaxPromptLength int    `yaml:"max_prompt_length"`
}

type SessionConfig struct {
	DefaultTimeout     time.Duration `yaml:"default_timeout"`
	MaxInactiveTime    time.Duration `yaml:"max_inactive_time"`
	MaxSessionsPerUser int           `yaml:"max_sessions_per_user"`
}

type LearningConfig struct {
	InitialMastery         float64 `yaml:"initial_mastery"`
	MasteryUpdateRate      float64 `yaml:"mastery_update_rate"`
	MisconceptionDecayRate float64 `yaml:"misconception_decay_rate"`
	TransferWeight         float64 `yaml:"transfer_weight"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

type PathsConfig struct {
	Prompts  string `yaml:"prompts"`
	Concepts string `yaml:"concepts"`
	Bubbles  string `yaml:"bubbles"`
	Scripts  string `yaml:"scripts"`
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	fmt.Printf("📋 Loading config from: %s\n", path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	fmt.Printf("✅ Config file read successfully (%d bytes)\n", len(data))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	fmt.Printf("✅ Config parsed successfully\n")

	// 从环境变量覆盖敏感信息
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		fmt.Printf("🔑 Using OPENAI_API_KEY from environment variable\n")
		cfg.OpenAI.APIKey = apiKey
	} else if cfg.OpenAI.APIKey != "" {
		fmt.Printf("🔑 Using OPENAI_API_KEY from config file\n")
	}

	// LLM API keys
	if llmKey := os.Getenv("LLM_API_KEY"); llmKey != "" {
		fmt.Printf("🔑 Using LLM_API_KEY from environment variable\n")
		if cfg.LLM.Provider == "openai" {
			cfg.LLM.OpenAI.APIKey = llmKey
		} else if cfg.LLM.Provider == "anthropic" {
			cfg.LLM.Anthropic.APIKey = llmKey
		} else if cfg.LLM.Provider == "talopenai" {
			cfg.LLM.TalOpenAI.APIKey = llmKey
		}
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		fmt.Printf("🔑 Using ANTHROPIC_API_KEY from environment variable\n")
		cfg.LLM.Anthropic.APIKey = anthropicKey
	}

	if model := os.Getenv("OPENAI_REALTIME_MODEL"); model != "" {
		fmt.Printf("🤖 Using OPENAI_REALTIME_MODEL from environment: %s\n", model)
		cfg.OpenAI.Model = model
	}
	if voice := os.Getenv("OPENAI_REALTIME_VOICE"); voice != "" {
		fmt.Printf("🎤 Using OPENAI_REALTIME_VOICE from environment: %s\n", voice)
		cfg.OpenAI.Voice = voice
	}

	// 打印关键配置
	fmt.Printf("\n📊 Configuration Summary:\n")
	fmt.Printf("   Server: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("   OpenAI Model: %s\n", cfg.OpenAI.Model)
	fmt.Printf("   OpenAI Voice: %s\n", cfg.OpenAI.Voice)
	fmt.Printf("   Bubbles Path: %s\n", cfg.Paths.Bubbles)
	fmt.Printf("   Prompts Dir: %s\n", cfg.Paths.Prompts)
	if cfg.Paths.Scripts != "" {
		fmt.Printf("   Scripts Dir: %s\n", cfg.Paths.Scripts)
	}
	fmt.Printf("\n")

	// 验证必需配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	fmt.Printf("✅ Config validation passed\n\n")

	return &cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.OpenAI.APIKey == "" {
		return fmt.Errorf("OpenAI API key is required (set OPENAI_API_KEY env var or config)")
	}
	if c.Paths.Bubbles == "" {
		return fmt.Errorf("bubbles path is required")
	}
	return nil
}
