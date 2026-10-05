// Package v1alpha1 holds the bluepave config API: JSON Schemas for the platform config, profiles
// and module manifests (ADR-0001).
package v1alpha1

import "embed"

// APIVersion is the apiVersion of every v1alpha1 document.
const APIVersion = "bluepave.dev/v1alpha1"

// Schemas are the JSON Schemas, embedded so the CLI needs no files at runtime.
//
//go:embed *.schema.json
var Schemas embed.FS
