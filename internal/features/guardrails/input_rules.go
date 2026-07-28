package guardrails

type inputGuardrail struct {
	ID          string
	Severity    Severity
	Description string
	Patterns    []string
}

func allInputGuardrails() []inputGuardrail {
	return []inputGuardrail{
		promptInjection(), jailbreak(), roleplayBypass(), encodingObfuscation(),
		tokenBoundary(), systemPromptOverride(), urlInjection(),
		codeExecution(), dataExfiltration(), repetitivePattern(),
	}
}

func promptInjection() inputGuardrail {
	return inputGuardrail{
		ID: "input_prompt_injection", Severity: SevCritical, Description: "Prompt injection: direct instruction override attempts",
		Patterns: []string{
			`(?i)\bdo anything now\b`,
			`(?i)jailbreak mode\s*(?:enabled|activated|on|start)`,
			`(?i)(?:you are|you're)\s+(?:now\s+)?(?:GPT-\d+|a model without\s+(?:rules|restrictions|limits))`,
			`(?i)ignore (?:all|any|previous|the above|all prior)\s+(?:instructions|prompts|rules|directives|constraints)`,
			`(?i)(?:reveal|show|print|output|display|disclose)\s+(?:your|the)\s+(?:system|hidden|internal|original)\s+(?:prompt|instruction|message)`,
			`(?i)what (?:is|are|were)\s+(?:your|the)\s+(?:system|hidden|internal|original|initial)\s+(?:prompt|instruction|message|directive)`,
			`(?i)<\/?(?:system|assistant|user|inst|s|chat)\s*>`,
			`(?i)\[INST\]|\[/INST\]`,
			`(?i)<<\s*SYS\s*>>|<<\s*/SYS\s*>>`,
			`(?i)^(?:assistant|system|user|chatbot|ai)\s*:\s*`,
			`(?i)(?:example|shot)\s+\d+\s*:\s*(?:user|human|input)\s*:`,
			`(?i)(?:don't|do not|never|stop)\s+(?:refuse|decline|apologize|say\s+(?:sorry|you can't|you cannot|no))`,
			`(?i)respond (?:as if|like)\s+(?:you have|there are)\s+(?:no|zero)\s+(?:restrictions|limits|rules|boundaries|constraints)`,
		},
	}
}

func jailbreak() inputGuardrail {
	return inputGuardrail{
		ID: "input_jailbreak", Severity: SevCritical, Description: "Jailbreak: indirect rule override via role-play",
		Patterns: []string{
			`(?i)pretend\s+(?:to be|you are|that you are|we're|that we are)\s+(?:in\s+)?(?:a\s+)?(?:new|different|another)\s+(?:reality|world|situation|scenario|game)`,
			`(?i)(?:you are|you're)\s+(?:now\s+)?(?:acting as|playing the role of|role-playing as)`,
			`(?i)from\s+(?:now on|this point forward|this moment)\s*,\s*(?:you are|you'll be|you will be|act as)`,
			`(?i)(?:let's|lets)\s+(?:play a game|role.?play|pretend)`,
		},
	}
}

func roleplayBypass() inputGuardrail {
	return inputGuardrail{
		ID: "input_roleplay_bypass", Severity: SevHigh, Description: "Role-play bypass: fictional scenario to circumvent policies",
		Patterns: []string{
			`(?i)in a (?:fictional|hypothetical|imaginary|alternate|simulated|made.?up)\s+(?:world|scenario|setting|universe|reality)`,
			`(?i)write\s+(?:a|an)\s+(?:story|tale|narrative|screenplay|script)\s+(?:about|where|in which|that involves)`,
			`(?i)(?:pretend|imagine|suppose|assume|picture)\s+(?:that|for a moment|you're|we're|you are)`,
			`(?i)let's\s+(?:say|suppose|pretend|imagine)\s+you(?:'re| are)`,
		},
	}
}

func encodingObfuscation() inputGuardrail {
	return inputGuardrail{
		ID: "input_encoding_obfuscation", Severity: SevHigh, Description: "Encoding obfuscation: attempt to bypass pattern matching",
		Patterns: []string{
			`(?i)(?:base64|base32|base16|hex|rot13|rot47|uuencode|quoted-printable)\s*(?:encoded|encode|decode|decoded)`,
			`(?i)(?:unicode|utf|escape|percent|url)\s*(?:encoded|encode|decode|escaped)`,
			`(?i)(?:cipher|crypt|obfuscat|stegano|hidden|concealed)\s+(?:text|message|content|data|string)`,
		},
	}
}

func tokenBoundary() inputGuardrail {
	return inputGuardrail{
		ID: "input_token_boundary", Severity: SevLow, Description: "Token boundary: suspicious token patterns or unusual character sequences",
		Patterns: []string{
			`(?:[\x00-\x08\x0b\x0c\x0e-\x1f]){5,}`,
			`(?:[^\p{L}\p{N}\p{P}\p{Z}\p{S}]){10,}`,
		},
	}
}

func systemPromptOverride() inputGuardrail {
	return inputGuardrail{
		ID: "input_system_prompt_override", Severity: SevCritical, Description: "System prompt override: attempts to modify system behavior",
		Patterns: []string{
			`(?i)(?:change|modify|update|set|override|replace|alter)\s+(?:your|the|system's|default)\s+(?:system|behavior|rules|guidelines|configuration|settings|prompt|directive|instructions)`,
			`(?i)(?:add|remove|disable|enable|turn\s+(?:on|off)|activate|deactivate)\s+(?:a|the|any|all)\s+(?:rule|rules|feature|capability|restriction|limit|filter|moderation|safety)`,
			`(?i)(?:you\s+)?(?:must|should|need to|have to|shall)\s+(?:forget|ignore|disregard|overlook|bypass|circumvent|neglect|skip)\s+(?:your|the|previous|any|all)\s+(?:rules|instructions|guidelines|constraints|policies|limits)`,
			`(?i)revert\s+(?:to|back to)\s+(?:your|the|original)\s+(?:version|mode|settings|behavior|state|configuration)`,
			`(?i)(?:go\s+back|reset|restore)\s+(?:to|your)\s+(?:default|original|base)\s+(?:state|mode|settings|behavior|configuration)`,
			`(?i)(?:new|updated|fresh)\s+(?:instructions|directives|guidelines|rules|orders)\s*(?::|follow|are|below)`,
		},
	}
}

func urlInjection() inputGuardrail {
	return inputGuardrail{
		ID: "input_url_injection", Severity: SevLow, Description: "URL injection: attempts to make the model access external content",
		Patterns: []string{
			`(?:fetch|retrieve|download|access|open|visit|load|get)\s+(?:https?://|www\.|this\s+url|the\s+(?:following|above|below)\s+(?:url|link|page|content|website|site))`,
			`(?:read|parse|extract|scrape|capture)\s+(?:content|data|information|text|html|page)\s+(?:from|at)\s+https?://`,
		},
	}
}

func codeExecution() inputGuardrail {
	return inputGuardrail{
		ID: "input_code_execution", Severity: SevHigh, Description: "Code execution: attempts to run or evaluate code",
		Patterns: []string{
			`(?i)(?:run|execute|eval|exec|system|shell|bash|cmd|powershell|subprocess|spawn|popen|fork)\s*(?:a\s+)?(?:command|code|script|program|binary|process)`,
			`(?i)(?:import|include|require|load)\s+(?:os|sys|subprocess|shutil|shlex|commands|distutils|ctypes|pdb|inspect|builtins|__builtins__)`,
			`(?i)(?:os\.system|subprocess\.|sys\.stdin|sys\.stdout|sys\.stderr|builtins\.eval|builtins\.exec|eval\(|exec\(|exec\s+open|__import__)`,
			`(?i)curl\s+|wget\s+|nc\s+|ncat\s+|telnet\s+|ssh\s+`,
		},
	}
}

func dataExfiltration() inputGuardrail {
	return inputGuardrail{
		ID: "input_data_exfiltration", Severity: SevHigh, Description: "Data exfiltration: attempts to extract or transmit data",
		Patterns: []string{
			`(?i)(?:exfiltrat|leak|steal|extract|dump)\s+(?:data|information|content|files|documents|records|database|credentials|secrets|tokens|keys)`,
			`(?i)(?:send|transmit|upload|post|forward|relay)\s+(?:data|info|content|file|document)\s+(?:to|via|using|through|over)\s+(?:http|https|ftp|smtp|dns|external|remote|server|url|api)`,
			`(?i)(?:encode|pack|compress|archive|serialize|convert)\s+(?:data|content|info|file|documents)\s+(?:as|into|to|using)\s+(?:base64|hex|binary|json|xml|yaml|bytes)`,
		},
	}
}

func repetitivePattern() inputGuardrail {
	return inputGuardrail{
		ID: "input_repetitive_pattern", Severity: SevLow, Description: "Repetitive pattern: unusually repetitive or templated content",
		// backreferences (\1) removed — never matched in RE2, Go 1.26 rejects them.
		// For same-char repetition, the token boundary rule [^\w\s]{20,} below catches many cases.
		Patterns: []string{
			`(?i)(?:repeat|say|write|output|print|type|echo)\s+(?:this|the following|after me|exactly)\s+(?:over and over|repeatedly|\d+\s+times)`,
		},
	}
}
