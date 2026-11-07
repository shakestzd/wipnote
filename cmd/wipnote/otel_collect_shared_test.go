package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/collector"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func claudeTracePayload(sessionAttr string) *tracepb.TracesData {
	now := time.Now().UnixNano()
	return &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: mhResource(mhKV("service.name", "claude-code")),
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code"},
			Spans: []*tracepb.Span{{
				TraceId: bytes.Repeat([]byte{0xab}, 16), SpanId: bytes.Repeat([]byte{0xcd}, 8),
				Name: "claude_code.interaction", StartTimeUnixNano: uint64(now), EndTimeUnixNano: uint64(now + 1e9),
				Attributes: []*commonpb.KeyValue{mhKV("session.id", sessionAttr)},
			}},
		}},
	}}}
}

// One shared collector serves many sessions; each session's spans land in
// its own events.ndjson, chosen by the /s/<sid> path prefix.
func TestSharedCollector_RoutesEachSessionToItsOwnLog(t *testing.T) {
	project := mkOtelCollectProject(t)
	sinks := newSessionSinks(project, 1)
	srv := httptest.NewServer(buildSharedMux(sinks, newAttachSet(time.Now(), func(int) bool { return true })))
	defer srv.Close()

	for _, sid := range []string{"sessA", "sessB"} {
		resp := postProto(t, srv.URL+collector.SessionEndpointPath(sid)+"/v1/traces", claudeTracePayload(sid))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", sid, resp.StatusCode)
		}
	}
	if resp := postProto(t, srv.URL+"/v1/traces", claudeTracePayload("x")); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("un-prefixed path: status %d, want 404", resp.StatusCode)
	}
	sinks.closeAll()

	for _, sid := range []string{"sessA", "sessB"} {
		data, err := os.ReadFile(filepath.Join(project, ".wipnote", "sessions", sid, "events.ndjson"))
		if err != nil {
			t.Fatalf("%s: %v", sid, err)
		}
		if !strings.Contains(string(data), "collector_start") || !strings.Contains(string(data), "claude_code.interaction") {
			t.Fatalf("%s log missing start or span:\n%s", sid, data)
		}
		other := map[string]string{"sessA": "sessB", "sessB": "sessA"}[sid]
		if strings.Contains(string(data), `"wipnote_sid":"`+other+`"`) {
			t.Fatalf("%s log holds %s's events", sid, other)
		}
	}
}

func TestSharedCollector_HealthAndAttach(t *testing.T) {
	attached := newAttachSet(time.Now(), func(int) bool { return true })
	srv := httptest.NewServer(buildSharedMux(newSessionSinks(t.TempDir(), 1), attached))
	defer srv.Close()

	resp, err := http.Post(srv.URL+collector.AttachPath+"?pid=4242", "text/plain", nil)
	if err != nil || resp.StatusCode/100 != 2 {
		t.Fatalf("attach: %v %v", resp, err)
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	h, err := collector.ProbeHealth(port, time.Second)
	if err != nil || h.PID != os.Getpid() || h.Mode != "shared" || h.Attached != 1 {
		t.Fatalf("health = %+v, %v", h, err)
	}
}

// A detaching launcher's session log is flushed and closed at once, so the
// SessionEnd finalize reads every event without waiting for the sync tick.
func TestSharedCollector_DetachClosesSessionLog(t *testing.T) {
	sinks := newSessionSinks(mkOtelCollectProject(t), 1)
	srv := httptest.NewServer(buildSharedMux(sinks, newAttachSet(time.Now(), func(int) bool { return true })))
	defer srv.Close()
	postProto(t, srv.URL+"/s/sx/v1/traces", claudeTracePayload("sx"))
	if sinks.open() != 1 {
		t.Fatalf("open sinks = %d", sinks.open())
	}
	resp, err := http.Post(srv.URL+collector.DetachPath+"?session=sx&pid=4242", "text/plain", nil)
	if err != nil || resp.StatusCode/100 != 2 {
		t.Fatalf("detach: %v %v", resp, err)
	}
	if sinks.open() != 0 {
		t.Fatal("detach left the session log open")
	}
}

// The collector outlives one launcher but not all of them: once every
// attached launcher is dead, the empty clock starts.
func TestAttachSet_EmptyOnlyWhenEveryLauncherIsGone(t *testing.T) {
	start := time.Now()
	live := map[int]bool{10: true, 11: true}
	a := newAttachSet(start, func(pid int) bool { return live[pid] })
	a.add(10)
	a.add(11)
	if d := a.emptyFor(start.Add(time.Minute)); d != 0 {
		t.Fatalf("emptyFor with live launchers = %v", d)
	}
	live[10] = false
	if d := a.emptyFor(start.Add(2 * time.Minute)); d != 0 {
		t.Fatalf("emptyFor with one live launcher = %v", d)
	}
	live[11] = false
	gone := start.Add(3 * time.Minute)
	if d := a.emptyFor(gone); d != 0 {
		t.Fatalf("emptyFor at the moment the last launcher died = %v, want 0", d)
	}
	if d := a.emptyFor(gone.Add(40 * time.Second)); d != 40*time.Second {
		t.Fatalf("emptyFor 40s later = %v", d)
	}
}

// Issue #183 end to end: two launches in one project run ONE otel-collect
// process, on one port, through the real binary.
func TestSharedCollector_TwoLaunchesOneProcess(t *testing.T) {
	bin := buildOtelCollectTestBinary(t)
	project := mkOtelCollectProject(t)
	t.Setenv("WIPNOTE_OTEL_SHARED", "")

	first, exit1 := spawnSessionCollectorTo(project, bin, io.Discard)
	second, exit2 := spawnSessionCollectorTo(project, bin, io.Discard)
	if exit1 || exit2 || first.CollectorPort == 0 {
		t.Fatalf("spawn failed: %+v %+v", first, second)
	}
	rec, err := collector.ReadSharedRecord(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(rec.PID, syscall.SIGTERM) }) // the test's own child
	if second.CollectorPort != first.CollectorPort || rec.Port != first.CollectorPort {
		t.Fatalf("ports differ: %d %d lease %d", first.CollectorPort, second.CollectorPort, rec.Port)
	}
	if first.EndpointPath == second.EndpointPath || !strings.HasPrefix(first.EndpointPath, "/s/") {
		t.Fatalf("endpoint paths %q %q", first.EndpointPath, second.EndpointPath)
	}
	h, err := collector.ProbeHealth(rec.Port, time.Second)
	if err != nil || h.PID != rec.PID || h.Attached != 1 { // both launches are this one test process
		t.Fatalf("health = %+v, %v", h, err)
	}
	first.Cleanup()
	second.Cleanup()
	if !collector.PIDAlive(rec.PID) {
		t.Fatal("detaching must not kill the shared collector")
	}
}
