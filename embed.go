// Package opacontroller exposes the files that are embedded into the binary.
package opacontroller

import "embed"

// Policies holds the OPA policy files located in the policies directory.
//
//go:embed policies/*.rego
var Policies embed.FS
