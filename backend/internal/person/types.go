package person

import "time"

const (
	LinkSourcePlusTag         = "plus_tag"
	LinkSourceExactName       = "exact_name"
	LinkSourceForwarderUnwrap = "forwarder_unwrap"
	LinkSourceJaroWinkler     = "jaro_winkler"
	LinkSourceJaccard         = "jaccard"
	LinkSourceManual          = "manual"
	LinkSourceSingleton       = "singleton"
	LinkSourceManualMerge     = "manual_merge"
)

// Person is one canonical human (or, for non-human clusters that survive
// matching, one canonical entity — downstream filters drop non-humans).
type Person struct {
	ID            int64     `json:"id"`
	CanonicalName string    `json:"canonical_name"`
	PrimaryEmail  string    `json:"primary_email"`
	Note          string    `json:"note,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PersonEmail attaches one email address to a Person, recording how the link
// was made (deterministic / fuzzy / manual) and how confident we are.
type PersonEmail struct {
	EmailAddress string    `json:"email_address"`
	PersonID     int64     `json:"person_id"`
	DisplayName  string    `json:"display_name"`
	LinkSource   string    `json:"link_source"`
	Confidence   float64   `json:"confidence"`
	Locked       bool      `json:"locked"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ResolveOptions controls the matcher pass.
type ResolveOptions struct {
	// JaroThreshold is the minimum Jaro-Winkler score to emit an advisory
	// suggestion. It is never used for automatic linking.
	JaroThreshold float64

	// JaccardThreshold is the minimum Jaccard score (on display-name token
	// sets) to emit an advisory suggestion. It is never used for automatic
	// linking.
	JaccardThreshold float64

	// MinMessagesForFuzzy skips fuzzy advisory suggestions for low-volume
	// clusters to keep noise down.
	MinMessagesForFuzzy int64
}

// DefaultResolveOptions returns conservative advisory-suggestion defaults.
func DefaultResolveOptions() ResolveOptions {
	return ResolveOptions{
		JaroThreshold:       0.92,
		JaccardThreshold:    0.6,
		MinMessagesForFuzzy: 5,
	}
}

// ResolveSuggestion is an advisory duplicate-person signal emitted during
// resolution. Cluster IDs are run-local and are mapped to persisted person IDs
// by later persistence/suggestion-store code.
type ResolveSuggestion struct {
	ClusterA       int      `json:"cluster_a"`
	ClusterB       int      `json:"cluster_b"`
	Sources        []string `json:"sources"`
	NameSimilarity float64  `json:"name_similarity"`
	TokenOverlap   float64  `json:"token_overlap"`
}

// ResolveReport summarizes a matcher run.
type ResolveReport struct {
	GeneratedAt      time.Time           `json:"generated_at"`
	Database         string              `json:"database"`
	ParticipantsSeen int                 `json:"participants_seen"`
	LockedSkipped    int                 `json:"locked_skipped"`
	PersonsTotal     int                 `json:"persons_total"`
	PersonsCreated   int                 `json:"persons_created"`
	EmailsLinked     int                 `json:"emails_linked"`
	BySource         map[string]int      `json:"by_source"`
	Suggestions      []ResolveSuggestion `json:"suggestions,omitempty"`
}
