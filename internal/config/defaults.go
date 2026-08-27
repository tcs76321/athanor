// Defaults for omitted optional configuration fields (§29 reference config).
package config

import "time"

const (
	minute = time.Minute
	second = time.Second
)

// Val resolves an optional boolean flag, returning def when unset.
func Val(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

// applyDefaults fills every omitted optional field with its documented
// default (§29 reference config).
func applyDefaults(c *Config) {
	setStr := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	setDur := func(dst *Duration, v Duration) {
		if *dst == 0 {
			*dst = v
		}
	}
	setInt := func(dst *int, v int) {
		if *dst == 0 {
			*dst = v
		}
	}
	setTrue := func(dst **bool) {
		if *dst == nil {
			v := true
			*dst = &v
		}
	}

	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	setStr(&c.Agent.ActiveHours, "00:00-24:00")
	setDur(&c.Agent.IdleThreshold, Duration(5*minute))
	setInt(&c.Agent.MaxProactiveTasksPerDay, 10)

	setTrue(&c.Power.RequireACForDeepWork)
	if c.Power.BatteryPauseThresholdPercent == 0 {
		c.Power.BatteryPauseThresholdPercent = 20
	}
	setDur(&c.Power.IdleResumeAfter, Duration(5*minute))
	setTrue(&c.Power.PauseOnSleep)
	setTrue(&c.Power.ResumeOnWake)
	setTrue(&c.Power.DaydreamOnIdle)
	setInt(&c.Power.DaydreamMaxConcurrent, 2)
	setInt(&c.Power.DaydreamMaxWallTimeMinutes, 30)

	setStr(&c.Inference.DefaultBackend, "ollama")
	setStr(&c.Inference.OllamaURL, "http://host.containers.internal:11434")
	setTrue(&c.Inference.CloudRequiresApproval)

	defaultPersona(&c.Personas.Wide, "qwen2.5:7b", 65536, 0.7)
	defaultPersona(&c.Personas.Tall, "qwen2.5-coder:32b", 16384, 0.2)
	defaultPersona(&c.Personas.Main, "mistral-nemo:12b", 32768, 0.4)
	defaultPersona(&c.Personas.Security, "phi3:3.8b", 8192, 0.0)
	defaultPersona(&c.Personas.Alternative, "llama3.1:8b", 32768, 0.8)

	setInt(&c.ContextEngine.CodingFloor, 32768)
	setInt(&c.ContextEngine.ResearchFloor, 32768)
	setInt(&c.ContextEngine.DocumentFloor, 16384)
	setInt(&c.ContextEngine.SimpleFloor, 8192)
	setTrue(&c.ContextEngine.EnableLosslessSwapping)
	if c.ContextEngine.KVCacheWarningThresh == 0 {
		c.ContextEngine.KVCacheWarningThresh = 0.85
	}
	if c.ContextEngine.KVCacheCriticalThresh == 0 {
		c.ContextEngine.KVCacheCriticalThresh = 0.95
	}

	setInt(&c.Execution.DivergenceCandidates, 3)
	setInt(&c.Execution.MaxHardTaskVariations, 10)
	setStr(&c.Execution.JudgePersona, "security")
	setTrue(&c.Execution.RequireTestsForCode)
	setTrue(&c.Execution.RequireDocumentationForCode)
	setTrue(&c.Execution.CompareBeforeAccept)
	if c.Execution.MinJudgeConfidence == 0 {
		c.Execution.MinJudgeConfidence = 0.7
	}
	if c.Execution.PhaseWallTimeBudgets == nil {
		c.Execution.PhaseWallTimeBudgets = map[string]Duration{}
	}
	for phase, d := range map[string]Duration{
		"planning":   Duration(300 * second),
		"evaluating": Duration(600 * second),
		"default":    Duration(300 * second),
	} {
		if _, ok := c.Execution.PhaseWallTimeBudgets[phase]; !ok {
			c.Execution.PhaseWallTimeBudgets[phase] = d
		}
	}

	setTrue(&c.StrategyAnalysis.Enabled)
	setInt(&c.StrategyAnalysis.MinCohortSize, 20)
	if c.StrategyAnalysis.MinAcceptRateDelta == 0 {
		c.StrategyAnalysis.MinAcceptRateDelta = 0.15
	}
	setInt(&c.StrategyAnalysis.MaxActiveInsights, 10)
	setInt(&c.StrategyAnalysis.InsightExpiryDays, 90)
	setTrue(&c.StrategyAnalysis.StrategyNotesInPrompts)

	setInt(&c.Limits.MaxConcurrentJobs, 2)
	setInt(&c.Limits.MaxConcurrentLLMCalls, 1)
	setInt(&c.Limits.MaxConcurrentFetches, 5)
	setInt(&c.Limits.MaxTasksPerHour, 20)
	setInt(&c.Limits.MaxTotalRecoveriesPerJob, 8)

	setInt(&c.Recovery.MaxToolCallRetries, 2)
	setInt(&c.Recovery.MaxLoopInterventions, 1)
	setInt(&c.Recovery.MaxContextCompactions, 3)

	setStr(&c.Network.DefaultPolicy, "deny")
	setInt(&c.Network.RateLimitPerMinute, 30)
	if c.Network.MaxResponseBytes == 0 {
		c.Network.MaxResponseBytes = 10485760
	}
	setTrue(&c.Network.ReaderModeDefault)
	setTrue(&c.Network.BrowserModeRequiresApproval)

	setTrue(&c.Security.ScanIngressFiles)
	setTrue(&c.Security.ScanEgressFiles)
	setTrue(&c.Security.PromptInjectionScan)
	setTrue(&c.Security.QuarantineSuspiciousFiles)

	setTrue(&c.Backup.Auto)
	setStr(&c.Backup.Schedule, "0 3 * * *")
	setInt(&c.Backup.MaxLocalBackups, 10)
	setTrue(&c.Backup.IncludeWorkspaceMetadata)

	setStr(&c.Logging.Level, "info")
	if len(c.Logging.Categories) == 0 {
		c.Logging.Categories = append([]string(nil), Categories...)
	}
}

func defaultPersona(p *PersonaConfig, model string, ctx int, temp float64) {
	if p.Model == "" {
		p.Model = model
	}
	if p.ContextTarget == 0 {
		p.ContextTarget = ctx
	}
	if p.Temperature == nil {
		t := temp
		p.Temperature = &t
	}
}
