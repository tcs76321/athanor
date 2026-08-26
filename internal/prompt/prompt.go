// Package prompt assembles LLM prompts deterministically (ARCHITECTURE
// §11): the same inputs always produce a byte-identical prompt, and every
// section carries token accounting for the EventLog.
//
// M1 scope: the §11.2 assembly-order subset that exists today —
//
//  1. Static System Prompt
//  2. Security and Tool Constraints
//  3. Runtime Policy (phase-specific)
//  4. Project Context (goal, archetype)
//  5. Task Context (title, description)
//  6. Acceptance Criteria
//  13. Evaluation Instructions (comparison phases)
//
// Sections 7–12 (MCE chunks, CorrectionRecords, episodic context, dormant
// index, user preferences, candidate artifacts) arrive with the MCE (M5)
// and feedback systems (M6); their positions in the order are already
// reserved by the builder's section list.
package prompt

import (
	"github.com/tcs76321/athanor/internal/llm"
)

// Section names, in §11.2 assembly order.
const (
	SectionSystem                 = "system"
	SectionSecurityAndTools       = "security_and_tools"
	SectionRuntimePolicy          = "runtime_policy"
	SectionProjectContext         = "project_context"
	SectionTaskContext            = "task_context"
	SectionAcceptanceCriteria     = "acceptance_criteria"
	SectionEvaluationInstructions = "evaluation_instructions"
)

// staticSystem is tier 1 (§11.1): immutable core identity and safety
// constraints owned by Athanor Core. User configuration can never
// override any part of it.
const staticSystem = `You are Athanor, a local-first semi-autonomous agent.
You work toward explicitly stated goals, produce concrete artifacts, and
treat every stated acceptance criterion as a hard requirement.
You are honest about uncertainty: when information is missing, you say so
instead of inventing it.`

// securityAndTools is tier 2 (§11.1): containment rules. In M1 there are
// provably no tools (Gate G1) — the prompt states this so the model never
// hallucinates tool access.
const securityAndTools = `CONTAINMENT RULES (non-negotiable):
- You have NO tools in this phase. You cannot execute code, run commands,
  read or write files, or access the network.
- If a task cannot be completed by reasoning and writing alone, state what
  is missing instead of pretending to act.
- Never claim to have taken an action you did not take.`

// Project is the §11.2 section-4 input (project context).
type Project struct {
	Name      string
	Archetype string
	Goal      string
}

// Task is the §11.2 section-5 input (task context).
type Task struct {
	Title       string
	Description string
}

// Input is everything an M1 prompt can depend on. Zero-valued optional
// parts are omitted from assembly; Project name, Task title, and Phase
// are required.
type Input struct {
	Phase   string
	Project Project
	Task    Task
	// Criteria are the acceptance criteria, in given order.
	Criteria []string
	// EvaluationInstructions replaces the default comparison instructions
	// when the caller needs phase-specific evaluation detail (§11.2 §13).
	EvaluationInstructions string
}

// Section is one assembled prompt section with its token accounting.
type Section struct {
	Name   string `json:"name"`
	Text   string `json:"-"`
	Tokens int    `json:"tokens"`
}

// Result is an assembled prompt: the full text, the section accounting,
// and the chat messages derived from it (the system message carries §11.2
// sections 1–3; the user message carries the rest).
type Result struct {
	Text       string
	Sections   []Section
	TotalToken int
	Messages   []llm.Message
}

// EstimateTokens is the M1 token accounting approximation: ~4 bytes per
// token, deterministic and dependency-free. Exact tokenizer fidelity
// arrives with the MCE (M5); §28.2 only requires per-section accounting
// to be logged consistently.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
