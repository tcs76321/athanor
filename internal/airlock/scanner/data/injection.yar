// Baseline YARA rule set for the M4-T3 YARA adapter
// (internal Cmd-athanor-scanners-yara.go). This is the
// minimum useful set: prompt-injection-shaped strings the
// heuristic regex might miss. The set is intentionally
// small and easy to extend; a hardened host replaces
// this file with a private rule set via config
// (airlock.yara_rule_set).
//
// The rules here are deliberately broader than the
// heuristic's regex patterns. The YARA scanner runs
// *in addition to* the heuristic, not as a replacement;
// the asymmetry (cheap+fast heuristic + slow+expensive
// YARA) is the fail-closed posture ADR-0015 documents.
rule athanor_prompt_injection_ignore_previous
{
    meta:
        description = "Common prompt-injection: ignore previous instructions"
        severity = "high"
    strings:
        $a = /ignore (all |every )?(previous|prior|above) (instructions?|directives?|rules?)/ nocase
        $b = /disregard (all|every|the) (the )?(rules?|instructions?|directives?)/ nocase
    condition:
        $a or $b
}

rule athanor_prompt_injection_role_reassign
{
    meta:
        description = "Role-reassignment: 'you are now' or 'act as admin/root'"
        severity = "high"
    strings:
        $a = /you are now (an? )?(unrestricted|admin|root|jailbroken|new)/ nocase
        $b = /act as (a|an) (admin|root|system|developer)/ nocase
    condition:
        $a or $b
}

rule athanor_chatml_smuggle
{
    meta:
        description = "ChatML / special-token smuggling"
        severity = "critical"
    strings:
        $a = /<\|im_start\|>/
        $b = /<\|im_end\|>/
        $c = /<\|system\|>/
    condition:
        any of them
}

rule athanor_system_role_marker
{
    meta:
        description = "System/assistant role markers in user-supplied text"
        severity = "medium"
    strings:
        $a = /^system:/ nocase
        $b = /^assistant:/ nocase
    condition:
        any of them
}
