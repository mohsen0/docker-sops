package main

import (
	"context"

	"github.com/mohsen0/docker-sops/internal/argscan"
	"github.com/mohsen0/docker-sops/internal/tempstore"
)

// rewriteCompose handles encrypted files referenced from inside the compose
// project (env_file, secrets, configs). Implemented once composefix lands.
func rewriteCompose(_ context.Context, argv []string, decrypted []argscan.Decrypted, _ *tempstore.Store, _ wrapOptions) ([]string, []argscan.Decrypted, error) {
	return argv, decrypted, nil
}
