//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPluginIsDiscoveredByDocker(t *testing.T) {
	out, err := exec.Command("docker", "sops", "version").CombinedOutput()
	if err != nil {
		t.Fatalf("docker sops version: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "docker-sops version ") {
		t.Fatalf("unexpected output: %q", out)
	}
}
