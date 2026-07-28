package integration

import (
	"os"
	"testing"

	"github.com/ilter-ai/ilter/internal/features/pii"
)

func TestMain(m *testing.M) {
	pii.LoadPatterns(pii.DefaultPIIPatterns)
	os.Exit(m.Run())
}
