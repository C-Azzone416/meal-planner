// Package draft is the transport-independent recipe-draft core. Hosted recipes also
// reuse its strict JSON-string validation before decoding untrusted input.
//
// It owns the Draft data model, structural validation, the strict content decoder, stable error
// categories, and the Service (create, get, list, update, validate) over the small Store
// persistence boundary. It is specified by docs/local-draft-contract.md, which is normative.
//
// The package imports the standard library only: no MCP, CLI, HTTP, database, provider or
// process-execution code, and no concrete store. The in-memory store implements Store.
// Local file storage, CLI draft commands and MCP adapters are deferred from the web release;
// their design in the contract does not imply that those executable surfaces are available.
//
// Validation is structural only. A structurally valid draft is well-formed, never verified,
// reviewed or approved. Nothing in this package calls an LLM or fetches a URL.
package draft
