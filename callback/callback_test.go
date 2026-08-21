package callback

import (
	"errors"
	"testing"
	"time"
)

func TestParseHadesWebhook(t *testing.T) {
	body := []byte(`{
		"job_id": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f",
		"status": "Succeeded",
		"started_at": "2026-08-20T10:00:00.123456789Z",
		"finished_at": "2026-08-20T10:00:31.987654321Z"
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.JobID != "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f" {
		t.Errorf("JobID = %q", n.JobID)
	}
	if n.Source != SourceHades {
		t.Errorf("Source = %q, want %q", n.Source, SourceHades)
	}
	if !n.Terminal {
		t.Error("Succeeded must be terminal")
	}
	if n.Status != StatusSucceeded {
		t.Errorf("Status = %q, want %q", n.Status, StatusSucceeded)
	}
	if n.RawStatus != "Succeeded" {
		t.Errorf("RawStatus = %q, want the verbatim value", n.RawStatus)
	}

	// The reported timestamps are provenance, but they must survive with full
	// nanosecond resolution: truncating them here would hide a clock problem
	// rather than reveal one.
	wantStart := time.Date(2026, 8, 20, 10, 0, 0, 123456789, time.UTC).UnixNano()
	if n.ReportedStartNs == nil || *n.ReportedStartNs != wantStart {
		t.Errorf("ReportedStartNs = %v, want %d", n.ReportedStartNs, wantStart)
	}
	wantEnd := time.Date(2026, 8, 20, 10, 0, 31, 987654321, time.UTC).UnixNano()
	if n.ReportedEndNs == nil || *n.ReportedEndNs != wantEnd {
		t.Errorf("ReportedEndNs = %v, want %d", n.ReportedEndNs, wantEnd)
	}
}

func TestParseHadesTerminalStatuses(t *testing.T) {
	cases := map[string]struct {
		status   string
		want     string
		terminal bool
	}{
		"Succeeded": {"Succeeded", StatusSucceeded, true},
		"Failed":    {"Failed", StatusFailed, true},
		"Stopped":   {"Stopped", StatusStopped, true},
		"Queued":    {"Queued", "", false},
		"Running":   {"Running", "", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n, err := Parse([]byte(`{"job_id":"job-1","status":"` + tc.status + `"}`))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if n.Terminal != tc.terminal {
				t.Errorf("Terminal = %v, want %v", n.Terminal, tc.terminal)
			}
			if n.Status != tc.want {
				t.Errorf("Status = %q, want %q", n.Status, tc.want)
			}
		})
	}
}

// The Jenkins notification plugin nests everything under "build" and returns
// the job id only as a build parameter.
func TestParseJenkinsCompleted(t *testing.T) {
	body := []byte(`{
		"name": "benchmark-job",
		"url": "job/benchmark-job/",
		"build": {
			"number": 42,
			"phase": "COMPLETED",
			"status": "SUCCESS",
			"timestamp": 1787220000000,
			"duration": 31000,
			"parameters": {"HADES_UUID": "6F1A5B1E-6D1A-4F5F-9D0E-1A2B3C4D5E6F"}
		}
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Source != SourceJenkins {
		t.Errorf("Source = %q, want %q", n.Source, SourceJenkins)
	}
	// Jenkins echoes the parameter in whatever case it stored; the id must be
	// canonicalised or it will never join to its submission row.
	if n.JobID != "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f" {
		t.Errorf("JobID = %q, want the canonical lower-case UUID", n.JobID)
	}
	if !n.Terminal || n.Status != StatusSucceeded {
		t.Errorf("Terminal = %v, Status = %q", n.Terminal, n.Status)
	}
	if n.Phase != "COMPLETED" {
		t.Errorf("Phase = %q", n.Phase)
	}

	wantStart := int64(1787220000000) * int64(time.Millisecond)
	if n.ReportedStartNs == nil || *n.ReportedStartNs != wantStart {
		t.Errorf("ReportedStartNs = %v, want %d", n.ReportedStartNs, wantStart)
	}
	wantEnd := wantStart + 31000*int64(time.Millisecond)
	if n.ReportedEndNs == nil || *n.ReportedEndNs != wantEnd {
		t.Errorf("ReportedEndNs = %v, want %d", n.ReportedEndNs, wantEnd)
	}
}

// Jenkins fires the notification at STARTED as well. Treating that as a
// completion would record a build time of roughly zero for every Jenkins job.
func TestParseJenkinsStartedIsNotTerminal(t *testing.T) {
	body := []byte(`{
		"name": "benchmark-job",
		"build": {
			"number": 42,
			"phase": "STARTED",
			"parameters": {"HADES_UUID": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}
		}
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Terminal {
		t.Error("a STARTED notification must not be terminal")
	}
}

func TestParseJenkinsFailureStatuses(t *testing.T) {
	cases := map[string]string{
		"FAILURE":   StatusFailed,
		"UNSTABLE":  StatusFailed,
		"ABORTED":   StatusStopped,
		"NOT_BUILT": StatusStopped,
	}

	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			body := []byte(`{"build":{"phase":"COMPLETED","status":"` + raw + `","parameters":{"HADES_UUID":"job-9"}}}`)
			n, err := Parse(body)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !n.Terminal {
				t.Fatal("must be terminal")
			}
			if n.Status != want {
				t.Errorf("Status = %q, want %q", n.Status, want)
			}
		})
	}
}

func TestParseMalformed(t *testing.T) {
	cases := map[string]struct {
		body string
		want error
	}{
		"not json":          {`not json at all`, ErrMalformed},
		"json array":        {`[1,2,3]`, ErrMalformed},
		"json string":       {`"a string"`, ErrMalformed},
		"empty body":        {``, ErrMalformed},
		"json null":         {`null`, ErrMalformed},
		"no job id":         {`{"status":"Succeeded"}`, ErrNoJobID},
		"blank job id":      {`{"job_id":"   ","status":"Succeeded"}`, ErrNoJobID},
		"no status":         {`{"job_id":"job-1"}`, ErrNoStatus},
		"blank status":      {`{"job_id":"job-1","status":""}`, ErrNoStatus},
		"nested but no id":  {`{"build":{"phase":"COMPLETED","status":"SUCCESS"}}`, ErrNoJobID},
		"truncated payload": {`{"job_id":"job-1","status":`, ErrMalformed},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.body))
			if !errors.Is(err, tc.want) {
				t.Errorf("Parse err = %v, want %v", err, tc.want)
			}
		})
	}
}

// An unrecognised status must never be promoted to a completion; a silently
// mis-classified status would fabricate a measurement.
func TestParseUnknownStatusIsNotTerminal(t *testing.T) {
	n, err := Parse([]byte(`{"job_id":"job-1","status":"WOBBLY"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Terminal {
		t.Error("unknown status must not be terminal")
	}
	if n.RawStatus != "WOBBLY" {
		t.Errorf("RawStatus = %q, the raw value must still be retained", n.RawStatus)
	}
}

func TestScaleEpochUnits(t *testing.T) {
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	want := base.UnixNano()

	cases := map[string]any{
		"seconds":     float64(base.Unix()),
		"millis":      float64(base.UnixMilli()),
		"micros":      float64(base.UnixMicro()),
		"nanos":       float64(base.UnixNano()),
		"rfc3339":     base.Format(time.RFC3339),
		"rfc3339nano": base.Format(time.RFC3339Nano),
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			got := toEpochNanos(value)
			if got == nil {
				t.Fatal("toEpochNanos returned nil")
			}
			// Float64 cannot represent nanosecond epochs exactly; allow the
			// representation error but nothing larger.
			if diff := *got - want; diff > int64(time.Microsecond) || diff < -int64(time.Microsecond) {
				t.Errorf("got %d, want %d (diff %d ns)", *got, want, diff)
			}
		})
	}
}

func TestCanonicalJobIDLeavesNonUUIDsAlone(t *testing.T) {
	if got := canonicalJobID("  build-17  "); got != "build-17" {
		t.Errorf("canonicalJobID = %q, want %q", got, "build-17")
	}
}

func TestExplicitSourceOverridesDetection(t *testing.T) {
	n, err := Parse([]byte(`{"source":"hades","job_id":"job-1","status":"Succeeded"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Source != SourceHades {
		t.Errorf("Source = %q, want %q", n.Source, SourceHades)
	}
}

// The exact payload the Hades status webhook sends (hades PR #507). This test
// is the contract: if Hades changes the wire shape, this fails first.
func TestParseHadesWebhookContract(t *testing.T) {
	body := []byte(`{
		"event": "job.completed",
		"job_id": "7f3a1c2b-1111-2222-3333-444455556666",
		"name": "Example Job",
		"status": "Failed",
		"reason": "ImagePullBackOff: no such image",
		"queued_at": "2026-08-21T12:00:00Z",
		"started_at": "2026-08-21T12:00:05Z",
		"finished_at": "2026-08-21T12:00:41Z",
		"duration_ms": 36000,
		"attempt": 1
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if n.Source != SourceHades {
		t.Errorf("Source = %q, want %q", n.Source, SourceHades)
	}
	if n.JobID != "7f3a1c2b-1111-2222-3333-444455556666" {
		t.Errorf("JobID = %q", n.JobID)
	}
	if !n.Terminal || n.Status != StatusFailed {
		t.Errorf("Terminal = %v, Status = %q, want terminal failed", n.Terminal, n.Status)
	}
	if n.Event != "job.completed" {
		t.Errorf("Event = %q", n.Event)
	}
	if n.Reason != "ImagePullBackOff: no such image" {
		t.Errorf("Reason = %q", n.Reason)
	}
	if n.DeliveryAttempt == nil || *n.DeliveryAttempt != 1 {
		t.Errorf("DeliveryAttempt = %v, want 1", n.DeliveryAttempt)
	}
	if n.ReportedDurationMs == nil || *n.ReportedDurationMs != 36000 {
		t.Errorf("ReportedDurationMs = %v, want 36000", n.ReportedDurationMs)
	}

	wantQueued := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC).UnixNano()
	if n.ReportedQueuedNs == nil || *n.ReportedQueuedNs != wantQueued {
		t.Errorf("ReportedQueuedNs = %v, want %d", n.ReportedQueuedNs, wantQueued)
	}
	wantStart := time.Date(2026, 8, 21, 12, 0, 5, 0, time.UTC).UnixNano()
	if n.ReportedStartNs == nil || *n.ReportedStartNs != wantStart {
		t.Errorf("ReportedStartNs = %v, want %d", n.ReportedStartNs, wantStart)
	}
	wantEnd := time.Date(2026, 8, 21, 12, 0, 41, 0, time.UTC).UnixNano()
	if n.ReportedEndNs == nil || *n.ReportedEndNs != wantEnd {
		t.Errorf("ReportedEndNs = %v, want %d", n.ReportedEndNs, wantEnd)
	}
}

// Only event, job_id, status, finished_at and attempt are guaranteed present;
// everything else is omitempty and must not be required.
func TestParseHadesWebhookMinimalPayload(t *testing.T) {
	body := []byte(`{
		"event": "job.completed",
		"job_id": "7f3a1c2b-1111-2222-3333-444455556666",
		"status": "Succeeded",
		"finished_at": "2026-08-21T12:00:41Z",
		"attempt": 3
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !n.Terminal || n.Status != StatusSucceeded {
		t.Errorf("Terminal = %v, Status = %q", n.Terminal, n.Status)
	}
	if n.ReportedStartNs != nil {
		t.Errorf("ReportedStartNs = %v, want nil for an omitted field", n.ReportedStartNs)
	}
	if n.DeliveryAttempt == nil || *n.DeliveryAttempt != 3 {
		t.Errorf("DeliveryAttempt = %v, want 3", n.DeliveryAttempt)
	}
}

// event is a discriminator with one value today. An unknown event with a known
// terminal status must still be measured, because the outcome lives in status.
func TestParseHadesUnknownEventStillMeasuresStatus(t *testing.T) {
	n, err := Parse([]byte(`{"event":"job.superseded","job_id":"job-1","status":"Succeeded"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !n.Terminal {
		t.Error("an unknown event with a terminal status must still be terminal")
	}
	if n.Event != "job.superseded" {
		t.Errorf("Event = %q", n.Event)
	}
}

// A Jenkins COMPLETED/FINALIZED phase with no build.status says the build ended
// but not how. statusPaths falls back to build.phase when build.status is
// missing, so treating the phase as an outcome recorded every result-less build
// as a SUCCESS - silently inflating the success rate.
func TestJenkinsPhaseWithoutStatusIsNotASuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"status absent", `{
			"name": "benchmark-job",
			"build": {"number": 42, "phase": "COMPLETED",
				"parameters": {"HADES_UUID": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}}
		}`},
		{"status null", `{
			"name": "benchmark-job",
			"build": {"number": 42, "phase": "COMPLETED", "status": null,
				"parameters": {"HADES_UUID": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}}
		}`},
		{"finalized without status", `{
			"name": "benchmark-job",
			"build": {"number": 42, "phase": "FINALIZED",
				"parameters": {"HADES_UUID": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}}
		}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.body))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if n.Terminal {
				t.Errorf("Terminal = true for a phase with no reported result")
			}
			if n.Status == StatusSucceeded {
				t.Errorf("Status = %q; a build with no reported result must never count as a success", n.Status)
			}
			// The raw value still has to survive for inspection.
			if n.RawStatus == "" {
				t.Error("RawStatus is empty; the phase must still be recorded")
			}
		})
	}
}

// A real Jenkins completion carries build.status, which statusPaths prefers over
// build.phase, so the normal path must be unaffected by the above.
func TestJenkinsCompletedWithStatusIsStillTerminal(t *testing.T) {
	body := []byte(`{
		"name": "benchmark-job",
		"build": {"number": 42, "phase": "COMPLETED", "status": "SUCCESS",
			"parameters": {"HADES_UUID": "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}}
	}`)

	n, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !n.Terminal || n.Status != StatusSucceeded {
		t.Errorf("Terminal = %v, Status = %q, want a terminal success", n.Terminal, n.Status)
	}
}
