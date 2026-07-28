package guardrails

// builtInInjectionPatterns sourced from OWASP LLM Top 10 and Portkey's rule list.
var builtInInjectionPatterns = []struct {
	ID          string
	Severity    Severity
	Description string
	Patterns    []string
}{
	{
		ID: "injection_dan", Severity: SevCritical, Description: "DAN-style jailbreak",
		Patterns: []string{
			`(?i)\bdo anything now\b`,
			`(?i)you are now (?:DAN|GPT-\d+|a model without (?:rules|restrictions))`,
			`(?i)jailbreak mode\s*(?:enabled|activated|on|start)`,
		},
	},
	{
		ID: "injection_system_prompt", Severity: SevHigh, Description: "System prompt extraction",
		Patterns: []string{
			`(?i)ignore (?:all|any|previous) (?:instructions|prompts|rules)`,
			`(?i)(?:reveal|show|print|output) (?:your|the) system prompt`,
			`(?i)what (?:is|are) (?:your|the) (?:system|hidden|internal) prompt`,
		},
	},
	{
		ID: "injection_roleplay", Severity: SevHigh, Description: "Character roleplay override",
		Patterns: []string{
			`(?i)you (?:must|will|should) (?:act|behave|pretend) (?:as|like|to be)`,
			`(?i)forget (?:everything|all|your) (?:above|previous|prior)`,
		},
	},
	{
		ID: "injection_delimiter", Severity: SevHigh, Description: "Delimiter manipulation",
		Patterns: []string{
			`(?i)<\/?(?:system|assistant|user|inst)\s*>`,
			`(?i)\[INST\]|\[/INST\]`,
			`(?i)<<\s*SYS\s*>>|<<\s*/SYS\s*>>`,
		},
	},
	{
		ID: "injection_refusal_suppression", Severity: SevHigh, Description: "Refusal suppression",
		Patterns: []string{
			`(?i)(?:don't|do not|never) (?:refuse|apologize|say (?:sorry|you can't))`,
			`(?i)respond as if (?:you have|there are) no (?:restrictions|limits|rules)`,
		},
	},
	{
		ID: "injection_prefix", Severity: SevMedium, Description: "Prefix injection",
		Patterns: []string{
			`(?i)^(?:assistant|system|user)\s*:\s*`,
		},
	},
	{
		ID: "injection_fewshot", Severity: SevMedium, Description: "Few-shot injection",
		Patterns: []string{
			`(?i)example \d+:\s*user:`,
		},
	},
	{
		ID: "injection_hypothetical", Severity: SevLow, Description: "Hypothetical scenario framing",
		Patterns: []string{
			`(?i)in a (?:hypothetical|fictional|alternate) (?:world|universe|scenario) where`,
		},
	},
	{
		ID: "injection_translation_bypass", Severity: SevMedium, Description: "Translation bypass",
		Patterns: []string{
			`(?i)translate .{0,80}to (?:a language|other language)`,
			`(?i)respond in (?:a )?(?:language|other) (?:where|that)`,
		},
	},
	{
		ID: "injection_encoding_bypass", Severity: SevHigh, Description: "Encoding bypass",
		Patterns: []string{
			`(?i)(?:base64|rot13|hex|unicode) (?:encoded|decoded) (?:instruction|prompt)`,
			`(?i)decode (?:this|the following) (?:base64|hex)`,
		},
	},
}

// builtInToxicPatterns cover hate, violence, harassment, sexual content, and self-harm.
// Self-harm is hard-coded to block (Mode is per-rule, not per-config) because it is the
// highest-harm category.
var builtInToxicPatterns = []struct {
	ID       string
	Severity Severity
	Mode     Action
	Patterns []string
}{
	{
		ID: "toxic_hate", Severity: SevHigh, Mode: ActionBlock,
		Patterns: []string{`(?i)\b(?:kill all|exterminate|gas the)\b\s+\w+`},
	},
	{
		ID: "toxic_violence", Severity: SevHigh, Mode: ActionBlock,
		Patterns: []string{`(?i)\b(?:how to (?:make|build)\s+a\s+(?:bomb|explosive|weapon))`},
	},
	{
		ID: "toxic_harassment", Severity: SevMedium, Mode: ActionBlock,
		Patterns: []string{`(?i)\b(?:go die|kill yourself|kys)\b`},
	},
	{
		ID: "toxic_sexual", Severity: SevMedium, Mode: ActionWarn,
		Patterns: []string{`(?i)\b(?:porn|explicit (?:sex|content))\b`},
	},
	{
		ID: "toxic_selfharm", Severity: SevCritical, Mode: ActionBlock,
		Patterns: []string{`(?i)\b(?:suicide methods?|ways to (?:end|take) my life)\b`},
	},
}
