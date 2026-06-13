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
	LinkSourceSignatureMerge  = "signature_merge"
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
	// IncludeFuzzy turns on the Jaro-Winkler + Jaccard second pass. Off by
	// default because fuzzy matches are inherently lower-confidence and the
	// deterministic pass already covers Phase-0 needs.
	IncludeFuzzy bool

	// JaroThreshold is the minimum Jaro-Winkler score to accept a fuzzy link.
	// 0.92 keeps obvious typos and middle-name variations while rejecting
	// most coincidental name overlaps. Ignored when IncludeFuzzy is false.
	JaroThreshold float64

	// JaccardThreshold is the minimum Jaccard score (on display-name token
	// sets) to accept a fuzzy link. Ignored when IncludeFuzzy is false.
	JaccardThreshold float64

	// MinMessagesForFuzzy skips the fuzzy pass for low-volume participants
	// to keep noise down — fuzzy matches on a single-message contact are
	// almost never worth the false-positive risk.
	MinMessagesForFuzzy int64
}

// DefaultResolveOptions returns the Phase-0 defaults.
func DefaultResolveOptions() ResolveOptions {
	return ResolveOptions{
		IncludeFuzzy:        false,
		JaroThreshold:       0.92,
		JaccardThreshold:    0.6,
		MinMessagesForFuzzy: 5,
	}
}

// ResolveReport summarizes a matcher run.
type ResolveReport struct {
	GeneratedAt      time.Time      `json:"generated_at"`
	Database         string         `json:"database"`
	ParticipantsSeen int            `json:"participants_seen"`
	LockedSkipped    int            `json:"locked_skipped"`
	PersonsTotal     int            `json:"persons_total"`
	PersonsCreated   int            `json:"persons_created"`
	EmailsLinked     int            `json:"emails_linked"`
	BySource         map[string]int `json:"by_source"`
}
