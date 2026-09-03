// Command identity runs the Coinly identity service: registration, login,
// JWT issuance and the JWKS endpoint.
//
// Wiring of the real domain/app/adapter layers lands in later commits;
// for now this is a placeholder so the module builds, lints and tests.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "identity: not implemented yet")
	os.Exit(1)
}
