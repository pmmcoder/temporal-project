package obs_test

import (
	"bytes"
	"strings"
	"testing"

	"personal-workflow-platform/platform/obs"
)

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	var output bytes.Buffer
	logger := obs.NewLoggerWithWriter("info", &output)
	logger.Info(
		"test",
		"authorization", "Bearer top-secret",
		"cookie", "cookie-value",
		"safe_field", "visible",
	)

	logged := output.String()
	for _, secret := range []string{"top-secret", "cookie-value"} {
		if strings.Contains(logged, secret) {
			t.Errorf("log contains sensitive value %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "<REDACTED>") || !strings.Contains(logged, "visible") {
		t.Errorf("log output = %s", logged)
	}
}
