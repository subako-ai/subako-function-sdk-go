// Runs the example module as Subako's function worker does: one session per
// request list, each request sent only once the previous one is answered,
// stdin closed after the last answer, and subako_http answered by the test.
package e2e

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const revision = "2026-07-28"

type worker struct {
	runtime wazero.Runtime
	module  wazero.CompiledModule
	volume  string
	// What subako_http answers, and the requests it was sent.
	answer string
	sent   []map[string]any
}

var (
	shared     *worker
	sharedErr  error
	sharedOnce sync.Once
)

// newWorker is the one worker every test shares, built on first use, with
// its volume emptied and subako_http reset.
func newWorker(t *testing.T) *worker {
	t.Helper()
	sharedOnce.Do(func() { shared, sharedErr = start() })
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	shared.volume = t.TempDir()
	shared.answer = ""
	shared.sent = nil
	return shared
}

func start() (*worker, error) {
	dir, err := os.MkdirTemp("", "subakofn-e2e")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	wasm := filepath.Join(dir, "module.wasm")
	build := exec.Command("go", "build", "-o", wasm, "./examples/weather")
	build.Dir = ".."
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build the example: %v\n%s", err, out)
	}
	bytes, err := os.ReadFile(wasm)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	w := &worker{runtime: wazero.NewRuntime(ctx)}
	wasi_snapshot_preview1.MustInstantiate(ctx, w.runtime)
	var pending []byte
	_, err = w.runtime.NewHostModuleBuilder("subako_http").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) int32 {
		body, _ := m.Memory().Read(ptr, size)
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			return -1
		}
		w.sent = append(w.sent, sent)
		pending = []byte(w.answer)
		return int32(len(pending))
	}).Export("request").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) int32 {
		if !m.Memory().Write(ptr, pending[:size]) {
			return -1
		}
		return 0
	}).Export("read_response").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}
	if w.module, err = w.runtime.CompileModule(ctx, bytes); err != nil {
		return nil, err
	}
	return w, nil
}

// session sends each request once the one before it is answered, then
// closes stdin, and returns the answers.
func (w *worker) session(t *testing.T, requests ...map[string]any) []map[string]any {
	t.Helper()
	stdin, toModule := io.Pipe()
	fromModule, stdout := io.Pipe()
	config := wazero.NewModuleConfig().
		WithStdin(stdin).WithStdout(stdout).WithStderr(os.Stderr).
		WithFSConfig(wazero.NewFSConfig().WithDirMount(w.volume, "/volume")).
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().WithRandSource(rand.Reader)
	exited := make(chan error, 1)
	go func() {
		_, err := w.runtime.InstantiateModule(context.Background(), w.module, config)
		stdout.Close()
		exited <- err
	}()
	lines := bufio.NewReader(fromModule)
	var answers []map[string]any
	for i, request := range requests {
		request["jsonrpc"] = "2.0"
		request["id"] = i + 1
		params, _ := request["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    revision,
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "subako", "version": "0"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}
		request["params"] = params
		line, _ := json.Marshal(request)
		go toModule.Write(append(line, '\n'))
		answer := make(chan []byte, 1)
		go func() {
			line, _ := lines.ReadBytes('\n')
			answer <- line
		}()
		select {
		case line := <-answer:
			var message map[string]any
			if err := json.Unmarshal(line, &message); err != nil {
				t.Fatalf("answer to %v is not JSON: %q", request["method"], line)
			}
			answers = append(answers, message)
		case <-time.After(10 * time.Second):
			t.Fatalf("no answer to %v within 10 s", request["method"])
		}
	}
	toModule.Close()
	select {
	case err := <-exited:
		var exit *sys.ExitError
		if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 0) {
			t.Fatalf("the module failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the module did not exit within 1 s of stdin ending")
	}
	return answers
}

func result(t *testing.T, answer map[string]any) map[string]any {
	t.Helper()
	result, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("not a result: %v", answer)
	}
	if result["resultType"] != "complete" {
		t.Errorf("resultType = %v", result["resultType"])
	}
	return result
}

func call(tool string, arguments map[string]any) map[string]any {
	return map[string]any{"method": "tools/call", "params": map[string]any{"name": tool, "arguments": arguments}}
}

func TestTheWorkerSurveysThePackage(t *testing.T) {
	w := newWorker(t)
	answers := w.session(t, map[string]any{"method": "server/discover"}, map[string]any{"method": "tools/list"})
	versions := result(t, answers[0])["supportedVersions"].([]any)
	if !slices.Contains(versions, any(revision)) {
		t.Errorf("supportedVersions = %v, want %s among them", versions, revision)
	}
	var names []string
	for _, tool := range result(t, answers[1])["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if got, _ := json.Marshal(names); string(got) != `["fetch","forecast","plot"]` {
		t.Errorf("tools = %s", got)
	}
}

func TestACallAnswersWithStructuredContent(t *testing.T) {
	w := newWorker(t)
	got := result(t, w.session(t, call("forecast", map[string]any{"city": "東京"}))[0])
	if structured, _ := json.Marshal(got["structuredContent"]); string(structured) != `{"city":"東京","high_c":21.5}` {
		t.Errorf("structuredContent = %s", structured)
	}
}

func TestBadInputIsAToolError(t *testing.T) {
	w := newWorker(t)
	got := result(t, w.session(t, call("forecast", map[string]any{}))[0])
	if got["isError"] != true {
		t.Errorf("isError = %v", got["isError"])
	}
}

func TestNetHTTPGoesThroughSubakoHTTP(t *testing.T) {
	w := newWorker(t)
	w.answer = `{"ok":{"status":200,"headers":{},"body":{"type":"inline","text":"hello"}}}`
	got := result(t, w.session(t, call("fetch", map[string]any{"url": "https://example.com/"}))[0])
	if structured, _ := json.Marshal(got["structuredContent"]); string(structured) != `{"status":200,"text":"hello"}` {
		t.Errorf("structuredContent = %s", structured)
	}
	if len(w.sent) != 1 || w.sent[0]["url"] != "https://example.com/" || w.sent[0]["method"] != "GET" {
		t.Errorf("sent %v", w.sent)
	}
}

func TestAnImageInTheVolumeIsLinked(t *testing.T) {
	w := newWorker(t)
	got := result(t, w.session(t, call("plot", map[string]any{}))[0])
	content := got["content"].([]any)
	link := content[len(content)-1].(map[string]any)
	if link["type"] != "resource_link" || link["uri"] != "volume:///tmp/chart.png" {
		t.Errorf("last block = %v", link)
	}
	if _, err := os.Stat(filepath.Join(w.volume, "tmp", "chart.png")); err != nil {
		t.Error(err)
	}
}
