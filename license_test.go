package attributed

import (
	"os"
	"strings"
	"testing"
)

// TestNoticesCarryBothLicenseFiles reads the license files from disk so a
// broken embed or a truncated notice fails independently of the embed.
func TestNoticesCarryBothLicenseFiles(t *testing.T) {
	t.Parallel()
	notices := Notices()
	for _, name := range []string{"LICENSE", "LICENSE.upstream"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(notices, string(data)) {
			t.Errorf("Notices() does not contain %s verbatim", name)
		}
	}
	own := strings.Index(notices, "Copyright (c) 2026 Austin Michne")
	upstream := strings.Index(notices, "Copyright (c) 2026 Comarch S.A.")
	if own < 0 || upstream < 0 || own > upstream {
		t.Errorf("Notices() order: own notice at %d, upstream notice at %d; want own first", own, upstream)
	}
}
