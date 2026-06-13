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

// Resolve runs the deterministic-first matcher and (optionally) the fuzzy
// second pass. It returns a slice of clusters; persistence is the caller's
// job. Participants whose email is already locked in memento_person_email are
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

	// Pass 2 — exact normalized display name. "Jane Smith (via Google
	// Photos)" and "Jane Smith" both normalize to "jane smith"
	// and merge here. Single-token names ("admin", "support") are skipped —
	// see isGenericName. Members brought in via name match are re-tagged as
	// exact_name; the absorbing cluster's original members keep their tags.
	byName := map[string]int{}
	for _, m := range members {
		if m.NormalizedName == "" || isGenericName(m.NormalizedName) {
			continue
		}
		current := clusters[m.ClusterID]
		if current == nil {
			continue
		}
		existingID, seen := byName[m.NormalizedName]
		if !seen {
			byName[m.NormalizedName] = current.ID
			continue
		}
		existing := clusters[existingID]
		if existing == nil {
			byName[m.NormalizedName] = current.ID
			continue
		}
		if existing.ID == current.ID {
			continue
		}
		mergeClusters(clusters, existing, current, LinkSourceExactName, 0.95)
		byName[m.NormalizedName] = existing.ID
	}

	// Pass 2b — forwarder unwrap. Any member whose original display name
	// carried a forwarder parenthetical ("X (via Google Photos)") gets its
	// per-member source re-tagged. This applies regardless of how the member
	// joined its cluster — the evidence describes the address, not the merge.
	for _, m := range members {
		original := strings.TrimSpace(m.Participant.DisplayName)
		if original == "" {
			continue
		}
		if stripForwarderParenthetical(original) == "" {
			continue
		}
		m.LinkSource = LinkSourceForwarderUnwrap
		m.Confidence = 0.95
	}

	// Pass 3 — optional fuzzy pass. Only on members above the message-volume
	// floor, and only against the current per-cluster canonical name.
	if opts.IncludeFuzzy {
		runFuzzyPass(members, clusters, opts)
	}

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

func runFuzzyPass(members []*clusterMember, clusters map[int]*cluster, opts ResolveOptions) {
	// Build per-cluster canonical names from highest-volume member.
	type clusterKey struct {
		clusterID int
		name      string
		tokens    []string
		domain    string
	}
	var anchors []clusterKey
	for _, c := range clusters {
		// Skip clusters whose total volume is below the floor — fuzzy on
		// low-volume contacts is the main source of false positives.
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
			domain:    anchor.Participant.Domain,
		})
	}

	// For every unmerged anchor pair, decide whether to merge.
	for i := 0; i < len(anchors); i++ {
		for j := i + 1; j < len(anchors); j++ {
			a, b := anchors[i], anchors[j]
			if a.clusterID == b.clusterID {
				continue
			}
			// Same domain is a weak signal — fuzzy on the local part of a
			// shared corporate domain produces noisy merges. Require cross-
			// domain evidence.
			if a.domain != "" && a.domain == b.domain {
				continue
			}
			jw := jaroWinkler(a.name, b.name)
			jc := jaccardTokens(a.tokens, b.tokens)
			var source string
			var confidence float64
			switch {
			case jw >= opts.JaroThreshold:
				source = LinkSourceJaroWinkler
				confidence = jw
			case jc >= opts.JaccardThreshold:
				source = LinkSourceJaccard
				confidence = jc
			default:
				continue
			}
			ca := clusters[a.clusterID]
			cb := clusters[b.clusterID]
			if ca == nil || cb == nil {
				continue
			}
			mergeClusters(clusters, ca, cb, source, confidence)
			// Anchors[i] / Anchors[j] are now stale; rebuilding mid-loop is
			// expensive and Phase-0 fuzzy is single-shot, so just continue.
		}
	}
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

// mergeClusters folds `drop` into `keep`. Both sides' members are re-tagged
// with the merge's evidence — the act of merging is symmetric, so the anchor
// shouldn't keep a weaker tag than the addresses joining it. Only overwrite
// when the merge evidence outranks the current tag; never downgrade.
func mergeClusters(all map[int]*cluster, keep, drop *cluster, source string, confidence float64) {
	if keep == nil || drop == nil || keep == drop {
		return
	}
	upgrade := func(m *clusterMember) {
		if linkSourceRank(source) > linkSourceRank(m.LinkSource) {
			m.LinkSource = source
			m.Confidence = confidence
		}
	}
	for _, m := range keep.Members {
		upgrade(m)
	}
	for _, m := range drop.Members {
		m.ClusterID = keep.ID
		upgrade(m)
		keep.Members = append(keep.Members, m)
	}
	delete(all, drop.ID)
}

// linkSourceRank decides which evidence wins when two signals fire on the
// same member. Manual always wins; forwarder_unwrap is the most specific
// deterministic signal (the display name literally says "X via Y"); plus_tag
// is next because it's identity-strong on the normalized email; exact_name
// is a weaker deterministic match; fuzzy signals come last; the empty string
// means "no evidence yet" and any signal upgrades it.
func linkSourceRank(source string) int {
	switch source {
	case LinkSourceManual:
		return 100
	case LinkSourceForwarderUnwrap:
		return 80
	case LinkSourcePlusTag:
		return 70
	case LinkSourceExactName:
		return 60
	case LinkSourceJaroWinkler:
		return 50
	case LinkSourceJaccard:
		return 40
	case LinkSourceSingleton:
		return 10
	default:
		return 0 // "" — no evidence
	}
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
