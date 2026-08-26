package prompt

import (
	"fmt"
	"strings"

	"github.com/tcs76321/athanor/internal/llm"
)

// runtimePolicy returns the tier-3 (§11.1) phase instructions from §13.1.
// Every phase pins its purpose; judgment phases restate determinism.
func runtimePolicy(phase string) (string, error) {
	switch phase {
	case llm.PhasePlanning:
		return "PHASE: PLANNING (low temperature 0.2).\nRead the task, project context, and acceptance criteria. Identify missing\ninformation. Propose an implementation plan including tests and\ndocumentation updates.", nil
	case llm.PhaseDiverging:
		return "PHASE: DIVERGENCE (high temperature).\nGenerate candidate solutions that genuinely differ in approach. Explore\northogonal options; avoid premature convergence on one strategy.", nil
	case llm.PhaseEvaluating:
		return "PHASE: EVALUATION (temperature 0.0 — maximally deterministic).\nCheck the work strictly against each acceptance criterion. Identify\nfailures precisely. Do not improvise criteria that were not stated.", nil
	case llm.PhaseReflecting:
		return "PHASE: REFLECTION.\nAnalyze why candidates failed. Identify missing constraints. Propose\nimprovements or hybrid approaches.", nil
	case llm.PhaseSynthesizing:
		return "PHASE: SYNTHESIS (low temperature 0.2).\nProduce the final artifact, complete and self-contained, with a change\nsummary and known limitations.", nil
	case llm.PhaseComparing:
		return "PHASE: COMPARISON (temperature 0.0 — maximally deterministic).\nCompare the candidate artifact against the previous best using the\nacceptance criteria and evaluation results. Output a structured verdict:\nwinner (new|previous|none), confidence (0.0-1.0), reasons, and missing\nrequirements.", nil
	default:
		return "", fmt.Errorf("prompt: no runtime policy defined for phase %q (§13.1)", phase)
	}
}

// Assemble builds the deterministic M1 prompt. It is a pure function:
// identical inputs produce byte-identical output.
func Assemble(in Input) (Result, error) {
	if in.Project.Name == "" {
		return Result{}, fmt.Errorf("prompt: project name is required")
	}
	if in.Task.Title == "" {
		return Result{}, fmt.Errorf("prompt: task title is required")
	}
	policy, err := runtimePolicy(in.Phase)
	if err != nil {
		return Result{}, err
	}

	var sections []Section
	add := func(name, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		sections = append(sections, Section{Name: name, Text: text, Tokens: EstimateTokens(text)})
	}

	add(SectionSystem, staticSystem)
	add(SectionSecurityAndTools, securityAndTools)
	add(SectionRuntimePolicy, policy)
	add(SectionProjectContext, projectContext(in.Project))
	add(SectionTaskContext, taskContext(in.Task))
	add(SectionAcceptanceCriteria, criteria(in.Criteria))
	add(SectionEvaluationInstructions, in.EvaluationInstructions)

	var b strings.Builder
	total := 0
	systemText := &strings.Builder{}
	userText := &strings.Builder{}
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s.Text)
		total += s.Tokens
		// §11.2 sections 1–3 form the system message; the rest the user
		// message. The split point is fixed, so output is deterministic.
		switch s.Name {
		case SectionSystem, SectionSecurityAndTools, SectionRuntimePolicy:
			if systemText.Len() > 0 {
				systemText.WriteString("\n\n")
			}
			systemText.WriteString(s.Text)
		default:
			if userText.Len() > 0 {
				userText.WriteString("\n\n")
			}
			userText.WriteString(s.Text)
		}
	}

	return Result{
		Text:       b.String(),
		Sections:   sections,
		TotalToken: total,
		Messages: []llm.Message{
			{Role: "system", Content: systemText.String()},
			{Role: "user", Content: userText.String()},
		},
	}, nil
}

func projectContext(p Project) string {
	var b strings.Builder
	b.WriteString("PROJECT\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	fmt.Fprintf(&b, "archetype: %s\n", p.Archetype)
	fmt.Fprintf(&b, "goal: %s", p.Goal)
	return b.String()
}

func taskContext(t Task) string {
	var b strings.Builder
	b.WriteString("TASK\n")
	fmt.Fprintf(&b, "title: %s\n", t.Title)
	if t.Description != "" {
		fmt.Fprintf(&b, "description: %s", t.Description)
	}
	return b.String()
}

// criteria renders acceptance criteria as a numbered list. Input order is
// preserved verbatim — criteria are requirements, not suggestions.
func criteria(cs []string) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("ACCEPTANCE CRITERIA (all must be met)\n")
	for i, c := range cs {
		fmt.Fprintf(&b, "%d. %s", i+1, c)
		if i < len(cs)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
