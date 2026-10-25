package runner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

func TestArtifactRetryPreservesCandidateAndCompletionAccounting(t *testing.T) {
	for _, lostUploadACK := range []bool{false, true} {
		t.Run(map[bool]string{false: "503", true: "committed ACK lost"}[lostUploadACK], func(t *testing.T) {
			f := newIntentFixture(t)
			executions := 0
			f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
				executions++
				err := os.WriteFile(filepath.Join(req.WorkDir, "test/greeting.test.mjs"), []byte("// fixed implementation\n"), 0600)
				return agent.Result{TreeStopped: true, OutputText: `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Complete"}`}, err
			}
			uploads, completionBodies, completionKeys := interceptArtifactHandoff(t, f, lostUploadACK)
			f.perform(context.Background())
			before := f.snapshot(t)
			if executions != 1 || *uploads != 2 || len(before.Candidates) != 1 || before.Tasks[0].Repairs != 0 ||
				before.Tasks[0].Retries != 0 || before.Runs[0].State != "succeeded" || before.Project.UsedSeconds < 3 {
				t.Fatal("delivery reran work, charged repairs or lost result", executions, *uploads, before.Tasks, before.Runs)
			}
			var pending spool
			if err := readJSON(f.r.spoolPath(f.record.Assignment.Run.ID), &pending); err != nil ||
				pending.Request == nil || pending.Request.Status != "succeeded" || pending.Request.ArtifactID == "" {
				t.Fatal("successful fixed completion not durable before delivery", err)
			}
			restarted, err := New(f.r.config)
			if err != nil {
				t.Fatal(err)
			}
			restarted.http.Transport = f.r.http.Transport
			restarted.OnEvent = func(Event) {}
			if err := restarted.recoverRecords(context.Background()); err != nil {
				t.Fatal(err)
			}
			after := f.snapshot(t)
			assertArtifactHandoffReplay(t, before, after, *completionBodies, *completionKeys)
			if executions != 1 || *uploads != 2 {
				t.Fatal("completion replay uploaded or executed again")
			}
		})
	}
}

func interceptArtifactHandoff(t *testing.T, f *intentFixture, lostUploadACK bool) (*int, *[]string, *[]string) {
	t.Helper()
	original := f.r.http.Transport
	uploads := 0
	var bodies, keys []string
	f.r.http.Transport = intentTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/artifacts") {
			uploads++
			if uploads == 1 && !lostUploadACK {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
			}
			response, err := original.RoundTrip(req)
			if uploads == 1 && err == nil {
				response.Body.Close()
				return nil, io.ErrUnexpectedEOF
			}
			return response, err
		}
		if !strings.HasSuffix(req.URL.Path, "/complete") {
			return original.RoundTrip(req)
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, string(raw))
		keys = append(keys, req.Header.Get("Idempotency-Key"))
		req.Body = io.NopCloser(bytes.NewReader(raw))
		response, err := original.RoundTrip(req)
		if len(bodies) == 1 && err == nil {
			response.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	})
	return &uploads, &bodies, &keys
}

func assertArtifactHandoffReplay(t *testing.T, before, after domain.Snapshot, bodies, keys []string) {
	t.Helper()
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Runs, after.Runs) ||
		!reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(before.Project, after.Project) ||
		!reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) {
		t.Fatal("completion replay changed candidates or cumulative accounting")
	}
	if len(bodies) != 2 || len(keys) != 2 || bodies[0] != bodies[1] || keys[0] != keys[1] {
		t.Fatal("completion replay changed its exact request identity")
	}
}
