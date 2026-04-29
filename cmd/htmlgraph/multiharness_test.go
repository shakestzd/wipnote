package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/shakestzd/htmlgraph/internal/otel/sink/ndjson"
)

// mhKV builds a string KeyValue proto for OTLP payloads.
func mhKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   k,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}},
	}
}

// mhResource builds a Resource proto with the given attributes.
func mhResource(attrs ...*commonpb.KeyValue) *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: attrs}
}

// postProto serialises msg and POSTs it to url with application/x-protobuf.
func postProto(t *testing.T, url string, msg proto.Message) *http.Response {
	t.Helper()
	body, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	resp.Body.Close()
	return resp
}

// TestMultiHarnessIngestion verifies that the per-session collector mux
// correctly routes payloads from all three harnesses (Codex, Gemini, Claude)
// through the adapter registry and into the ndjson sink.
func TestMultiHarnessIngestion(t *testing.T) {
	// --- Set up the project dir and ndjson sink ---
	projectDir := t.TempDir()
	sessionID := "mh-test-session"
	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll session dir: %v", err)
	}

	snk, err := ndjson.New(projectDir, sessionID)
	if err != nil {
		t.Fatalf("ndjson.New: %v", err)
	}

	lastActivity := &atomic.Int64{}
	lastActivity.Store(time.Now().UnixMilli())

	mux := buildCollectorMux(snk, lastActivity)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := time.Now().UnixNano()

	// --- POST 1: Codex logs payload ---
	codexLogs := &logspb.LogsData{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: mhResource(
				mhKV("service.name", "codex-cli"),
				mhKV("service.version", "0.1.0"),
			),
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope: &commonpb.InstrumentationScope{Name: "codex"},
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(now),
					Attributes: []*commonpb.KeyValue{
						mhKV("event.name", "codex.user_prompt"),
						mhKV("conversation.id", "codex-test-123"),
					},
				}},
			}},
		}},
	}
	resp := postProto(t, srv.URL+"/v1/logs", codexLogs)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Codex /v1/logs: status=%d, want 200", resp.StatusCode)
	}

	// --- POST 2: Gemini metrics payload ---
	geminiMetrics := &metricspb.MetricsData{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: mhResource(
				mhKV("service.name", "gemini-cli"),
				mhKV("service.version", "0.1.0"),
			),
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Scope: &commonpb.InstrumentationScope{Name: "gemini_cli"},
				Metrics: []*metricspb.Metric{{
					Name: "gemini_cli.session.count",
					Data: &metricspb.Metric_Sum{
						Sum: &metricspb.Sum{
							DataPoints: []*metricspb.NumberDataPoint{{
								TimeUnixNano: uint64(now),
								Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 1},
								Attributes: []*commonpb.KeyValue{
									mhKV("session.id", "gemini-test-456"),
								},
							}},
						},
					},
				}},
			}},
		}},
	}
	resp = postProto(t, srv.URL+"/v1/metrics", geminiMetrics)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Gemini /v1/metrics: status=%d, want 200", resp.StatusCode)
	}

	// --- POST 3: Claude traces payload ---
	claudeTraces := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: mhResource(
				mhKV("service.name", "claude-code"),
				mhKV("service.version", "2.1.42"),
			),
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code"},
				Spans: []*tracepb.Span{{
					TraceId:           bytes.Repeat([]byte{0xab}, 16),
					SpanId:            bytes.Repeat([]byte{0xcd}, 8),
					Name:              "claude_code.interaction",
					StartTimeUnixNano: uint64(now),
					EndTimeUnixNano:   uint64(now + 1_000_000_000),
					Attributes: []*commonpb.KeyValue{
						mhKV("session.id", "claude-test-789"),
					},
				}},
			}},
		}},
	}
	resp = postProto(t, srv.URL+"/v1/traces", claudeTraces)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Claude /v1/traces: status=%d, want 200", resp.StatusCode)
	}

	// --- Read the ndjson output and assert ---
	eventsPath := filepath.Join(sessDir, "events.ndjson")
	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatalf("open events.ndjson: %v", err)
	}
	defer f.Close()

	type signalRecord struct {
		Harness   string `json:"harness"`
		SessionID string `json:"session_id"`
		Kind      string `json:"kind"`
	}

	var records []signalRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec signalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Errorf("unmarshal ndjson line: %v — raw: %q", err, line)
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning events.ndjson: %v", err)
	}

	if len(records) == 0 {
		t.Fatal("events.ndjson is empty — no signals were persisted")
	}

	type want struct {
		harness   string
		sessionID string
	}
	assertions := []want{
		{"codex", "codex-test-123"},
		{"gemini_cli", "gemini-test-456"},
		{"claude_code", "claude-test-789"},
	}

	for _, a := range assertions {
		found := false
		for _, rec := range records {
			if rec.Harness == a.harness && rec.SessionID == a.sessionID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no signal with harness=%q session_id=%q in %d records",
				a.harness, a.sessionID, len(records))
		}
	}
}
