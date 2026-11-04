package draft

import "context"

// StoreInfo is the store's status, without recipe content or paths.
type StoreInfo struct {
	DataDirExists      bool
	PrivatePermissions bool // data root and drafts directory have no group/other access bits (always false where not supported)
	CrossProcessLock   bool // true when writes are protected by the cross-process lock
	DraftCount         int  // readable drafts
	SkippedCount       int  // unreadable entries
}

// ListResult is every readable draft plus a description of each entry that could not be read.
type ListResult struct {
	Drafts  []Draft
	Skipped []SkippedEntry
}

// Store is the persistence boundary. The Service depends only on this interface; the file store
// (internal/localstore) and the in-memory store (internal/core/draft/memstore) implement it, and
// internal/core/draft/storetest checks an implementation against the contract. Every error a Store
// returns is (or wraps) a *Error.
type Store interface {
	// Create stores d as a new draft. d is fully formed (ID, revision 1, timestamps) and
	// already validated by the service. It NEVER overwrites: if a draft with d.ID exists it
	// returns conflict/already_exists. If the store already holds MaxStoredDrafts it returns
	// limit_exceeded/store_full. On any error nothing is left behind.
	Create(ctx context.Context, d Draft) error

	// Get returns the stored draft. Errors: not_found/draft_not_found, corrupt/*,
	// unsupported_schema/schema_unsupported, io/*. It never modifies anything.
	Get(ctx context.Context, id string) (Draft, error)

	// Replace atomically replaces the stored draft id with next, if and only if the stored
	// revision equals expectedRevision. The check and the replacement are one atomic step
	// with respect to every other Store user of the same data root, including other
	// processes. Errors: conflict/revision_mismatch (with CurrentRevision; nothing written),
	// not_found, corrupt/* and unsupported_schema (the damaged file is NOT replaced), io/*.
	// The store also requires next.ID == id, next.Revision == expectedRevision+1 and
	// next.CreatedAt equal to the stored value; a violation is internal/internal and writes
	// nothing. On any error the previous valid file is intact.
	Replace(ctx context.Context, id string, expectedRevision int64, next Draft) error

	// List returns all readable drafts in unspecified order (the service sorts) and one
	// SkippedEntry per entry that could not be used. A bad entry never fails the whole call.
	// An empty or non-existent data root returns an empty result and no error.
	// It never modifies, repairs or deletes anything.
	List(ctx context.Context) (ListResult, error)

	// Info reports store status for server_status. It never modifies anything.
	Info(ctx context.Context) (StoreInfo, error)
}
