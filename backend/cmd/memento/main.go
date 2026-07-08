package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"memento/backend/internal/avatar"
	"memento/backend/internal/concept"
	"memento/backend/internal/config"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/person"
	"memento/backend/internal/project"
	"memento/backend/internal/refresh"
	"memento/backend/internal/server"
	"memento/backend/internal/social"
	"memento/backend/internal/store"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if !errors.Is(err, errDoctorFailed) {
			fmt.Fprint(os.Stderr, formatCLIError(err))
		}
		os.Exit(1)
	}
}

func formatCLIError(err error) string {
	msg := fmt.Sprintf("ERROR: %v\n", err)
	if !supportsANSI(os.Stderr) {
		return msg
	}
	return "\033[1;37;41m ERROR \033[0m \033[31;1m" + strings.TrimSuffix(msg, "\n") + "\033[0m\n"
}

func supportsANSI(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if (info.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	term := strings.TrimSpace(os.Getenv("TERM"))
	return term != "" && term != "dumb"
}

func run(ctx context.Context, args []string) error {
	config.LoadDotEnv()

	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "init":
		return runInit(ctx, args[1:])
	case "doctor":
		return runDoctor(ctx, args[1:])
	case "onboard":
		return runOnboard(ctx, args[1:])
	case "reset":
		return runReset(ctx, args[1:])
	case "stats":
		return runStats(ctx, args[1:])
	case "inspect-schema":
		return runInspectSchema(ctx, args[1:])
	case "migrate":
		return runMigrate(ctx, args[1:])
	case "people-candidates":
		return runPeopleCandidates(ctx, args[1:])
	case "person-resolve":
		return runPersonResolve(ctx, args[1:])
	case "person-show":
		return runPersonShow(ctx, args[1:])
	case "person-link":
		return runPersonLink(ctx, args[1:])
	case "person-split":
		return runPersonSplit(ctx, args[1:])
	case "person-repair-nondeterministic":
		return runPersonRepairNonDeterministic(ctx, args[1:])
	case "person-merge-suggest":
		return runPersonMergeSuggest(ctx, args[1:])
	case "person-merge":
		return runPersonMerge(ctx, args[1:])
	case "project":
		return runProject(ctx, args[1:])
	case "project-pages":
		return runProjectPages(ctx, args[1:])
	case "newsletter-detect":
		return runNewsletterDetect(ctx, args[1:])
	case "newsletter-generate":
		return runNewsletterGenerate(ctx, args[1:])
	case "newsletter-pages":
		return runNewsletterPages(ctx, args[1:])
	case "concept":
		return runConcept(ctx, args[1:])
	case "concept-pages":
		return runConceptPages(ctx, args[1:])
	case "refresh":
		return runRefresh(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	case "app":
		return runApp(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Print(`memento is the local backend for deriving Memento artifacts from msgvault.

Usage:
  memento <command> [flags]

Setup:
  init                  Interactive setup — resolves people, classifies, builds the graph
  onboard --demo        Prepare an unonboarded synthetic archive and start the local API
  onboard mark-complete Flip onboarding_status to "complete" without rerunning init
  doctor                Check prerequisites, archive health, and configuration
  reset                 Drop all Memento tables (msgvault archive data is untouched)

Identity pipeline (run by init; re-run individually after archive changes):
  person-resolve      Cluster participants into canonical persons
  people-candidates   Classify persons (human / weak / excluded bots)
  newsletter-detect   Detect newsletter sources from msgvault messages
  refresh             Rebuild materialized rollup tables + social graph; use --avatars for cached avatars

Review & dedup:
  person-merge-suggest List persisted duplicate-person review suggestions
  person-merge        Merge one person into another (transfers emails, facets, notes)

Manual person edits (locked overrides; run refresh afterwards):
  person-show         Show a person and all their linked emails
  person-link         Manually link an email to an existing person
  person-split        Split an email off into a brand-new person
  person-repair-nondeterministic
                       Dry-run/apply repair for legacy unsafe resolver links

Other dimensions:
  project             Manage tracked projects
  project-pages       Export a project page JSON file
  newsletter-generate Generate cited coverage summary for newsletter source(s)
  newsletter-pages    Export newsletter pages to JSON files
  concept             Manage opt-in concept knowledge pages
  concept-pages       Export concept pages (one JSON per concept + index)

Run:
  app                 Start Memento and open the web UI in your browser
  app --demo          Same, against a populated synthetic demo archive
  app --no-open       Start without launching the browser

Diagnostics:
  stats               Show basic msgvault archive counts
  inspect-schema      List tables and columns in the msgvault database
  migrate             Create or update Memento-owned memento_* tables
  serve               Start the HTTP server (localhost only): API + bundled web UI
  serve --demo        Prepare a populated synthetic demo archive and serve it

Common flags:
  --db PATH           Override msgvault SQLite database path
  --json              Emit JSON where supported
`)
}

func openReader(dbPath string) (*msgvault.Reader, error) {
	if dbPath == "" {
		var err error
		dbPath, err = config.ResolveMsgvaultDBPath()
		if err != nil {
			return nil, err
		}
	}
	return msgvault.OpenReader(dbPath)
}

func runOnboard(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "mark-complete" {
		return runOnboardMarkComplete(ctx, args[1:])
	}

	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	demo := fs.Bool("demo", false, "prepare a rich unonboarded demo archive and start the API")
	dbPath := fs.String("db", "", "onboarding demo SQLite database path")
	port := fs.Int("port", config.DefaultBackendPort, "TCP port to listen on (127.0.0.1 only)")
	prepareOnly := fs.Bool("prepare-only", false, "prepare the onboarding demo database without starting the API")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*demo {
		return fmt.Errorf("memento onboard currently supports only --demo or mark-complete")
	}
	return runDemoMode(ctx, demoModeOptions{
		DBPath:      *dbPath,
		Port:        *port,
		PrepareOnly: *prepareOnly,
		Onboarding:  true,
	})
}

// runOnboardMarkComplete flips the onboarding_status flag to "complete" without
// running the destructive init pipeline. For archives that were initialized by
// an earlier process (or hand-curated) where re-running onboarding would clobber
// user edits to narratives and reports.
func runOnboardMarkComplete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("onboard mark-complete", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	force := fs.Bool("force", false, "skip the owner-config sanity check")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, resolvedPath, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if status, _ := store.GetConfig(ctx, db, "onboarding_status"); strings.TrimSpace(status) == "complete" {
		completedAt, _ := store.GetConfig(ctx, db, "onboarding_completed_at")
		fmt.Printf("Onboarding is already marked complete (at %s).\n", completedAt)
		fmt.Printf("Database: %s\n", resolvedPath)
		return nil
	}

	if !*force {
		ownerEmail, _ := store.GetConfig(ctx, db, "owner_email")
		if strings.TrimSpace(ownerEmail) == "" {
			return fmt.Errorf("owner_email is not set in memento_config — this archive does not look initialized.\n" +
				"Re-run with --force to mark it complete anyway, or run the onboarding wizard first")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.SetConfig(ctx, db, "onboarding_status", "complete"); err != nil {
		return fmt.Errorf("save onboarding status: %w", err)
	}
	if err := store.SetConfig(ctx, db, "onboarding_completed_at", now); err != nil {
		return fmt.Errorf("save onboarding completion time: %w", err)
	}

	fmt.Printf("Marked onboarding complete at %s.\n", now)
	fmt.Printf("Database: %s\n", resolvedPath)
	fmt.Println("Next: `./memento serve` will now route to Home instead of /onboard.")
	return nil
}

func runStats(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	stats, err := reader.Stats(ctx)
	if err != nil {
		return err
	}

	if *jsonOut {
		return writeJSON(stats)
	}

	fmt.Printf("Database:     %s\n", reader.Path())
	fmt.Printf("Messages:     %d\n", stats.Messages)
	fmt.Printf("Conversations:%d\n", stats.Conversations)
	fmt.Printf("Participants: %d\n", stats.Participants)
	fmt.Printf("Recipients:   %d\n", stats.MessageRecipients)
	fmt.Printf("Labels:       %d\n", stats.Labels)
	fmt.Printf("Attachments:  %d\n", stats.Attachments)
	fmt.Printf("Sources:      %d\n", stats.Sources)
	return nil
}

func runInspectSchema(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inspect-schema", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	tables, err := reader.Schema(ctx)
	if err != nil {
		return err
	}

	if *jsonOut {
		return writeJSON(tables)
	}

	for _, table := range tables {
		fmt.Printf("%s\n", table.Name)
		for _, col := range table.Columns {
			nullable := "not null"
			if col.Nullable {
				nullable = "nullable"
			}
			fmt.Printf("  %-28s %-18s %s\n", col.Name, col.Type, nullable)
		}
	}
	return nil
}

func runMigrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		var err error
		*dbPath, err = config.ResolveMsgvaultDBPath()
		if err != nil {
			return err
		}
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	applied, err := store.Migrate(ctx, db)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Println("Memento schema already up to date.")
		return nil
	}
	fmt.Println("Applied migrations:")
	for _, migration := range applied {
		fmt.Printf("  %03d %s\n", migration.Version, migration.Name)
	}
	return nil
}

func runPeopleCandidates(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("people-candidates", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	limit := fs.Int("limit", 25, "maximum number of candidates")
	includeExcluded := fs.Bool("include-excluded", false, "include contacts classified as excluded")
	jsonOut := fs.Bool("json", false, "emit JSON")
	persist := fs.Bool("persist", false, "write report to memento_people_candidates")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	// --persist writes the canonical classified ledger: the full identity
	// universe, uncapped, with excluded rows included. The display path
	// (no --persist) keeps the top-N limit for a readable report.
	report, err := people.BuildCandidateReport(ctx, reader, people.CandidateOptions{
		Limit:           *limit,
		IncludeExcluded: *includeExcluded,
		Full:            *persist,
	})
	if err != nil {
		return err
	}

	if *persist {
		db, err := store.Open(reader.Path())
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err := store.Migrate(ctx, db); err != nil {
			return err
		}
		if err := people.PersistCandidateReport(ctx, db, report); err != nil {
			return err
		}
		// Summarize rather than dumping the full ledger (thousands of rows).
		byClass := map[string]int{}
		for _, c := range report.Candidates {
			byClass[c.Classification]++
		}
		fmt.Printf("Persisted %d classified persons to memento_people_candidates.\n", len(report.Candidates))
		fmt.Printf("  candidate=%d  weak_signal=%d  candidate_inbound_only=%d  excluded=%d\n",
			byClass["candidate"], byClass["weak_signal"], byClass["candidate_inbound_only"], byClass["excluded"])
		return nil
	}

	if *jsonOut {
		return writeJSON(report)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PERSON\tPRIMARY EMAIL\tEMAILS\tTOTAL\tFROM\tTO\tBIDIR\tCLASSIFICATION\tREASON\tSAMPLES")
	for _, candidate := range report.Candidates {
		fmt.Fprintf(
			w,
			"%s\t%s\t%d\t%d\t%d\t%d\t%.2f\t%s\t%s\t%v\n",
			candidate.CanonicalName,
			candidate.PrimaryEmail,
			candidate.EmailCount,
			candidate.TotalMessages,
			candidate.FromContactCount,
			candidate.ToContactCount,
			candidate.BidirectionalScore,
			candidate.Classification,
			candidate.ExclusionReason,
			candidate.SampleMessageIDs,
		)
	}
	return w.Flush()
}

func writeJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func openMementoStore(dbPath string) (*sql.DB, string, error) {
	if dbPath == "" {
		var err error
		dbPath, err = config.ResolveMsgvaultDBPath()
		if err != nil {
			return nil, "", err
		}
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return nil, "", err
	}
	return db, dbPath, nil
}

func runPersonResolve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-resolve", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	includeFuzzy := fs.Bool("fuzzy", false, "deprecated no-op; fuzzy evidence is advisory only")
	persist := fs.Bool("persist", false, "write results to memento_person / memento_person_email")
	jsonOut := fs.Bool("json", false, "emit JSON")
	jaroThreshold := fs.Float64("jaro", 0.92, "Jaro-Winkler threshold for advisory suggestions")
	jaccardThreshold := fs.Float64("jaccard", 0.6, "Jaccard threshold for advisory suggestions")
	minMessages := fs.Int64("min-messages", 5, "skip fuzzy advisory suggestions below this message count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *includeFuzzy {
		fmt.Fprintln(os.Stderr, "Warning: --fuzzy is deprecated and no longer affects automatic person linking; fuzzy evidence is advisory only.")
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	db, _, err := openMementoStore(reader.Path())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	opts := person.DefaultResolveOptions()
	opts.JaroThreshold = *jaroThreshold
	opts.JaccardThreshold = *jaccardThreshold
	opts.MinMessagesForFuzzy = *minMessages

	if *persist {
		report, err := person.ResolveAndPersist(ctx, reader, db, opts)
		if err != nil {
			return err
		}
		return writePersonResolveReport(report, reader.Path(), *persist, *jsonOut)
	}

	locked, err := person.LoadLockedEmails(ctx, db)
	if err != nil {
		return err
	}
	report, _, err := person.Resolve(ctx, reader, locked, opts)
	if err != nil {
		return err
	}
	return writePersonResolveReport(report, reader.Path(), *persist, *jsonOut)
}

func writePersonResolveReport(report person.ResolveReport, dbPath string, persisted bool, jsonOut bool) error {
	if jsonOut {
		return writeJSON(report)
	}

	fmt.Printf("Database:           %s\n", dbPath)
	fmt.Printf("Participants seen:  %d\n", report.ParticipantsSeen)
	fmt.Printf("Locked rows kept:   %d\n", report.LockedSkipped)
	fmt.Printf("Clusters formed:    %d\n", report.PersonsTotal)
	fmt.Printf("Emails clustered:   %d\n", report.EmailsLinked)
	if persisted {
		fmt.Printf("Persons created:    %d\n", report.PersonsCreated)
	}
	fmt.Printf("Advisory suggestions: %d\n", len(report.Suggestions))
	fmt.Println("By link source:")
	for source, n := range report.BySource {
		fmt.Printf("  %-20s %d emails\n", source, n)
	}
	return nil
}

func runPersonShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-show", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	email := fs.String("email", "", "look up a person by one of their email addresses")
	personID := fs.Int64("id", 0, "look up a person by id")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" && *personID == 0 {
		return fmt.Errorf("person-show requires --email or --id")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var p person.Person
	if *email != "" {
		var pe person.PersonEmail
		p, pe, err = person.LookupByEmail(ctx, db, *email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("no person mapped to %s — run person-resolve --persist first", *email)
			}
			return err
		}
		_ = pe
	} else {
		row := db.QueryRowContext(ctx, `SELECT id, canonical_name, primary_email, note FROM memento_person WHERE id = ?`, *personID)
		if err := row.Scan(&p.ID, &p.CanonicalName, &p.PrimaryEmail, &p.Note); err != nil {
			return err
		}
	}

	emails, err := person.EmailsForPerson(ctx, db, p.ID)
	if err != nil {
		return err
	}

	if *jsonOut {
		return writeJSON(struct {
			Person person.Person        `json:"person"`
			Emails []person.PersonEmail `json:"emails"`
		}{p, emails})
	}

	fmt.Printf("Person #%d: %s\n", p.ID, p.CanonicalName)
	fmt.Printf("Primary email: %s\n", p.PrimaryEmail)
	if p.Note != "" {
		fmt.Printf("Note: %s\n", p.Note)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tDISPLAY NAME\tSOURCE\tCONFIDENCE\tLOCKED")
	for _, e := range emails {
		locked := ""
		if e.Locked {
			locked = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%s\n", e.EmailAddress, e.DisplayName, e.LinkSource, e.Confidence, locked)
	}
	return w.Flush()
}

func runPersonLink(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-link", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	email := fs.String("email", "", "email to link")
	personID := fs.Int64("person", 0, "destination person id")
	note := fs.String("note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" || *personID == 0 {
		return fmt.Errorf("person-link requires --email and --person")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	if err := person.LinkEmailToPerson(ctx, db, *email, *personID, *note); err != nil {
		return err
	}
	fmt.Printf("Linked %s -> person #%d (locked).\n", *email, *personID)
	return nil
}

func runPersonSplit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-split", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	email := fs.String("email", "", "email to split into its own person")
	name := fs.String("name", "", "canonical name for the new person")
	note := fs.String("note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return fmt.Errorf("person-split requires --email")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	personID, err := person.SplitEmailToNewPerson(ctx, db, *email, *name, *note)
	if err != nil {
		return err
	}
	fmt.Printf("Created person #%d from %s (locked).\n", personID, *email)
	return nil
}

func runPersonRepairNonDeterministic(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-repair-nondeterministic", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	dryRun := fs.Bool("dry-run", false, "report unsafe legacy resolver links without changing data")
	apply := fs.Bool("apply", false, "split unsafe legacy resolver links")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *apply {
		return fmt.Errorf("person-repair-nondeterministic requires exactly one of --dry-run or --apply")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	report, err := person.RepairNonDeterministicLinks(ctx, db, person.RepairOptions{Apply: *apply})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(report)
	}

	mode := "Dry run"
	if report.Applied {
		mode = "Applied"
	}
	fmt.Printf("%s legacy non-deterministic link repair.\n", mode)
	fmt.Printf("Persons scanned:   %d\n", report.PersonsScanned)
	fmt.Printf("Persons affected:  %d\n", report.PersonsAffected)
	fmt.Printf("Emails split:      %d\n", report.EmailsSplit)
	if len(report.SplitCountsBySource) > 0 {
		fmt.Println("By prior source:")
		var sources []string
		for source := range report.SplitCountsBySource {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			fmt.Printf("  %-20s %d emails\n", source, report.SplitCountsBySource[source])
		}
	}
	if len(report.Splits) > 0 {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "OLD_PERSON\tNEW_PERSON\tNORMALIZED_EMAIL\tEMAILS\tPRIOR_SOURCES")
		for _, split := range report.Splits {
			newID := "-"
			if split.NewPersonID != 0 {
				newID = fmt.Sprintf("%d", split.NewPersonID)
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
				split.OriginalPersonID,
				newID,
				split.NormalizedEmail,
				strings.Join(split.Emails, ", "),
				strings.Join(split.PriorSources, ", "),
			)
		}
		return w.Flush()
	}
	return nil
}

// runPersonMergeSuggest lists persisted likely-duplicate pairs from the review
// queue. Read-only: no mutations.
func runPersonMergeSuggest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-merge-suggest", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	limit := fs.Int("limit", 25, "max candidates to surface")
	sortKey := fs.String("sort", "combined", "sort by combined, name_similarity, token_overlap, or signature")
	status := fs.String("status", "pending", "suggestion status to list")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	suggestions, err := person.ListMergeSuggestions(ctx, db, person.ListMergeSuggestionOptions{
		Status: *status,
		Sort:   *sortKey,
		Limit:  *limit,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		return writeJSON(suggestions)
	}

	if len(suggestions) == 0 {
		fmt.Println("No merge suggestions found.")
		fmt.Println("Tip: run `./memento refresh` first so the persisted queue is current.")
		return nil
	}

	fmt.Printf("Found %d merge suggestion(s):\n\n", len(suggestions))
	for i, suggestion := range suggestions {
		a, err := personSummaryLine(ctx, db, suggestion.PersonAID)
		if err != nil {
			return fmt.Errorf("look up person %d: %w", suggestion.PersonAID, err)
		}
		b, err := personSummaryLine(ctx, db, suggestion.PersonBID)
		if err != nil {
			return fmt.Errorf("look up person %d: %w", suggestion.PersonBID, err)
		}
		fmt.Printf("[%d] id=%d combined=%.3f signature=%.3f name=%.3f token=%.3f sources=%s",
			i+1, suggestion.ID, suggestion.CombinedScore, suggestion.SignatureScore,
			suggestion.NameSimilarity, suggestion.TokenOverlap, strings.Join(suggestion.Sources, ","))
		if suggestion.ScoresStale {
			fmt.Print(" scores=pending")
		}
		fmt.Println()
		fmt.Printf("    person #%d  %s\n", suggestion.PersonAID, a)
		fmt.Printf("    person #%d  %s\n", suggestion.PersonBID, b)
		fmt.Println("    Decide with: ./memento person-merge --from <id> --into <id>")
		fmt.Println()
	}
	fmt.Println("Review carefully. Name and graph evidence is suggestive, not authoritative.")
	return nil
}

// runPersonMerge absorbs --from into --into. All emails, facets, narratives,
// notes, and project memberships transfer; the source person row is deleted.
// Derived tables (candidates, report, social_edge, social_metric) cascade
// and will rebuild on the next refresh.
func runPersonMerge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("person-merge", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	fromID := fs.Int64("from", 0, "person id to absorb (will be deleted)")
	intoID := fs.Int64("into", 0, "person id to keep")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fromID == 0 || *intoID == 0 {
		return fmt.Errorf("person-merge requires --from and --into")
	}
	if *fromID == *intoID {
		return fmt.Errorf("--from and --into must differ")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	// Surface what's about to move so the user can sanity-check before
	// committing. This is the last line of defense against a typo.
	fromPerson, err := personSummaryLine(ctx, db, *fromID)
	if err != nil {
		return fmt.Errorf("look up --from: %w", err)
	}
	intoPerson, err := personSummaryLine(ctx, db, *intoID)
	if err != nil {
		return fmt.Errorf("look up --into: %w", err)
	}
	fmt.Println("About to merge:")
	fmt.Printf("  FROM person #%d  %s  (will be deleted)\n", *fromID, fromPerson)
	fmt.Printf("  INTO person #%d  %s  (will be kept)\n", *intoID, intoPerson)

	if !*yes {
		fmt.Print("\nProceed? Type 'yes' to confirm: ")
		var confirm string
		if _, err := fmt.Scanln(&confirm); err != nil {
			confirm = ""
		}
		if strings.ToLower(strings.TrimSpace(confirm)) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	result, err := person.MergePersons(ctx, db, *fromID, *intoID)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("Merge complete.")
	fmt.Printf("  Emails transferred:           %d\n", result.EmailsTransferred)
	fmt.Printf("  Facets transferred:           %d\n", result.FacetsTransferred)
	fmt.Printf("  Narratives transferred:       %d (skipped %d on conflict)\n",
		result.NarrativesTransferred, result.NarrativesSkippedConflict)
	fmt.Printf("  Notes transferred:            %d\n", result.NotesTransferred)
	fmt.Printf("  Project members transferred:  %d (deduped %d)\n",
		result.ProjectMembersTransferred, result.ProjectMembersSkipped)
	fmt.Println()
	fmt.Println("Run `./memento refresh` to rebuild people report and social graph.")
	return nil
}

// personSummaryLine renders "Name <email>  (N aliases, M locked)" — small
// signal that the right person was picked.
func personSummaryLine(ctx context.Context, db *sql.DB, personID int64) (string, error) {
	var name, email string
	if err := db.QueryRowContext(ctx,
		`SELECT canonical_name, primary_email FROM memento_person WHERE id = ? AND dismissed_at IS NULL`,
		personID,
	).Scan(&name, &email); err != nil {
		return "", err
	}
	var aliasCount, lockedCount int
	db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(locked), 0) FROM memento_person_email WHERE person_id = ?`,
		personID,
	).Scan(&aliasCount, &lockedCount)
	suffix := ""
	if aliasCount > 0 {
		suffix = fmt.Sprintf("  (%d aliases", aliasCount)
		if lockedCount > 0 {
			suffix += fmt.Sprintf(", %d locked", lockedCount)
		}
		suffix += ")"
	}
	return fmt.Sprintf("%s <%s>%s", name, email, suffix), nil
}

func runProject(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("project requires a subcommand: create, add-messages, add-person, show, clear-messages, remove-message, generate")
	}

	db, _, err := openMementoStore("")
	if err != nil {
		return err
	}
	defer db.Close()

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("project create", flag.ContinueOnError)
		name := fs.String("name", "", "project name")
		slug := fs.String("slug", "", "project slug")
		started := fs.String("started", "", "started date (YYYY-MM-DD)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || *slug == "" {
			return fmt.Errorf("create requires --name and --slug")
		}
		var startedPtr *string
		if *started != "" {
			startedPtr = started
		}
		id, err := project.CreateProject(ctx, db, *name, *slug, startedPtr)
		if err != nil {
			return err
		}
		fmt.Printf("Created project %q (slug: %s) with ID %d\n", *name, *slug, id)
		return nil

	case "add-messages":
		fs := flag.NewFlagSet("project add-messages", flag.ContinueOnError)
		slug := fs.String("slug", "", "project slug")
		search := fs.String("search", "", "msgvault search query")
		mode := fs.String("mode", "fts", "msgvault search mode: fts or hybrid")
		label := fs.String("label", "", "msgvault label")
		threadID := fs.Int64("thread", 0, "thread conversation id")
		msgID := fs.Int64("message-id", 0, "explicit message id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("add-messages requires --slug")
		}

		if *search != "" {
			n, err := project.AddMessagesBySearch(ctx, db, *slug, *search, *mode)
			if err != nil {
				return err
			}
			fmt.Printf("Added %d messages via %s search query %q\n", n, *mode, *search)
			return nil
		} else if *label != "" {
			n, err := project.AddMessagesByLabel(ctx, db, *slug, *label)
			if err != nil {
				return err
			}
			fmt.Printf("Added %d messages via label %q\n", n, *label)
			return nil
		} else if *threadID != 0 {
			n, err := project.AddMessagesByThread(ctx, db, *slug, *threadID)
			if err != nil {
				return err
			}
			fmt.Printf("Added %d messages via thread ID %d\n", n, *threadID)
			return nil
		} else if *msgID != 0 {
			err := project.AddMessageExplicit(ctx, db, *slug, *msgID, "manual")
			if err != nil {
				return err
			}
			fmt.Printf("Added message ID %d explicitly\n", *msgID)
			return nil
		}
		return fmt.Errorf("add-messages requires one of: --search, --label, --thread, or --message-id")

	case "clear-messages":
		fs := flag.NewFlagSet("project clear-messages", flag.ContinueOnError)
		slug := fs.String("slug", "", "project slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("clear-messages requires --slug")
		}
		n, err := project.ClearMessages(ctx, db, *slug)
		if err != nil {
			return err
		}
		fmt.Printf("Removed %d attached messages from project %s\n", n, *slug)
		return nil

	case "add-person":
		fs := flag.NewFlagSet("project add-person", flag.ContinueOnError)
		slug := fs.String("slug", "", "project slug")
		email := fs.String("email", "", "person email address")
		role := fs.String("role", "", "role in the project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" || *email == "" {
			return fmt.Errorf("add-person requires --slug and --email")
		}
		err := project.AddPerson(ctx, db, *slug, *email, *role)
		if err != nil {
			return err
		}
		fmt.Printf("Linked email %s to project %s with role %q\n", *email, *slug, *role)
		return nil

	case "show":
		fs := flag.NewFlagSet("project show", flag.ContinueOnError)
		slug := fs.String("slug", "", "project slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("show requires --slug")
		}
		return project.ShowProject(ctx, db, *slug)

	case "remove-message":
		fs := flag.NewFlagSet("project remove-message", flag.ContinueOnError)
		slug := fs.String("slug", "", "project slug")
		msgID := fs.Int64("message-id", 0, "message id to remove")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" || *msgID == 0 {
			return fmt.Errorf("remove-message requires --slug and --message-id")
		}
		err := project.RemoveMessage(ctx, db, *slug, *msgID)
		if err != nil {
			return err
		}
		fmt.Printf("Removed message ID %d from project %s\n", *msgID, *slug)
		return nil

	case "generate":
		return fmt.Errorf("project generate has moved to the UI — compile via the project page (calls the Next.js project-agent route)")

	default:
		return fmt.Errorf("unknown project subcommand %q", args[0])
	}
}

func runProjectPages(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("project-pages", flag.ContinueOnError)
	slug := fs.String("slug", "", "project slug")
	out := fs.String("out", "./data/projects/", "output directory or file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		return fmt.Errorf("project-pages requires --slug")
	}

	db, _, err := openMementoStore("")
	if err != nil {
		return err
	}
	defer db.Close()

	outPath := *out
	if strings.HasSuffix(outPath, "/") || filepath.Ext(outPath) == "" {
		outPath = filepath.Join(outPath, *slug+".json")
	}

	return project.ExportProjectPage(ctx, db, *slug, outPath)
}

func runNewsletterDetect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("newsletter-detect", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	minMessages := fs.Int("min-messages", 20, "minimum messages for a sender to be considered")
	persist := fs.Bool("persist", false, "write detected sources to memento_newsletter_source")
	jsonOut := fs.Bool("json", false, "emit JSON")
	limit := fs.Int("limit", 100, "maximum rows to print in table output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	report, err := newsletter.DetectSources(ctx, db, *minMessages)
	if err != nil {
		return err
	}
	if *persist {
		if err := newsletter.PersistSources(ctx, db, report); err != nil {
			return err
		}
	}
	if *jsonOut {
		return writeJSON(report)
	}

	fmt.Printf("Detected %d newsletter-like sources.\n", len(report.Sources))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SLUG\tSOURCE\tEMAIL\tMESSAGES\tUNSUB\tREASON")
	for i, source := range report.Sources {
		if i >= *limit {
			break
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
			source.Slug,
			source.DisplayName,
			source.SenderEmail,
			source.MessageCount,
			source.UnsubscribeCount,
			source.ClassificationReason,
		)
	}
	return w.Flush()
}

func runNewsletterGenerate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("newsletter-generate", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	slug := fs.String("slug", "", "newsletter source slug")
	all := fs.Bool("all", false, "generate all detected newsletter sources")
	llm := fs.Bool("llm", false, "trigger LLM narrative generation")
	limit := fs.Int("messages", 50, "number of recent messages to include in the prompt")
	maxSources := fs.Int("max-sources", 0, "maximum sources to generate when --all is used")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*llm {
		return fmt.Errorf("newsletter-generate requires --llm")
	}
	if *slug == "" && !*all {
		return fmt.Errorf("newsletter-generate requires --slug or --all")
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	if *all {
		sources, err := newsletter.ListSources(ctx, db)
		if err != nil {
			return err
		}
		count := 0
		for _, source := range sources {
			if *maxSources > 0 && count >= *maxSources {
				break
			}
			if err := newsletter.GenerateNarrative(ctx, db, source.Slug, *limit); err != nil {
				return fmt.Errorf("generate %s: %w", source.Slug, err)
			}
			count++
		}
		fmt.Printf("Generated %d newsletter narratives.\n", count)
		return nil
	}

	return newsletter.GenerateNarrative(ctx, db, *slug, *limit)
}

func runNewsletterPages(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("newsletter-pages", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	out := fs.String("out", "./data/newsletters", "output directory")
	timeline := fs.Int("timeline", 50, "messages to include per source timeline")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, _, err := openMementoStore(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}
	if err := newsletter.ExportPages(ctx, db, *out, *timeline); err != nil {
		return err
	}
	fmt.Printf("Successfully exported newsletter pages to %s\n", *out)
	return nil
}

func runConcept(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("concept requires a subcommand: create, add-messages, remove-message, show, list, generate")
	}

	db, _, err := openMementoStore("")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return err
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("concept create", flag.ContinueOnError)
		name := fs.String("name", "", "concept name (e.g. \"React\")")
		slug := fs.String("slug", "", "url-safe slug (default: derived from name)")
		scope := fs.String("scope", "", "scope description — what counts as on-topic")
		seedStr := fs.String("seed", "", "comma-separated seed search keywords")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return fmt.Errorf("concept create requires --name")
		}
		s := *slug
		if s == "" {
			s = slugifyArg(*name)
		}
		var seeds []string
		if *seedStr != "" {
			for _, k := range strings.Split(*seedStr, ",") {
				k = strings.TrimSpace(k)
				if k != "" {
					seeds = append(seeds, k)
				}
			}
		}
		id, err := concept.CreateConcept(ctx, db, *name, s, *scope, seeds)
		if err != nil {
			return err
		}
		fmt.Printf("Created concept #%d %q (slug: %s)\n", id, *name, s)
		if len(seeds) > 0 {
			fmt.Printf("  Seed keywords: %s\n", strings.Join(seeds, ", "))
			fmt.Printf("  Tip: run `memento concept add-messages --slug %s --seed` to back-fill with these terms.\n", s)
		}
		return nil

	case "add-messages":
		fs := flag.NewFlagSet("concept add-messages", flag.ContinueOnError)
		slug := fs.String("slug", "", "concept slug")
		search := fs.String("search", "", "msgvault search query (single)")
		useSeed := fs.Bool("seed", false, "back-fill using the concept's seed keywords")
		msgID := fs.Int64("message-id", 0, "explicit message id")
		limit := fs.Int("limit", 200, "per-query result limit")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("add-messages requires --slug")
		}
		if *msgID != 0 {
			if err := concept.AddMessageExplicit(ctx, db, *slug, *msgID, "manual", "", 1.0); err != nil {
				return err
			}
			fmt.Printf("Added message #%d to concept %s.\n", *msgID, *slug)
			return nil
		}
		var queries []string
		if *search != "" {
			queries = append(queries, *search)
		}
		if *useSeed {
			c, err := concept.GetConceptBySlug(ctx, db, *slug)
			if err != nil {
				return err
			}
			queries = append(queries, c.SeedKeywords...)
		}
		if len(queries) == 0 {
			return fmt.Errorf("add-messages requires --search, --seed, or --message-id")
		}
		n, err := concept.AddMessagesBySearch(ctx, db, *slug, queries, *limit)
		if err != nil {
			return err
		}
		fmt.Printf("Added %d messages to concept %s across %d query terms.\n", n, *slug, len(queries))
		return nil

	case "remove-message":
		fs := flag.NewFlagSet("concept remove-message", flag.ContinueOnError)
		slug := fs.String("slug", "", "concept slug")
		msgID := fs.Int64("message-id", 0, "message id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" || *msgID == 0 {
			return fmt.Errorf("remove-message requires --slug and --message-id")
		}
		if err := concept.RemoveMessage(ctx, db, *slug, *msgID); err != nil {
			return err
		}
		fmt.Printf("Removed message #%d from concept %s.\n", *msgID, *slug)
		return nil

	case "show":
		fs := flag.NewFlagSet("concept show", flag.ContinueOnError)
		slug := fs.String("slug", "", "concept slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("show requires --slug")
		}
		return concept.ShowConcept(ctx, db, *slug)

	case "list":
		fs := flag.NewFlagSet("concept list", flag.ContinueOnError)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		concepts, err := concept.ListConcepts(ctx, db)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\tNAME\tSTATUS\tSCOPE")
		for _, c := range concepts {
			scope := c.ScopeDescription
			if len(scope) > 60 {
				scope = scope[:57] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Slug, c.Name, c.Status, scope)
		}
		return w.Flush()

	case "generate":
		fs := flag.NewFlagSet("concept generate", flag.ContinueOnError)
		slug := fs.String("slug", "", "concept slug")
		llm := fs.Bool("llm", false, "trigger LLM narrative generation")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *slug == "" {
			return fmt.Errorf("generate requires --slug")
		}
		if !*llm {
			return fmt.Errorf("generate requires --llm flag")
		}
		return fmt.Errorf("concept generation now runs through the browser concept-agent at /concepts/%s", *slug)

	default:
		return fmt.Errorf("unknown concept subcommand %q", args[0])
	}
}

func runConceptPages(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("concept-pages", flag.ContinueOnError)
	slug := fs.String("slug", "", "limit export to one slug (default: all concepts)")
	out := fs.String("out", "./data/concepts/", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, _, err := openMementoStore("")
	if err != nil {
		return err
	}
	defer db.Close()

	outDir := *out
	if strings.HasSuffix(outDir, ".json") {
		// caller passed a file path; treat as directory of the file.
		outDir = filepath.Dir(outDir)
	}

	if *slug != "" {
		outPath := filepath.Join(outDir, *slug+".json")
		if err := concept.ExportConceptPage(ctx, db, *slug, outPath); err != nil {
			return err
		}
	} else {
		concepts, err := concept.ListConcepts(ctx, db)
		if err != nil {
			return err
		}
		for _, c := range concepts {
			outPath := filepath.Join(outDir, c.Slug+".json")
			if err := concept.ExportConceptPage(ctx, db, c.Slug, outPath); err != nil {
				return fmt.Errorf("export %s: %w", c.Slug, err)
			}
		}
	}
	if err := concept.ExportConceptIndex(ctx, db, outDir); err != nil {
		return fmt.Errorf("write concept index: %w", err)
	}
	fmt.Printf("Wrote concept pages + index.json to %s\n", outDir)
	return nil
}

// signalCancel listens for SIGINT/SIGTERM and triggers cancel when received.
// Used by runServe for graceful shutdown.
func signalCancel(ctx context.Context, cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(ch)
	select {
	case <-ctx.Done():
		return
	case <-ch:
		fmt.Fprintln(os.Stderr, "\nReceived shutdown signal.")
		cancel()
	}
}

// runServe starts the HTTP API server. Bound to 127.0.0.1 only — see
// internal/server/server.go for the local-first guardrail.
func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultBackendPort, "TCP port to listen on (127.0.0.1 only)")
	dbPath := fs.String("db", "", "msgvault SQLite database path (overrides config)")
	demo := fs.Bool("demo", false, "prepare/use the populated demo database before serving")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *demo {
		return runDemoMode(ctx, demoModeOptions{
			DBPath: *dbPath,
			Port:   *port,
		})
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	db, err := store.Open(reader.Path())
	if err != nil {
		return fmt.Errorf("open memento store: %w", err)
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		signalCancel(runCtx, cancel)
	}()

	srv := server.New(server.Options{
		Port:   *port,
		DBPath: reader.Path(),
	}, db, reader)
	return srv.Run(runCtx)
}

// runRefresh rebuilds the materialized rollup tables. Flags allow targeting
// individual dimensions; without flags all four are refreshed.
func runRefresh(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	people := fs.Bool("people", false, "refresh only the people rollup")
	newsletters := fs.Bool("newsletters", false, "refresh only the newsletters rollup")
	projects := fs.Bool("projects", false, "refresh only the projects rollup")
	concepts := fs.Bool("concepts", false, "refresh only the concepts rollup")
	avatars := fs.Bool("avatars", false, "refresh cached Gravatar avatars")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reader, err := openReader(*dbPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	db, err := store.Open(reader.Path())
	if err != nil {
		return fmt.Errorf("open memento store: %w", err)
	}
	defer db.Close()
	if _, err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	selection := refreshSelection{
		People:      *people,
		Newsletters: *newsletters,
		Projects:    *projects,
		Concepts:    *concepts,
		Avatars:     *avatars,
	}.resolve()
	if selection.People {
		fmt.Print("Refreshing people report… ")
		t0 := time.Now()
		n, err := refresh.RefreshPeopleReport(ctx, db)
		if err != nil {
			return fmt.Errorf("people: %w", err)
		}
		fmt.Printf("%d rows [%s]\n", n, time.Since(t0).Round(time.Millisecond))
	}
	if selection.Newsletters {
		fmt.Print("Refreshing newsletters report… ")
		t0 := time.Now()
		n, err := refresh.RefreshNewslettersReport(ctx, db)
		if err != nil {
			return fmt.Errorf("newsletters: %w", err)
		}
		fmt.Printf("%d rows [%s]\n", n, time.Since(t0).Round(time.Millisecond))
	}
	if selection.Projects {
		fmt.Print("Refreshing projects report… ")
		t0 := time.Now()
		n, err := refresh.RefreshProjectsReport(ctx, db)
		if err != nil {
			return fmt.Errorf("projects: %w", err)
		}
		fmt.Printf("%d rows [%s]\n", n, time.Since(t0).Round(time.Millisecond))
	}
	if selection.Concepts {
		fmt.Print("Refreshing concepts report… ")
		t0 := time.Now()
		n, err := refresh.RefreshConceptsReport(ctx, db)
		if err != nil {
			return fmt.Errorf("concepts: %w", err)
		}
		fmt.Printf("%d rows [%s]\n", n, time.Since(t0).Round(time.Millisecond))
	}
	// Social graph runs last — depends on resolved person state from people report.
	// Rebuild when refreshing all, or when person identity (--people) changes.
	if selection.Social {
		fmt.Print("Building social graph…     ")
		t0 := time.Now()
		result, err := social.BuildSocialGraph(ctx, db)
		if err != nil {
			return fmt.Errorf("social: %w", err)
		}
		fmt.Printf("%d edges, %d clusters [%s]\n", result.EdgeCount, result.ClusterCount, time.Since(t0).Round(time.Millisecond))

		fmt.Print("Persisting merge suggestions… ")
		t0 = time.Now()
		mergeCandidates, err := person.GenerateAndPersistGraphSuggestions(ctx, db, person.DefaultMergeOptions())
		if err != nil {
			return fmt.Errorf("merge suggestions: %w", err)
		}
		fmt.Printf("%d graph-backed suggestions [%s]\n", len(mergeCandidates), time.Since(t0).Round(time.Millisecond))
	}
	if selection.Avatars {
		fmt.Print("Refreshing avatars…        ")
		t0 := time.Now()
		result, err := avatar.RefreshAll(ctx, db, nil, avatar.RefreshOptions{})
		if err != nil {
			return fmt.Errorf("avatars: %w", err)
		}
		fmt.Printf("%d found, %d notfound, %d errors [%s]\n", result.Found, result.NotFound, result.Errors, time.Since(t0).Round(time.Millisecond))
	}
	return nil
}

type refreshSelection struct {
	People      bool
	Newsletters bool
	Projects    bool
	Concepts    bool
	Social      bool
	Avatars     bool
}

func (s refreshSelection) resolve() refreshSelection {
	dimensionAny := s.People || s.Newsletters || s.Projects || s.Concepts
	runDefaultDimensions := !dimensionAny && !s.Avatars
	return refreshSelection{
		People:      runDefaultDimensions || s.People,
		Newsletters: runDefaultDimensions || s.Newsletters,
		Projects:    runDefaultDimensions || s.Projects,
		Concepts:    runDefaultDimensions || s.Concepts,
		Social:      runDefaultDimensions || s.People,
		Avatars:     s.Avatars,
	}
}

// slugifyArg is a small local slugifier used by the CLI when --slug is
// omitted. Keep it conservative: lowercase, hyphenate non-alphanumeric.
func slugifyArg(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune('-')
		}
	}
	out := sb.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}
