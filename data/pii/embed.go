package piiembed

import "embed"

//go:embed names/*.txt.gz
var NamesFS embed.FS
