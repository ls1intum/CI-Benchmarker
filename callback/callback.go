// Package callback normalises the status notifications that systems under test
// push back to the benchmarker.
//
// Completion is observed, never inferred. The benchmarker does not poll and
// does not rely on the workload calling home from inside a step container: the
// scheduler itself reports the terminal status over HTTP, and the benchmarker
// stamps arrival on its own clock.
//
// Two wire shapes are supported today and the parser is deliberately
// alias-driven so a third costs one line:
//
//   - Hades status webhook (hades PR #507): a flat object using the vocabulary
//     of shared/buildstatus, delivered at-least-once from a JetStream durable
//     consumer with backoff retry on any non-2xx. Redelivery is expected, not
//     exceptional, which is why the receiver is idempotent by job id.
//   - Jenkins post-build notification, whose payload nests everything under
//     "build" and carries the job id back in build.parameters.
package callback

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sources a callback can come from.
const (
	SourceHades   = "hades"
	SourceJenkins = "jenkins"
	SourceUnknown = "unknown"
)

// Normalised terminal statuses.
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusStopped   = "stopped"
)

// Errors returned for payloads that cannot be turned into an observation.
var (
	// ErrMalformed means the body was not a JSON object.
	ErrMalformed = errors.New("callback payload is not a JSON object")
	// ErrNoJobID means no field carried a job identifier. Without one the
	// observation cannot be attributed, so it is rejected rather than stored
	// against a guessed id.
	ErrNoJobID = errors.New("callback payload carries no job id")
	// ErrNoStatus means no field carried a status.
	ErrNoStatus = errors.New("callback payload carries no status")
)

// Notification is a callback reduced to the fields the measurement path needs.
type Notification struct {
	JobID     string
	Source    string
	Phase     string
	RawStatus string
	// Status is the normalised terminal status. Empty when Terminal is false.
	Status   string
	Terminal bool
	Reason   string
	// Event is the Hades event discriminator ("job.completed"). It is recorded
	// but never switched on: more event types may be added, and the outcome
	// lives in Status, not here.
	Event string
	// ReportedQueuedNs, ReportedStartNs and ReportedEndNs come from the system
	// under test. Hades derives them from NATS server timestamps of the
	// underlying status events rather than from a clock read at send time, so
	// they survive dispatcher lag and redelivery intact and are usable as a
	// cross-check. They are still never the measurement: the two hosts are not
	// clock-synchronised.
	ReportedQueuedNs *int64
	ReportedStartNs  *int64
	ReportedEndNs    *int64
	// ReportedDurationMs is the SUT's own view of how long the job took.
	ReportedDurationMs *int64
	// DeliveryAttempt is the sender's redelivery counter (Hades sends
	// NumDelivered as "attempt"; the first delivery is 1).
	DeliveryAttempt *int64
}

// jobIDPaths lists, in priority order, where a job id may live.
var jobIDPaths = []string{
	"job_id", "jobId", "jobID",
	"build.parameters.HADES_UUID",
	"build.parameters.HADES_JOB_ID",
	"parameters.HADES_UUID",
	"uuid", "UUID",
	"id",
}

// statusPaths lists, in priority order, where a status may live. build.status
// is preferred over build.phase because the phase only says the build ended,
// not how.
var statusPaths = []string{
	"status", "job_status", "jobStatus", "state", "result",
	"build.status",
	"build.phase",
}

var phasePaths = []string{"phase", "build.phase"}

var eventPaths = []string{"event"}

var queuedTimePaths = []string{"queued_at", "queuedAt", "queued_time"}

var durationMsPaths = []string{"duration_ms", "durationMs"}

var attemptPaths = []string{"attempt", "delivery_attempt"}

var reasonPaths = []string{"reason", "message", "error", "build.reason"}

var startTimePaths = []string{
	"started_at", "start_time", "startTime", "startedAt",
}

var endTimePaths = []string{
	"finished_at", "completed_at", "end_time", "endTime", "finishedAt", "completedAt", "timestamp",
}

// terminalStatuses maps every accepted terminal spelling to its normalised
// form. Keys are lower-cased before lookup.
var terminalStatuses = map[string]string{
	// Hades (shared/buildstatus)
	"succeeded": StatusSucceeded,
	"failed":    StatusFailed,
	"stopped":   StatusStopped,
	// Jenkins build results
	"success":   StatusSucceeded,
	"failure":   StatusFailed,
	"unstable":  StatusFailed,
	"aborted":   StatusStopped,
	"not_built": StatusStopped,
	// Common synonyms so a third system under test needs no code change
	"ok":        StatusSucceeded,
	"passed":    StatusSucceeded,
	"completed": StatusSucceeded,
	"error":     StatusFailed,
	"cancelled": StatusStopped,
	"canceled":  StatusStopped,
	"timeout":   StatusStopped,
}

// nonTerminalStatuses are lifecycle notifications that must be logged but must
// not be treated as a completion. A Jenkins STARTED notification lands here
// because it carries no build.status, so build.phase is the only status field
// the parser can see.
var nonTerminalStatuses = map[string]bool{
	"queued":    true,
	"running":   true,
	"started":   true,
	"pending":   true,
	"scheduled": true,
}

// Parse turns a raw callback body into a Notification.
func Parse(body []byte) (Notification, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return Notification{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if doc == nil {
		return Notification{}, ErrMalformed
	}

	n := Notification{Source: detectSource(doc)}

	rawID, ok := firstString(doc, jobIDPaths)
	if !ok || strings.TrimSpace(rawID) == "" {
		return Notification{}, ErrNoJobID
	}
	n.JobID = canonicalJobID(rawID)

	n.Phase, _ = firstString(doc, phasePaths)
	n.Event, _ = firstString(doc, eventPaths)
	n.Reason, _ = firstString(doc, reasonPaths)
	if n.Phase == "" {
		// Hades has no phase field; the event discriminator is the closest
		// equivalent and keeps the audit log readable.
		n.Phase = n.Event
	}
	if attempt, ok := firstNumber(doc, attemptPaths); ok {
		value := int64(attempt)
		n.DeliveryAttempt = &value
	}

	rawStatus, ok := firstString(doc, statusPaths)
	if !ok || strings.TrimSpace(rawStatus) == "" {
		return Notification{}, ErrNoStatus
	}
	n.RawStatus = rawStatus

	n.Status, n.Terminal = classify(rawStatus, n.Phase)
	n.ReportedStartNs, n.ReportedEndNs = reportedTimes(doc, n.Source)

	if v, ok := firstValue(doc, queuedTimePaths); ok {
		n.ReportedQueuedNs = toEpochNanos(v)
	}
	if duration, ok := firstNumber(doc, durationMsPaths); ok {
		value := int64(duration)
		n.ReportedDurationMs = &value
	}

	return n, nil
}

// classify decides whether a status ends the job and what it normalises to.
//
// Jenkins is the awkward case: its post-build notification fires twice, once
// with phase COMPLETED and again with phase FINALIZED, both carrying the same
// build.status. Both are terminal, which is precisely why the receiver has to
// be idempotent - otherwise every Jenkins job would be counted twice, and the
// second, later arrival would overwrite the first with a worse measurement.
func classify(rawStatus, phase string) (status string, terminal bool) {
	key := normaliseKey(rawStatus)

	if nonTerminalStatuses[key] {
		return "", false
	}
	if mapped, found := terminalStatuses[key]; found {
		// A Jenkins STARTED notification carries build.status null but may
		// carry a phase; guard against treating an in-flight build as done.
		// Hades sets phase from its event discriminator ("job.completed"),
		// which never matches these.
		if p := normaliseKey(phase); p == "started" || p == "queued" || p == "running" {
			return "", false
		}
		return mapped, true
	}

	// Unrecognised status. Treat as non-terminal so an unknown spelling can
	// never silently become a completion measurement; the raw value still
	// lands in job_event for inspection.
	return "", false
}

func normaliseKey(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, " ", "_")))
}

// canonicalJobID lower-cases and canonicalises UUIDs so that a system under
// test echoing an id in a different case still joins to its submission row.
func canonicalJobID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if parsed, err := uuid.Parse(trimmed); err == nil {
		return parsed.String()
	}
	return trimmed
}

func detectSource(doc map[string]any) string {
	if explicit, ok := firstString(doc, []string{"source", "system"}); ok {
		switch normaliseKey(explicit) {
		case SourceHades:
			return SourceHades
		case SourceJenkins:
			return SourceJenkins
		}
	}
	// The Jenkins notification plugin nests everything under "build"; nothing
	// Hades emits does.
	if _, ok := doc["build"].(map[string]any); ok {
		return SourceJenkins
	}
	// The Hades webhook always carries job_id and event.
	if _, ok := doc["job_id"]; ok {
		return SourceHades
	}
	return SourceUnknown
}

// reportedTimes extracts the system under test's own view of when the job ran.
func reportedTimes(doc map[string]any, source string) (start, end *int64) {
	if source == SourceJenkins {
		// Jenkins reports build.timestamp as the start in epoch milliseconds
		// and build.duration as the elapsed time, also in milliseconds.
		if ts, ok := firstNumber(doc, []string{"build.timestamp"}); ok {
			startNs := int64(ts) * int64(time.Millisecond)
			start = &startNs
			if dur, hasDur := firstNumber(doc, []string{"build.duration"}); hasDur && dur > 0 {
				endNs := startNs + int64(dur)*int64(time.Millisecond)
				end = &endNs
			}
		}
		return start, end
	}

	if v, ok := firstValue(doc, startTimePaths); ok {
		start = toEpochNanos(v)
	}
	if v, ok := firstValue(doc, endTimePaths); ok {
		end = toEpochNanos(v)
	}
	return start, end
}

// toEpochNanos accepts an RFC3339 string or a bare number and returns epoch
// nanoseconds. Bare numbers are disambiguated by magnitude, which is
// unambiguous for any plausible benchmark date.
func toEpochNanos(v any) *int64 {
	switch typed := v.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, trimmed); err == nil {
				ns := parsed.UnixNano()
				return &ns
			}
		}
		if num, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return scaleEpoch(num)
		}
		return nil
	case float64:
		return scaleEpoch(typed)
	case json.Number:
		if num, err := typed.Float64(); err == nil {
			return scaleEpoch(num)
		}
	}
	return nil
}

// scaleEpoch guesses the unit of a timestamp from its magnitude, because the
// two accepted payload shapes do not declare one and do not agree with each
// other. The boundaries separate any real timestamp cleanly: a 2026 instant is
// ~1.8e9 as seconds, ~1.8e12 as milliseconds, ~1.8e15 as microseconds and
// ~1.8e18 as nanoseconds, so there are three orders of magnitude of slack on
// every boundary.
//
// It is ambiguous only near the epoch - 1000 is read as 1970-01-01T00:00:01,
// not as one millisecond - which affects synthetic values rather than anything
// a system under test would emit. This is acceptable because every value that
// passes through here lands in a reported_* column: provenance, never a
// measurement input. No latency is computed from these, so a misread unit
// cannot move a published number.
func scaleEpoch(v float64) *int64 {
	if v <= 0 {
		return nil
	}

	var ns int64
	switch {
	case v < 1e11: // seconds until the year 5138
		ns = int64(v * float64(time.Second))
	case v < 1e14: // milliseconds
		ns = int64(v * float64(time.Millisecond))
	case v < 1e17: // microseconds
		ns = int64(v * float64(time.Microsecond))
	default: // nanoseconds
		ns = int64(v)
	}
	return &ns
}

// firstValue resolves the first dotted path that exists in doc.
func firstValue(doc map[string]any, paths []string) (any, bool) {
	for _, path := range paths {
		if v, ok := lookup(doc, path); ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func firstString(doc map[string]any, paths []string) (string, bool) {
	v, ok := firstValue(doc, paths)
	if !ok {
		return "", false
	}
	switch typed := v.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return "", false
		}
		return typed, true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	}
	return "", false
}

func firstNumber(doc map[string]any, paths []string) (float64, bool) {
	v, ok := firstValue(doc, paths)
	if !ok {
		return 0, false
	}
	switch typed := v.(type) {
	case float64:
		return typed, true
	case string:
		if num, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return num, true
		}
	}
	return 0, false
}

func lookup(doc map[string]any, path string) (any, bool) {
	current := any(doc)
	for _, segment := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
