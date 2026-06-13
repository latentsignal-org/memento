package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"

	"memento/backend/internal/msgvaultapi"
)

type messageRecipient struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type"`
}

type publicMessageDetail struct {
	MessageID            int64              `json:"message_id"`
	Subject              string             `json:"subject"`
	Snippet              string             `json:"snippet"`
	BodyText             string             `json:"body_text"`
	SentAt               string             `json:"sent_at"`
	FromEmail            string             `json:"from_email"`
	FromName             string             `json:"from_name"`
	ConversationID       int64              `json:"conversation_id"`
	Recipients           []messageRecipient `json:"recipients"`
	SourceMessageID      string             `json:"source_message_id,omitempty"`
	SourceConversationID string             `json:"source_conversation_id,omitempty"`
	SourceType           string             `json:"source_type,omitempty"`
	AccountEmail         string             `json:"account_email,omitempty"`
	ExternalURL          string             `json:"external_url,omitempty"`
}

type msgvaultShowMessage struct {
	BodyText             string `json:"body_text"`
	SourceMessageID      string `json:"source_message_id"`
	SourceConversationID string `json:"source_conversation_id"`
}

func (s *Server) handleGetMessageDetail(w http.ResponseWriter, r *http.Request) {
	messageID, err := parseInt64Path(r.PathValue("id"))
	if err != nil || messageID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid message id"))
		return
	}

	detail, err := loadPublicMessageDetail(r.Context(), s.reader.DB(), messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("message %d not found", messageID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func loadPublicMessageDetail(ctx context.Context, db *sql.DB, messageID int64) (publicMessageDetail, error) {
	var detail publicMessageDetail
	err := db.QueryRowContext(ctx, `
		SELECT
			m.id,
			COALESCE(m.subject, ''),
			COALESCE(m.snippet, ''),
			COALESCE(m.sent_at, ''),
			COALESCE(p.email_address, ''),
			COALESCE(p.display_name, ''),
			COALESCE(m.conversation_id, 0),
			COALESCE(m.source_message_id, ''),
			COALESCE(c.source_conversation_id, ''),
			COALESCE(s.source_type, 'gmail'),
			COALESCE(s.identifier, '')
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN conversations c ON c.id = m.conversation_id
		LEFT JOIN sources s ON s.id = m.source_id
		WHERE m.id = ?
	`, messageID).Scan(
		&detail.MessageID,
		&detail.Subject,
		&detail.Snippet,
		&detail.SentAt,
		&detail.FromEmail,
		&detail.FromName,
		&detail.ConversationID,
		&detail.SourceMessageID,
		&detail.SourceConversationID,
		&detail.SourceType,
		&detail.AccountEmail,
	)
	if err != nil {
		return detail, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			COALESCE(p.email_address, ''),
			COALESCE(NULLIF(mr.display_name, ''), p.display_name, ''),
			COALESCE(mr.recipient_type, '')
		FROM message_recipients mr
		LEFT JOIN participants p ON p.id = mr.participant_id
		WHERE mr.message_id = ?
		  AND mr.recipient_type IN ('to', 'cc', 'bcc')
		ORDER BY
			CASE mr.recipient_type
				WHEN 'to' THEN 0
				WHEN 'cc' THEN 1
				WHEN 'bcc' THEN 2
				ELSE 3
			END,
			COALESCE(p.email_address, ''),
			COALESCE(NULLIF(mr.display_name, ''), p.display_name, '')
	`, messageID)
	if err != nil {
		return detail, err
	}
	defer rows.Close()

	for rows.Next() {
		var recipient messageRecipient
		if err := rows.Scan(&recipient.Email, &recipient.Name, &recipient.Type); err != nil {
			return detail, err
		}
		detail.Recipients = append(detail.Recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return detail, err
	}
	if detail.Recipients == nil {
		detail.Recipients = []messageRecipient{}
	}

	body, sourceMessageID, sourceConversationID, err := loadMessageBodyText(ctx, messageID)
	if err != nil {
		return detail, err
	}
	detail.BodyText = body
	if detail.SourceMessageID == "" {
		detail.SourceMessageID = sourceMessageID
	}
	if detail.SourceConversationID == "" {
		detail.SourceConversationID = sourceConversationID
	}

	detail.ExternalURL = buildExternalMessageURL(detail.SourceType, detail.AccountEmail, detail.SourceMessageID)
	return detail, nil
}

func loadMessageBodyText(ctx context.Context, messageID int64) (bodyText string, sourceMessageID string, sourceConversationID string, err error) {
	if client, ok := msgvaultapi.FromEnv(); ok {
		if msg, apiErr := client.Message(ctx, messageID); apiErr == nil {
			return msg.TextBody(), msg.SourceMessageID, msg.SourceConversationID, nil
		}
	}

	cmd := exec.CommandContext(ctx, "msgvault", "show-message", strconv.FormatInt(messageID, 10), "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", "", fmt.Errorf("msgvault show-message failed for %d: %w (stderr: %s)", messageID, err, stderr.String())
	}

	var payload msgvaultShowMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return "", "", "", fmt.Errorf("decode msgvault show-message for %d: %w", messageID, err)
	}
	return payload.BodyText, payload.SourceMessageID, payload.SourceConversationID, nil
}

func buildExternalMessageURL(sourceType, accountEmail, sourceMessageID string) string {
	if sourceMessageID == "" {
		return ""
	}
	switch sourceType {
	case "", "gmail":
		if accountEmail == "" {
			return "https://mail.google.com/mail/u/0/#all/" + sourceMessageID
		}
		return "https://mail.google.com/mail/u/?authuser=" + url.QueryEscape(accountEmail) + "#all/" + sourceMessageID
	default:
		return ""
	}
}

func parseInt64Path(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}
