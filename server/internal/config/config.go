package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	OpenAI   OpenAIConfig   `yaml:"openai"`
	Gateway  GatewayConfig  `yaml:"gateway"`
	Director DirectorConfig `yaml:"director"`
	Actor    ActorConfig    `yaml:"actor"`
	Session  SessionConfig  `yaml:"session"`
	Learning LearningConfig `yaml:"learning"`
	Logging  LoggingConfig  `yaml:"logging"`
	Paths    PathsConfig    `yaml:"paths"`
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

type GatewayConfig struct {
	DefaultInstructions string        `yaml:"default_instructions"`
	InputAudioFormat    string        `yaml:"input_audio_format"`
	OutputAudioFormat   string        `yaml:"output_audio_format"`
	PingInterval        time.Duration `yaml:"ping_interval"`
}

type DirectorConfig struct {
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
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 从环境变量覆盖敏感信息
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		cfg.OpenAI.APIKey = apiKey
	}
	if model := os.Getenv("OPENAI_REALTIME_MODEL"); model != "" {
		cfg.OpenAI.Model = model
	}
	if voice := os.Getenv("OPENAI_REALTIME_VOICE"); voice != "" {
		cfg.OpenAI.Voice = voice
	}

	// 验证必需配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

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
