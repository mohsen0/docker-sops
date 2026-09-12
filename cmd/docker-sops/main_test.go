package main

import (
	"bytes"
	"testing"

	"github.com/docker/cli/cli/command"

	"github.com/mohsen0/sops-docker-cli-plugin/internal/version"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	version.Version = "1.2.3-test"
	var out bytes.Buffer
	cli, err := command.NewDockerCli(command.WithOutputStream(&out))
	if err != nil {
		t.Fatal(err)
	}
	cmd := newVersionCommand(cli)
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "docker-sops version 1.2.3-test\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
