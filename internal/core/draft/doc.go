// Package draft is the transport-independent core of the local recipe-draft workflow.
//
// It owns the Draft data model, structural validation, the strict content decoder, stable error
// categories, and the Service (create, get, list, update, validate) over the small Store
// persistence boundary. It is specified by docs/local-draft-contract.md, which is normative.
//
// The package imports the standard library only: no MCP, CLI, HTTP, database, provider or
// process-execution code, and no concrete store. Adapters (MCP, CLI) call the Service; stores
// (internal/localstore, internal/core/draft/memstore) implement Store.
//
// Validation is structural only. A structurally valid draft is well-formed, never verified,
// reviewed or approved. Nothing in this package calls an LLM or fetches a URL.
package draft
