package newsletter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"memento/backend/internal/llm"
)

func GenerateNarrative(ctx context.Context, db *sql.DB, slug string, messageLimit int) error {
	started := time.Now()
	source, err := SourceBySlug(ctx, db, slug)
	if err != nil {
		return fmt.Errorf("lookup newsletter source: %w", err)
	}
	loadStarted := time.Now()
	messages, err := RecentMessages(ctx, db, source, messageLimit, 1500)
	if err != nil {
		return fmt.Errorf("load newsletter messages: %w", err)
	}
	if len(messages) == 0 {
		return fmt.Errorf("no messages found for newsletter source %s", slug)
	}
	log.Printf("[newsletter] loaded prompt messages slug=%s source_id=%d messages=%d duration=%s", slug, source.ID, len(messages), time.Since(loadStarted))

	var bundle bytes.Buffer
	for _, msg := range messages {
		bundle.WriteString(fmt.Sprintf("Message ID: %d\n", msg.MessageID))
		bundle.WriteString(fmt.Sprintf("Date: %s\n", msg.SentAt))
		bundle.WriteString(fmt.Sprintf("Subject: %s\n", msg.Subject))
		bundle.WriteString(fmt.Sprintf("Snippet: %s\n", msg.Snippet))
		bundle.WriteString(fmt.Sprintf("Body Preview: %s\n", msg.BodyText))
		bundle.WriteString("\n---\n\n")
	}

	systemPrompt := `You are a factual newsletter coverage analyst for Memento.
You summarize what one newsletter source has covered over time using only supplied email messages.

CRITICAL RULES:
1. Every factual sentence in coverage_summary MUST end with one or more inline citations in the exact form [msg:<id>] or [msg:<id>, msg:<id>].
2. If a claim cannot cite one of the supplied message IDs, omit it.
3. Never invent dates, names, article titles, products, companies, or dollar amounts.
4. recurring_themes and notable_recent source_message_ids must only contain supplied message IDs.
5. Keep coverage_summary concise: 4-7 sentences.
6. Avoid repeating the same claim across coverage_summary, recurring_themes, and notable_recent.
7. Prefer concrete language over abstract analysis framing.
8. notable_recent should prioritize the most recent, materially distinct items in the supplied messages.
9. Output strictly valid JSON only. Do not include markdown fences, commentary, or any text outside the JSON object.

Required JSON shape:
{
  "coverage_summary": "4-7 cited factual sentences",
  "recurring_themes": [
    {"theme": "theme label", "source_message_ids": [123]}
  ],
  "notable_recent": [
    {"headline": "specific recent item", "date": "YYYY-MM-DD or supplied date string", "source_message_ids": [123]}
  ]
}`

	userPrompt := fmt.Sprintf("Newsletter source: %s <%s>\n\nMessages:\n\n%s", source.DisplayName, source.SenderEmail, bundle.String())

	llmReq := llm.OneShotRequest{
		System: systemPrompt,
		Prompt: strings.ToValidUTF8(userPrompt, ""),
	}
	config, err := llm.ResolveConfig(llmReq)
	if err != nil {
		return fmt.Errorf("resolve newsletter model config: %w", err)
	}
	llmStarted := time.Now()
	log.Printf("[newsletter] LLM request started slug=%s source_id=%d model=%s messages=%d prompt_bytes=%d", slug, source.ID, config.Model, len(messages), len(userPrompt))
	resp, err := llm.OneShot(ctx, llmReq)
	if err != nil {
		return fmt.Errorf("model newsletter generation: %w", err)
	}
	log.Printf("[newsletter] LLM response received slug=%s model=%s duration=%s response_bytes=%d", slug, resp.Model, time.Since(llmStarted), len(resp.Text))
	raw := cleanNarrativeJSON(resp.Text)

	var narrative Narrative
	if err := json.Unmarshal([]byte(raw), &narrative); err != nil {
		return fmt.Errorf("parse newsletter narrative JSON: %w (raw response: %s)", err, raw)
	}
	narrative.CoverageSummary = normalizeCoverageSummaryCitations(narrative.CoverageSummary)
	narrative.CoverageSummary = sanitizeCoverageSummary(narrative.CoverageSummary)
	if err := validateNarrative(narrative, messages); err != nil {
		return err
	}
	if err := SaveNarrative(ctx, db, source.ID, narrative); err != nil {
		return err
	}
	log.Printf("[newsletter] bundle generated successfully slug=%s source_id=%d source=%q messages=%d themes=%d notable_recent=%d duration=%s", source.Slug, source.ID, source.DisplayName, len(messages), len(narrative.RecurringThemes), len(narrative.NotableRecent), time.Since(started))
	return nil
}

func cleanNarrativeJSON(text string) string {
	raw := strings.TrimSpace(text)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if json.Valid([]byte(raw)) {
		return raw
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(raw[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return raw
}

func validateNarrative(n Narrative, messages []Message) error {
	allowed := map[int64]bool{}
	for _, msg := range messages {
		allowed[msg.MessageID] = true
	}
	if err := validateEverySentenceCited(n.CoverageSummary); err != nil {
		return err
	}
	for _, id := range extractCitations(n.CoverageSummary) {
		if !allowed[id] {
			return fmt.Errorf("coverage_summary cites message %d outside prompt bundle", id)
		}
	}
	for _, theme := range n.RecurringThemes {
		for _, id := range theme.SourceMessageIDs {
			if !allowed[id] {
				return fmt.Errorf("theme %q cites message %d outside prompt bundle", theme.Theme, id)
			}
		}
	}
	for _, item := range n.NotableRecent {
		for _, id := range item.SourceMessageIDs {
			if !allowed[id] {
				return fmt.Errorf("notable item %q cites message %d outside prompt bundle", item.Headline, id)
			}
		}
	}
	return nil
}

func validateEverySentenceCited(text string) error {
	for _, sentence := range splitSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if !msgIDRE.MatchString(sentence) {
			return fmt.Errorf("coverage_summary has uncited sentence: %q", sentence)
		}
	}
	return nil
}

func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i, r := range text {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if !looksLikeSentenceBoundary(text, i, i+len(string(r))) {
			continue
		}
		end := i + len(string(r))
		sentences = append(sentences, text[start:end])
		start = end
	}
	if start < len(text) {
		sentences = append(sentences, text[start:])
	}
	return sentences
}

func looksLikeSentenceBoundary(text string, punctIndex int, after int) bool {
	if rune(text[punctIndex]) == '.' && hasKnownAbbreviationBefore(text, punctIndex) {
		return false
	}
	if after >= 2 {
		prev := rune(text[after-2])
		if unicode.IsUpper(prev) {
			return false
		}
	}
	for after < len(text) {
		r := rune(text[after])
		if !unicode.IsSpace(r) {
			return unicode.IsUpper(r) || unicode.IsDigit(r)
		}
		after++
	}
	return true
}

func hasKnownAbbreviationBefore(text string, punctIndex int) bool {
	start := punctIndex
	for start > 0 {
		r := rune(text[start-1])
		if !unicode.IsLetter(r) {
			break
		}
		start--
	}
	if start == punctIndex {
		return false
	}
	token := strings.ToLower(text[start:punctIndex])
	_, ok := sentenceAbbreviations[token]
	return ok
}

func sanitizeCoverageSummary(text string) string {
	rawSentences := splitSentences(text)
	merged := make([]string, 0, len(rawSentences))
	for _, sentence := range rawSentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if citationOnlyRE.MatchString(sentence) && len(merged) > 0 {
			merged[len(merged)-1] = strings.TrimSpace(merged[len(merged)-1] + " " + sentence)
			continue
		}
		merged = append(merged, sentence)
	}

	clean := make([]string, 0, len(merged))
	for _, sentence := range merged {
		if msgIDRE.MatchString(sentence) {
			clean = append(clean, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(clean, " "))
}

func extractCitations(text string) []int64 {
	matches := msgIDRE.FindAllStringSubmatch(text, -1)
	seen := map[int64]bool{}
	var ids []int64
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func normalizeCoverageSummaryCitations(text string) string {
	return citationGroupRE.ReplaceAllStringFunc(text, func(group string) string {
		ids := numericCitationRE.FindAllString(group, -1)
		if len(ids) == 0 {
			return group
		}
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, "msg:"+id)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	})
}

var msgIDRE = regexp.MustCompile(`msg:(\d+)`)
var citationGroupRE = regexp.MustCompile(`\[\s*(?:msg:\s*)?\d+(?:\s*[,;]\s*(?:msg:\s*)?\d+)*\s*\]`)
var citationOnlyRE = regexp.MustCompile(`^(?:\s*\[\s*(?:msg:\s*)?\d+(?:\s*[,;]\s*(?:msg:\s*)?\d+)*\s*\]\s*)+$`)
var numericCitationRE = regexp.MustCompile(`\d+`)
var sentenceAbbreviations = map[string]struct{}{
	"dr":   {},
	"mr":   {},
	"mrs":  {},
	"ms":   {},
	"prof": {},
	"sr":   {},
	"jr":   {},
	"st":   {},
	"vs":   {},
}
