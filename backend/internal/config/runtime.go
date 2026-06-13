package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvModelProvider           = "MEMENTO_MODEL_PROVIDER"
	EnvAgentModel              = "MEMENTO_AGENT_MODEL"
	EnvModelAPIKey             = "MEMENTO_MODEL_API_KEY"
	EnvModelBaseURL            = "MEMENTO_MODEL_BASE_URL"
	EnvModelThinking           = "MEMENTO_MODEL_THINKING"
	EnvModelReasoningEffort    = "MEMENTO_MODEL_REASONING_EFFORT"
	EnvModelReplayReasoning    = "MEMENTO_MODEL_REPLAY_REASONING"
	EnvAgentStepLimit          = "MEMENTO_AGENT_STEP_LIMIT"
	EnvAgentStaleAfterMS       = "MEMENTO_AGENT_STALE_AFTER_MS"
	EnvAgentDecisionTimeoutMS  = "MEMENTO_AGENT_DECISION_TIMEOUT_MS"
	EnvAgentContextLimitTokens = "MEMENTO_AGENT_CONTEXT_LIMIT_TOKENS"
	EnvAgentVerboseLogs        = "MEMENTO_AGENT_VERBOSE_LOGS"
	EnvBackendURL              = "MEMENTO_BACKEND_URL"
	EnvInternalToken           = "MEMENTO_INTERNAL_TOKEN"
	EnvMsgvaultDB              = "MEMENTO_MSGVAULT_DB"
	EnvMsgvaultHome            = "MEMENTO_MSGVAULT_HOME"
	EnvMsgvaultAPIURL          = "MEMENTO_MSGVAULT_API_URL"
	EnvAllowedDevOrigins       = "MEMENTO_ALLOWED_DEV_ORIGINS"
	EnvAgentSimulation         = "MEMENTO_AGENT_SIMULATION"
	EnvAgentSimulationDelayMS  = "MEMENTO_AGENT_SIMULATION_DELAY_MS"
	EnvPublicAgentSimulation   = "NEXT_PUBLIC_MEMENTO_AGENT_SIMULATION"
)

const DefaultBackendPort = 8787

const (
	ProviderGemini           = "gemini"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderFake             = "fake"
)

const DefaultGeminiInteractionsEndpoint = "https://generativelanguage.googleapis.com/v1beta/interactions?alt=sse"

func EnvString(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func EnvBool(name string) bool {
	return EnvString(name) == "1"
}

func EnvInt(name string, defaultValue int) int {
	raw := EnvString(name)
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultValue
	}
	return n
}

func EnvInt64(name string, defaultValue int64) int64 {
	raw := EnvString(name)
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return defaultValue
	}
	return n
}

func EnvMillisDuration(name string, defaultValue time.Duration) time.Duration {
	ms := EnvInt64(name, int64(defaultValue/time.Millisecond))
	return time.Duration(ms) * time.Millisecond
}

func ModelProvider() string {
	if v := EnvString(EnvModelProvider); v != "" {
		return v
	}
	return ProviderGemini
}

func ModelAPIKey() string {
	return EnvString(EnvModelAPIKey)
}

func ModelBaseURL() string {
	return strings.TrimRight(EnvString(EnvModelBaseURL), "/")
}

func ModelReasoningEffort() string {
	return EnvString(EnvModelReasoningEffort)
}

func ModelThinking() string {
	return strings.ToLower(EnvString(EnvModelThinking))
}

func GeminiEndpoint() string {
	if v := EnvString(EnvModelBaseURL); v != "" {
		return v
	}
	return DefaultGeminiInteractionsEndpoint
}

func ModelReplayReasoning() bool {
	return EnvBool(EnvModelReplayReasoning)
}

func DefaultModelForProvider(provider string) string {
	switch provider {
	case ProviderOpenAICompatible:
		return "gemma-4-26b-a4b-it"
	case ProviderFake:
		return "fake"
	default:
		return "gemini-3.5-flash"
	}
}

func AgentModelFor(agentType, provider string) string {
	if suffix := agentModelSuffix(agentType); suffix != "" {
		if v := EnvString("MEMENTO_" + suffix + "_MODEL"); v != "" {
			return v
		}
	}
	if v := EnvString(EnvAgentModel); v != "" {
		return v
	}
	return DefaultModelForProvider(provider)
}

func agentModelSuffix(agentType string) string {
	switch agentType {
	case "collector":
		return "COLLECTOR"
	case "project_compile":
		return "PROJECT"
	case "concept_compile":
		return "CONCEPT"
	case "person_enrich":
		return "PERSON"
	case "dashboard":
		return "MEMENTO"
	default:
		return ""
	}
}

func AgentStepLimit() int {
	return EnvInt(EnvAgentStepLimit, 20)
}

func AgentMaxParallelTools() int {
	// Default to a higher concurrency limit of 8 when using the msgvault HTTP API,
	// since API requests can be executed concurrently without local CLI invocation overhead.
	// Otherwise, default to 4 when calling the msgvault CLI to avoid high process fork overhead.
	if EnvString(EnvMsgvaultAPIURL) != "" {
		return 8
	}
	return 4
}

func AgentStaleAfter() time.Duration {
	return EnvMillisDuration(EnvAgentStaleAfterMS, 2*time.Minute)
}

func AgentDecisionTimeout() time.Duration {
	return EnvMillisDuration(EnvAgentDecisionTimeoutMS, 90*time.Second)
}

func AgentContextLimitTokens() int64 {
	return EnvInt64(EnvAgentContextLimitTokens, 128000)
}

func AgentVerboseLogs() bool {
	return EnvBool(EnvAgentVerboseLogs)
}

func InternalToken() string {
	return EnvString(EnvInternalToken)
}
