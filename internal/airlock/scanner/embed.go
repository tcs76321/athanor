package scanner

import _ "embed"

// defaultYARARules is the in-tree baseline YARA rule
// set used by the YARA adapter when the operator has
// not configured a private rule set. The file lives
// at internal/airlock/scanner/data/injection.yar and
// is shipped with the binary via go:embed so a fresh
// clone has rules available without a separate
// download step. Operators who want a private rule
// set configure `airlock.yara_rule_set` in config
// (which the cmd layer wires through to the YARA
// adapter's RuleSet field).
//
//go:embed data/injection.yar
var defaultYARARules string

// DefaultYARARules returns the in-tree baseline YARA
// rule set. The cmd layer materializes this to
// <state-dir>/yara/injection.yar on every boot and
// passes the materialized path to the YARA adapter.
func DefaultYARARules() string { return defaultYARARules }
