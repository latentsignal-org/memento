package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// JobStatus is the lifecycle state of a refresh/generate job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// JobEvent is a single progress update streamed to the SSE consumer.
type JobEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Status    JobStatus `json:"status"`
	Step      string    `json:"step,omitempty"`
	Done      int       `json:"done,omitempty"`
	Total     int       `json:"total,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Result    any       `json:"result,omitempty"`
}

// Job tracks one refresh/generate run. In-memory only — does not survive
// server restart (acceptable per current design).
type Job struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"` // e.g. "people-refresh", "concept-generate"
	Status    JobStatus  `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Events    []JobEvent `json:"events"`
	Result    any        `json:"result,omitempty"`

	// listeners receive new events for SSE streaming. Mutex-protected by the
	// parent JobStore.
	listeners []chan JobEvent
}

type JobSnapshot struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Status    JobStatus  `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Events    []JobEvent `json:"events"`
	Result    any        `json:"result,omitempty"`
}

// JobStore is the in-memory registry of jobs. Bounded retention is the
// caller's problem; for hackathon scale this is fine.
type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewJobStore() *JobStore {
	return &JobStore{jobs: map[string]*Job{}}
}

// Create allocates a new job, records its start, and returns it ready to
// receive Append calls.
func (s *JobStore) Create(kind string) *Job {
	id := newJobID()
	job := &Job{
		ID:        id,
		Kind:      kind,
		Status:    JobPending,
		StartedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	return job
}

// Get returns the job by id, or nil if not found.
func (s *JobStore) Get(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *JobStore) Snapshot(id string) (JobSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return JobSnapshot{}, false
	}
	events := append([]JobEvent(nil), job.Events...)
	return JobSnapshot{
		ID:        job.ID,
		Kind:      job.Kind,
		Status:    job.Status,
		StartedAt: job.StartedAt,
		EndedAt:   job.EndedAt,
		Error:     job.Error,
		Events:    events,
		Result:    job.Result,
	}, true
}

func (s *JobStore) AppendProgress(jobID, step string, done, total int, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	ev := JobEvent{
		Timestamp: time.Now().UTC(), Message: detail, Status: JobRunning,
		Step: step, Done: done, Total: total, Detail: detail,
	}
	job.Events = append(job.Events, ev)
	job.Status = JobRunning
	for _, listener := range job.listeners {
		select {
		case listener <- ev:
		default:
		}
	}
}

func (s *JobStore) SetResult(jobID string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		job.Result = result
	}
}

// Subscribe registers a listener for new events on this job. Returns the
// channel and an unsubscribe function. The channel is buffered to avoid
// blocking the producer if the consumer falls behind briefly.
func (s *JobStore) Subscribe(jobID string) (<-chan JobEvent, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, func() {}, false
	}
	ch := make(chan JobEvent, 32)
	job.listeners = append(job.listeners, ch)
	// Replay existing events so the consumer doesn't miss anything that
	// happened before subscription.
	for _, ev := range job.Events {
		select {
		case ch <- ev:
		default:
		}
	}
	unsub := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		j, ok := s.jobs[jobID]
		if !ok {
			return
		}
		out := j.listeners[:0]
		for _, l := range j.listeners {
			if l != ch {
				out = append(out, l)
			}
		}
		j.listeners = out
		close(ch)
	}
	return ch, unsub, true
}

// Append records a new event on the job, broadcasts to listeners, and
// updates the job status if status is non-empty.
func (s *JobStore) Append(jobID string, message string, status JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	ev := JobEvent{
		Timestamp: time.Now().UTC(),
		Message:   message,
		Status:    status,
	}
	job.Events = append(job.Events, ev)
	if status != "" {
		job.Status = status
	}
	for _, l := range job.listeners {
		select {
		case l <- ev:
		default:
			// Drop event if listener is wedged; SSE consumer will see the
			// terminal status via the final replay on reconnect.
		}
	}
}

// Finish marks the job terminal with success or failure.
func (s *JobStore) Finish(jobID string, err error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.EndedAt = &now
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
	} else {
		job.Status = JobSucceeded
	}
	final := JobEvent{Timestamp: now, Status: job.Status, Result: job.Result}
	if err != nil {
		final.Message = "Failed: " + err.Error()
	} else {
		final.Message = "Done."
	}
	job.Events = append(job.Events, final)
	for _, l := range job.listeners {
		select {
		case l <- final:
		default:
		}
	}
}

func newJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
