// Package keep pins dependencies that other packages will import so that
// parallel work never needs to edit go.mod. Delete once the imports exist.
package keep

import (
	_ "github.com/compose-spec/compose-go/v2/cli"
	_ "github.com/compose-spec/compose-go/v2/types"
	_ "github.com/zalando/go-keyring"
)
