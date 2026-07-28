package pii

var DefaultPIIPatterns = []Pattern{
	// Core / Global patterns
	{Name: "email", Regex: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Enabled: true, Action: ActionMask},
	{Name: "phone", Regex: `(?:\b|\+)(?:90|0)?[5-9](?:[\s-]?\d){9}\b`, Enabled: true, Action: ActionMask},
	{Name: "credit_card", Regex: `\b(?:\d[ -]*?){13,16}\b`, Enabled: true, Action: ActionMask},
	{Name: "ipv4", Regex: `\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`, Enabled: true, Action: ActionMask},
	{Name: "ssn", Regex: `\b\d{3}-\d{2}-\d{4}\b`, Enabled: true, Action: ActionMask},

	// Turkey (TR) specific patterns
	{Name: "tckn", Regex: `\b[1-9][0-9]{10}\b`, Enabled: true, Action: ActionMask},
	{Name: "tr_iban", Regex: `\bTR[0-9]{2}[0-9]{5}[0-9]{1}[0-9]{16}\b`, Enabled: true, Action: ActionMask},
	{Name: "tr_plate", Regex: `\b(0[1-9]|[1-7][0-9]|8[0-1])\s+[A-Z]{1,3}\s+[0-9]{2,4}\b`, Enabled: true, Action: ActionMask},
	{Name: "tr_vkn", Regex: `\b[0-9]{10}\b`, Enabled: true, Action: ActionMask},

	// Europe (EU / GDPR) specific patterns
	{Name: "eu_iban", Regex: `\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`, Enabled: true, Action: ActionMask},
	{Name: "eu_passport", Regex: `\b[A-Z0-9]{9}\b`, Enabled: true, Action: ActionMask},
	{Name: "vin", Regex: `\b[A-HJ-NPR-Z0-9]{17}\b`, Enabled: true, Action: ActionMask},

	// USA (US) specific patterns
	{Name: "us_zip", Regex: `\b\d{5}(?:-\d{4})?\b`, Enabled: true, Action: ActionMask},
	{Name: "us_ein", Regex: `\b\d{2}-\d{7}\b`, Enabled: true, Action: ActionMask},
	{Name: "us_itin", Regex: `\b9\d{2}-\d{2}-\d{4}\b`, Enabled: true, Action: ActionMask},
}
