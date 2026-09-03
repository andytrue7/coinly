module github.com/andytrue7/coinly/services/identity

go 1.25.0

require (
	github.com/andytrue7/coinly/pkg v0.0.0
	golang.org/x/crypto v0.55.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// Workspace-sibling modules are wired with replace directives so the
// service also builds outside go.work (e.g. a per-service Docker build
// that copies only pkg/ + gen/go + this service).
replace github.com/andytrue7/coinly/pkg => ../../pkg
