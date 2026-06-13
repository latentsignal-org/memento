package people

type PeoplePagesReport struct {
	GeneratedAt string         `json:"generated_at"`
	People      []PagePerson   `json:"people"`
	Counts      map[string]int `json:"counts"`
}

type PagePerson struct {
	PersonID           int64             `json:"person_id"`
	CanonicalName      string            `json:"canonical_name"`
	PrimaryEmail       string            `json:"primary_email"`
	Domain             string            `json:"domain"`
	EmailCount         int64             `json:"email_count"`
	Aliases            []Alias           `json:"aliases"`
	TotalMessages      int64             `json:"total_messages"`
	FromContactCount   int64             `json:"from_contact_count"`
	ToContactCount     int64             `json:"to_contact_count"`
	BidirectionalScore float64           `json:"bidirectional_score"`
	Classification     string            `json:"classification"`
	ExclusionReason    string            `json:"exclusion_reason,omitempty"`
	FirstMessageAt     string            `json:"first_message_at,omitempty"`
	LastMessageAt      string            `json:"last_message_at,omitempty"`
	Slug               string            `json:"slug,omitempty"`
	Timeline           []TimelineEntry   `json:"timeline"`
	TopCorrespondents  []Correspondent   `json:"top_correspondents,omitempty"`
	Facets             []PersonFacet     `json:"facets,omitempty"`
	Narrative          PersonNarrative   `json:"narrative,omitempty"`
	Attributes         []PersonAttribute `json:"attributes,omitempty"`
	// Social graph fields — joined at read time from memento_social_metric/cluster.
	StructuralRole string  `json:"structural_role,omitempty"`
	ClusterID      *int64  `json:"cluster_id,omitempty"`
	ClusterLabel   string  `json:"cluster_label,omitempty"`
	DormancyDays   *int64  `json:"dormancy_days,omitempty"`
	WeightedDegree float64 `json:"weighted_degree,omitempty"`
}

type Alias struct {
	EmailAddress string `json:"email_address"`
	DisplayName  string `json:"display_name"`
	LinkSource   string `json:"link_source"`
	Locked       bool   `json:"locked"`
}

type TimelineEntry struct {
	Date      string `json:"date"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet"`
	MessageID int64  `json:"message_id"`
	Direction string `json:"direction"`
	ViaEmail  string `json:"via_email"`
}

type Correspondent struct {
	PersonID      int64  `json:"person_id"`
	CanonicalName string `json:"canonical_name"`
	PrimaryEmail  string `json:"primary_email"`
	SharedCount   int64  `json:"shared_count"`
}

type PersonFacet struct {
	ID               int64   `json:"id"`
	FacetType        string  `json:"facet_type"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	Confidence       float64 `json:"confidence"`
	EditedBy         string  `json:"edited_by"`
	GeneratedAt      string  `json:"generated_at"`
}

type PersonAttribute struct {
	ID               int64   `json:"id"`
	Category         string  `json:"category"`
	Label            string  `json:"label"`
	Value            string  `json:"value"`
	DateValue        string  `json:"date_value,omitempty"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	Confidence       float64 `json:"confidence"`
	EditedBy         string  `json:"edited_by"`
	GeneratedAt      string  `json:"generated_at"`
}

type NarrativeSection struct {
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	EditedBy         string  `json:"edited_by,omitempty"`
	GeneratedAt      string  `json:"generated_at,omitempty"`
}

type PersonNarrative struct {
	Summary         NarrativeSection `json:"summary,omitempty"`
	RelationshipArc NarrativeSection `json:"relationship_arc,omitempty"`
	CurrentStatus   NarrativeSection `json:"current_status,omitempty"`
}
