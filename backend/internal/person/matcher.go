package person

import (
	"context"
	"sort"
	"strings"
	"time"

	"memento/backend/internal/msgvault"
)

// clusterMember is one participant under consideration during a matcher run.
// It carries the originating participant info plus normalized keys we use for
// clustering. LinkSource / Confidence record *why this email* ended up in the
// cluster — they're per-email, not per-cluster, so the audit trail survives
// multiple merge passes.
type clusterMember struct {
	Participant     msgvault.ParticipantForResolution
	NormalizedEmail string
	NormalizedName  string
	NameTokens      []string
	ClusterID       int
	LinkSource      string
	Confidence      float64
}

// cluster groups participants we believe are the same person. ID is a per-run
// integer; it is *not* the persisted person_id. Per-email evidence lives on
// each clusterMember, not here, so the audit trail survives multi-pass merges.
type cluster struct {
	ID      int
	Members []*clusterMember
}

// Resolve runs the deterministic-only matcher. It returns clusters suitable
// for automatic persistence plus advisory suggestions that require review.
// Participants whose email is already locked in memento_person_email are
// skipped here — the caller layers them back in afterwards.
func Resolve(
	ctx context.Context,
	reader *msgvault.Reader,
	locked map[string]bool,
	opts ResolveOptions,
) (ResolveReport, []cluster, error) {
	participants, err := reader.ListParticipantsForResolution(ctx)
	if err != nil {
		return ResolveReport{}, nil, err
	}

	report := ResolveReport{
		GeneratedAt:      time.Now().UTC(),
		Database:         reader.Path(),
		ParticipantsSeen: len(participants),
		BySource:         map[string]int{},
	}

	members := make([]*clusterMember, 0, len(participants))
	for _, p := range participants {
		if locked[strings.ToLower(p.EmailAddress)] {
			report.LockedSkipped++
			continue
		}
		members = append(members, &clusterMember{
			Participant:     p,
			NormalizedEmail: normalizeEmail(p.EmailAddress),
			NormalizedName:  normalizeName(p.DisplayName),
			NameTokens:      displayNameTokens(p.DisplayName),
			ClusterID:       -1,
		})
	}

	clusters := make(map[int]*cluster)
	nextID := 0
	newCluster := func() *cluster {
		nextID++
		c := &cluster{ID: nextID}
		clusters[nextID] = c
		return c
	}
	assign := func(m *clusterMember, c *cluster, source string, confidence float64) {
		m.ClusterID = c.ID
		m.LinkSource = source
		m.Confidence = confidence
		c.Members = append(c.Members, m)
	}

	// Pass 1 — plus-tag clustering. Members sharing a normalized email enter
	// the same cluster. Tag everyone with the empty link source ("no evidence
	// yet"); we re-tag only the members that actually share a cluster after
	// this pass — singletons get re-tagged at persistence time.
	byNormalizedEmail := map[string]*cluster{}
	for _, m := range members {
		if m.ClusterID != -1 {
			continue
		}
		if c, ok := byNormalizedEmail[m.NormalizedEmail]; ok {
			assign(m, c, "", 0)
			continue
		}
		c := newCluster()
		byNormalizedEmail[m.NormalizedEmail] = c
		assign(m, c, "", 0)
	}
	// Promote pass-1 multi-member clusters to plus_tag.
	for _, c := range clusters {
		if len(c.Members) > 1 {
			for _, m := range c.Members {
				m.LinkSource = LinkSourcePlusTag
				m.Confidence = 1.0
			}
		}
	}

	report.Suggestions = collectResolveSuggestions(members, clusters, opts)

	// Materialize a slice ordered by cluster size desc, message volume desc.
	out := make([]cluster, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return clusterVolume(out[i]) > clusterVolume(out[j])
	})

	for _, c := range out {
		singleton := len(c.Members) == 1
		for _, m := range c.Members {
			source := m.LinkSource
			if singleton || source == "" {
				source = LinkSourceSingleton
			}
			report.BySource[source]++
		}
	}
	report.PersonsTotal = len(out)
	report.EmailsLinked = len(members)

	return report, out, nil
}

type resolveSuggestionBuilder struct {
	suggestions map[[2]int]*ResolveSuggestion
}

func collectResolveSuggestions(members []*clusterMember, clusters map[int]*cluster, opts ResolveOptions) []ResolveSuggestion {
	builder := resolveSuggestionBuilder{suggestions: map[[2]int]*ResolveSuggestion{}}

	byLiteralName := map[string][]int{}
	byNormalizedName := map[string][]*clusterMember{}
	for _, m := range members {
		if m.ClusterID <= 0 {
			continue
		}
		literal := normalizeLiteralName(m.Participant.DisplayName)
		if literal != "" && !isGenericName(literal) {
			byLiteralName[literal] = appendUniqueInt(byLiteralName[literal], m.ClusterID)
		}
		if m.NormalizedName != "" && !isGenericName(m.NormalizedName) {
			byNormalizedName[m.NormalizedName] = append(byNormalizedName[m.NormalizedName], m)
		}
	}
	for _, ids := range byLiteralName {
		forEachClusterPair(ids, func(a, b int) {
			builder.add(a, b, LinkSourceExactName, 1, 1)
		})
	}
	for _, group := range byNormalizedName {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a.ClusterID == b.ClusterID {
					continue
				}
				if stripForwarderParenthetical(a.Participant.DisplayName) == "" && stripForwarderParenthetical(b.Participant.DisplayName) == "" {
					continue
				}
				builder.add(a.ClusterID, b.ClusterID, LinkSourceForwarderUnwrap, 1, jaccardTokens(a.NameTokens, b.NameTokens))
			}
		}
	}
	collectFuzzySuggestions(clusters, opts, &builder)

	out := make([]ResolveSuggestion, 0, len(builder.suggestions))
	for _, suggestion := range builder.suggestions {
		sort.Strings(suggestion.Sources)
		out = append(out, *suggestion)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterA != out[j].ClusterA {
			return out[i].ClusterA < out[j].ClusterA
		}
		return out[i].ClusterB < out[j].ClusterB
	})
	return out
}

func collectFuzzySuggestions(clusters map[int]*cluster, opts ResolveOptions, builder *resolveSuggestionBuilder) {
	type clusterKey struct {
		clusterID int
		name      string
		tokens    []string
	}
	var anchors []clusterKey
	for _, c := range clusters {
		if clusterVolume(*c) < opts.MinMessagesForFuzzy {
			continue
		}
		anchor := pickAnchor(c)
		if anchor == nil || anchor.NormalizedName == "" || isGenericName(anchor.NormalizedName) {
			continue
		}
		anchors = append(anchors, clusterKey{
			clusterID: c.ID,
			name:      anchor.NormalizedName,
			tokens:    anchor.NameTokens,
		})
	}

	for i := 0; i < len(anchors); i++ {
		for j := i + 1; j < len(anchors); j++ {
			a, b := anchors[i], anchors[j]
			if a.clusterID == b.clusterID {
				continue
			}
			jw := jaroWinkler(a.name, b.name)
			jc := jaccardTokens(a.tokens, b.tokens)
			if jw >= opts.JaroThreshold {
				builder.add(a.clusterID, b.clusterID, LinkSourceJaroWinkler, jw, jc)
			}
			if jc >= opts.JaccardThreshold {
				builder.add(a.clusterID, b.clusterID, LinkSourceJaccard, jw, jc)
			}
		}
	}
}

func (b *resolveSuggestionBuilder) add(clusterA, clusterB int, source string, nameSimilarity, tokenOverlap float64) {
	if clusterA == clusterB {
		return
	}
	if clusterA > clusterB {
		clusterA, clusterB = clusterB, clusterA
	}
	key := [2]int{clusterA, clusterB}
	suggestion := b.suggestions[key]
	if suggestion == nil {
		suggestion = &ResolveSuggestion{ClusterA: clusterA, ClusterB: clusterB}
		b.suggestions[key] = suggestion
	}
	if !containsString(suggestion.Sources, source) {
		suggestion.Sources = append(suggestion.Sources, source)
	}
	if nameSimilarity > suggestion.NameSimilarity {
		suggestion.NameSimilarity = nameSimilarity
	}
	if tokenOverlap > suggestion.TokenOverlap {
		suggestion.TokenOverlap = tokenOverlap
	}
}

func normalizeLiteralName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ToLower(name)
	name = whitespaceRun.ReplaceAllString(name, " ")
	return strings.TrimSpace(name)
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func forEachClusterPair(ids []int, fn func(a, b int)) {
	sort.Ints(ids)
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			fn(ids[i], ids[j])
		}
	}
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func pickAnchor(c *cluster) *clusterMember {
	if len(c.Members) == 0 {
		return nil
	}
	best := c.Members[0]
	for _, m := range c.Members[1:] {
		if m.Participant.MessageCount > best.Participant.MessageCount {
			best = m
		}
	}
	return best
}

func clusterVolume(c cluster) int64 {
	var sum int64
	for _, m := range c.Members {
		sum += m.Participant.MessageCount
	}
	return sum
}

// isGenericName filters out display names that are too generic to use as a
// clustering key (single-token role names like "admin", "support"). These
// would otherwise merge unrelated participants from different organizations.
func isGenericName(n string) bool {
	if n == "" {
		return true
	}
	if !strings.Contains(n, " ") {
		// single-token display names — too ambiguous on their own.
		return true
	}
	return false
}

// CanonicalNameFor picks the best display name from a cluster. The ranking
// rewards clean human names and penalizes forwarder parentheticals — so
func CanonicalNameFor(c cluster) string {
	var best string
	var bestScore int64 = -1
	for _, m := range c.Members {
		name := strings.TrimSpace(m.Participant.DisplayName)
		// Skip empty names and names that are themselves email addresses
		// (some clients set display_name = email_address).
		if name == "" || strings.Contains(name, "@") {
			continue
		}
		score := int64(len(name)) + m.Participant.MessageCount
		// Forwarder display names (e.g. "X (via Google Photos)") get a hard
		// penalty so the clean variant wins whenever one exists.
		if stripForwarderParenthetical(name) != "" {
			score -= 10000
		}
		if score > bestScore {
			best = name
			bestScore = score
		}
	}
	if best == "" {
		primary := PrimaryEmailFor(c)
		if primary == "" && len(c.Members) > 0 {
			primary = c.Members[0].Participant.EmailAddress
		}
		best = nameFromEmail(primary)
	}
	return normalizeLastFirst(best)
}

// normalizeLastFirst converts "Last, First" format to "First Last".
// Only acts when the name has exactly one comma and a non-empty first part.
func normalizeLastFirst(name string) string {
	idx := strings.Index(name, ",")
	if idx <= 0 {
		return name
	}
	if strings.Contains(name[idx+1:], ",") {
		return name // multiple commas — don't guess
	}
	first := strings.TrimSpace(name[idx+1:])
	last := strings.TrimSpace(name[:idx])
	if first == "" {
		return name
	}
	return first + " " + last
}

// nameFromEmail derives a human-readable name from an email address local-part.
// "kasturi@gmail.com" → "Kasturi", "john.doe@example.com" → "John Doe".
func nameFromEmail(email string) string {
	local := emailLocal(email)
	if isSystemLocalPart(local) {
		return email
	}
	parts := strings.FieldsFunc(local, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '+'
	})
	if len(parts) == 0 {
		return email
	}
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

// PrimaryEmailFor picks the most "human" email from a cluster: prefer
// non-no-reply / non-role-looking local parts, then highest message volume.
func PrimaryEmailFor(c cluster) string {
	var best string
	var bestScore int64 = -1
	for _, m := range c.Members {
		email := m.Participant.EmailAddress
		if email == "" {
			continue
		}
		score := m.Participant.MessageCount
		local := emailLocal(email)
		if !isSystemLocalPart(local) {
			score += 10000
		}
		if score > bestScore {
			best = email
			bestScore = score
		}
	}
	return best
}

func emailLocal(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	return email[:at]
}

func isSystemLocalPart(local string) bool {
	local = strings.ToLower(local)
	for _, marker := range []string{"noreply", "no-reply", "donotreply", "do-not-reply", "auto-reply"} {
		if strings.Contains(local, marker) {
			return true
		}
	}
	return false
}
