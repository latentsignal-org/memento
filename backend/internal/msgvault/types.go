package msgvault

type Stats struct {
	Messages          int64 `json:"messages"`
	Conversations     int64 `json:"conversations"`
	Participants      int64 `json:"participants"`
	MessageRecipients int64 `json:"message_recipients"`
	Labels            int64 `json:"labels"`
	Attachments       int64 `json:"attachments"`
	Sources           int64 `json:"sources"`
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	PK       bool   `json:"pk"`
}

type Message struct {
	ID              int64   `json:"id"`
	ConversationID  int64   `json:"conversation_id"`
	SourceID        int64   `json:"source_id"`
	SourceMessageID *string `json:"source_message_id,omitempty"`
	MessageType     string  `json:"message_type"`
	SentAt          *string `json:"sent_at,omitempty"`
	SenderID        *int64  `json:"sender_id,omitempty"`
	IsFromMe        bool    `json:"is_from_me"`
	Subject         *string `json:"subject,omitempty"`
	Snippet         *string `json:"snippet,omitempty"`
	HasAttachments  bool    `json:"has_attachments"`
	AttachmentCount int64   `json:"attachment_count"`
}

type Participant struct {
	ID           int64   `json:"id"`
	EmailAddress *string `json:"email_address,omitempty"`
	PhoneNumber  *string `json:"phone_number,omitempty"`
	DisplayName  *string `json:"display_name,omitempty"`
	Domain       *string `json:"domain,omitempty"`
	CanonicalID  *string `json:"canonical_id,omitempty"`
}

type Recipient struct {
	ID            int64   `json:"id"`
	MessageID     int64   `json:"message_id"`
	ParticipantID int64   `json:"participant_id"`
	RecipientType string  `json:"recipient_type"`
	DisplayName   *string `json:"display_name,omitempty"`
}

type Conversation struct {
	ID               int64   `json:"id"`
	SourceID         int64   `json:"source_id"`
	SourceIDExternal *string `json:"source_conversation_id,omitempty"`
	ConversationType string  `json:"conversation_type"`
	Title            *string `json:"title,omitempty"`
	ParticipantCount int64   `json:"participant_count"`
	MessageCount     int64   `json:"message_count"`
	LastMessageAt    *string `json:"last_message_at,omitempty"`
}

type Label struct {
	ID            int64   `json:"id"`
	SourceID      *int64  `json:"source_id,omitempty"`
	SourceLabelID *string `json:"source_label_id,omitempty"`
	Name          string  `json:"name"`
	LabelType     *string `json:"label_type,omitempty"`
	Color         *string `json:"color,omitempty"`
}

type Attachment struct {
	ID          int64   `json:"id"`
	MessageID   int64   `json:"message_id"`
	Filename    *string `json:"filename,omitempty"`
	MimeType    *string `json:"mime_type,omitempty"`
	Size        *int64  `json:"size,omitempty"`
	ContentHash *string `json:"content_hash,omitempty"`
	StoragePath string  `json:"storage_path"`
}

// ParticipantForResolution is the input shape consumed by the person resolver.
// Lower-cased email is used as the canonical key; display name is the primary
// matching signal; message_count is for tie-breaking and primary-email pick.
type ParticipantForResolution struct {
	ID           int64  `json:"id"`
	EmailAddress string `json:"email_address"`
	DisplayName  string `json:"display_name"`
	Domain       string `json:"domain"`
	MessageCount int64  `json:"message_count"`
}

// PeopleCandidateRow is the raw shape returned by the msgvault adapter for
// candidate scoring. It is keyed on a canonical memento_person row — every
// field aggregates across all of the person's mapped emails. Scoring
// (bidirectional ratio etc.) is computed in the `people` package — keep
// derived fields off this struct.
type PeopleCandidateRow struct {
	PersonID            int64   `json:"person_id"`
	CanonicalName       string  `json:"canonical_name"`
	PrimaryEmail        string  `json:"primary_email"`
	Domain              string  `json:"domain"`
	EmailCount          int64   `json:"email_count"`
	TotalMessages       int64   `json:"total_messages"`
	FromContactCount    int64   `json:"from_contact_count"`
	ToContactCount      int64   `json:"to_contact_count"`
	FirstMessageAt      *string `json:"first_message_at,omitempty"`
	LastMessageAt       *string `json:"last_message_at,omitempty"`
	SampleMessageIDsRaw *string `json:"-"`
	SampleMessageIDs    []int64 `json:"sample_message_ids"`
}
