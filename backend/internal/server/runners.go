package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"memento/backend/internal/msgvault"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/person"
	"memento/backend/internal/refresh"
	"memento/backend/internal/social"
)

// jobCtx is a 10-minute background context for long-running jobs. Long enough
// for a model provider call; bounded so a wedged process doesn't pin
// resources forever.
func jobCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Minute)
}

// runPeopleRefresh runs the full people pipeline: resolver -> candidates
// classifier. Mirrors the CLI commands `person-resolve --persist` and
// `people-candidates --persist`.
func (s *Server) runPeopleRefresh(jobID string) {
	ctx, cancel := jobCtx()
	defer cancel()

	s.jobs.Append(jobID, "Resolving canonical persons…", JobRunning)
	report, err := person.ResolveAndPersist(ctx, s.reader, s.db, person.DefaultResolveOptions())
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("resolve and persist persons: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Resolved %d clusters from %d participants.", report.PersonsTotal, report.ParticipantsSeen), JobRunning)
	s.jobs.Append(jobID, fmt.Sprintf("Persisted %d new persons, %d emails linked.", report.PersonsCreated, report.EmailsLinked), JobRunning)

	s.jobs.Append(jobID, "Building candidate report…", JobRunning)
	candReport, err := people.BuildCandidateReport(ctx, s.reader, people.CandidateOptions{Limit: 200})
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("build candidate report: %w", err))
		return
	}
	if err := people.PersistCandidateReport(ctx, s.db, candReport); err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("persist candidates: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Wrote %d candidate rows.", len(candReport.Candidates)), JobRunning)

	s.jobs.Append(jobID, "Building people report rollup…", JobRunning)
	n, err := refresh.RefreshPeopleReport(ctx, s.db)
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("refresh people report: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Wrote %d rows to memento_people_report.", n), JobRunning)

	s.jobs.Append(jobID, "Building social graph…", JobRunning)
	graphResult, err := social.BuildSocialGraph(ctx, s.db)
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("build social graph: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Wrote %d social edges across %d clusters.", graphResult.EdgeCount, graphResult.ClusterCount), JobRunning)

	s.jobs.Finish(jobID, nil)
}

// runNewsletterDetect mirrors the `newsletter-detect --persist` command.
func (s *Server) runNewsletterDetect(jobID string) {
	ctx, cancel := jobCtx()
	defer cancel()

	s.jobs.Append(jobID, "Detecting newsletter sources…", JobRunning)
	report, err := newsletter.DetectSources(ctx, s.db, 20)
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("detect: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Detected %d sources.", len(report.Sources)), JobRunning)

	if err := newsletter.PersistSources(ctx, s.db, report); err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("persist: %w", err))
		return
	}
	s.jobs.Append(jobID, "Persisted sources (sweep + upsert).", JobRunning)

	s.jobs.Append(jobID, "Building newsletters report rollup…", JobRunning)
	n, err := refresh.RefreshNewslettersReport(ctx, s.db)
	if err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("refresh newsletters report: %w", err))
		return
	}
	s.jobs.Append(jobID, fmt.Sprintf("Wrote %d rows to memento_newsletters_report.", n), JobRunning)

	s.jobs.Finish(jobID, nil)
}

func (s *Server) runNewsletterGenerate(jobID, slug string) {
	ctx, cancel := jobCtx()
	defer cancel()

	started := time.Now()
	log.Printf("[newsletter] generation job started slug=%s job_id=%s", slug, jobID)
	s.jobs.Append(jobID, fmt.Sprintf("Generating narrative for newsletter %q (LLM call)…", slug), JobRunning)
	// 200 messages is the default message budget — see newsletter.GenerateNarrative.
	if err := newsletter.GenerateNarrative(ctx, s.db, slug, 200); err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("generate narrative: %w", err))
		return
	}
	s.jobs.Append(jobID, "Saved narrative sections.", JobRunning)

	s.jobs.Append(jobID, "Refreshing newsletter report rollup…", JobRunning)
	if _, err := refresh.RefreshNewslettersReport(ctx, s.db); err != nil {
		s.jobs.Finish(jobID, fmt.Errorf("refresh newsletters report: %w", err))
		return
	}
	log.Printf("[newsletter] generation job succeeded slug=%s job_id=%s duration=%s", slug, jobID, time.Since(started))
	s.jobs.Finish(jobID, nil)
}

// Ensure the msgvault package import isn't pruned (the server holds a reader).
var _ = msgvault.Reader{}
