package person

import (
	"context"
	"database/sql"
	"fmt"

	"memento/backend/internal/msgvault"
)

// ResolveAndPersist runs the deterministic resolver and writes the resulting
// clusters. Advisory suggestions are returned in the report but are not stored
// until the merge-suggestion queue is wired in.
func ResolveAndPersist(ctx context.Context, reader *msgvault.Reader, db *sql.DB, opts ResolveOptions) (ResolveReport, error) {
	locked, err := LoadLockedEmails(ctx, db)
	if err != nil {
		return ResolveReport{}, fmt.Errorf("load locked emails: %w", err)
	}
	report, clusters, err := Resolve(ctx, reader, locked, opts)
	if err != nil {
		return ResolveReport{}, err
	}
	created, linked, clusterPersonIDs, err := PersistClustersWithMapping(ctx, db, clusters)
	if err != nil {
		return ResolveReport{}, err
	}
	report.PersonsCreated = created
	report.EmailsLinked = linked
	if _, err := PersistResolveSuggestions(ctx, db, report.Suggestions, clusterPersonIDs); err != nil {
		return ResolveReport{}, fmt.Errorf("persist merge suggestions: %w", err)
	}
	return report, nil
}
