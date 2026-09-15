package supervisor

import "os"

const (
	ExampleEcho     = "echo"
	ExampleClaude   = "claude-code"
	ExampleGrok     = "grok-build"
	ExampleDeepseek = "deepseek-harness"

	TestVendorKey = "htm-test-key"

	ClaudeHost   = "claude.hermetarium.test"
	GrokHost     = "grok.hermetarium.test"
	DeepseekHost = "deepseek.hermetarium.test"

	VendorMockPort = 18082

	ClaudeImage   = "hermetarium-claude-code:local"
	GrokImage     = "hermetarium-grok-build:local"
	DeepseekImage = "hermetarium-deepseek-harness:local"
)

type Example struct {
	Name       string
	Image      string
	VendorHost string // empty for echo
	KeyEnv     string
	Dialect    string // anthropic | openai
	LivePeer   string
	LivePort   int
}

func LookupExample(name string) (Example, bool) {
	switch name {
	case "", ExampleEcho:
		return Example{Name: ExampleEcho, Image: EchoImage}, true
	case ExampleClaude:
		return Example{
			Name: ExampleClaude, Image: ClaudeImage, VendorHost: ClaudeHost,
			KeyEnv: "HERMETARIUM_ANTHROPIC_API_KEY", Dialect: "anthropic",
			LivePeer: "api.anthropic.com", LivePort: 443,
		}, true
	case ExampleGrok:
		return Example{
			Name: ExampleGrok, Image: GrokImage, VendorHost: GrokHost,
			KeyEnv: "HERMETARIUM_XAI_API_KEY", Dialect: "openai",
			LivePeer: "api.x.ai", LivePort: 443,
		}, true
	case ExampleDeepseek:
		return Example{
			Name: ExampleDeepseek, Image: DeepseekImage, VendorHost: DeepseekHost,
			KeyEnv: "HERMETARIUM_DEEPSEEK_API_KEY", Dialect: "openai",
			LivePeer: "api.deepseek.com", LivePort: 443,
		}, true
	default:
		return Example{}, false
	}
}

func (e Example) Secret() (key string, live bool) {
	if e.KeyEnv == "" {
		return "", false
	}
	if v := os.Getenv(e.KeyEnv); v != "" {
		return v, true
	}
	return TestVendorKey, false
}

func VendorHosts() []string {
	return []string{ClaudeHost, GrokHost, DeepseekHost}
}
