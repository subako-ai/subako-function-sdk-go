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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	// What subako_http answers each request with, and the requests it was sent.
	answer func(sent map[string]any) string
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
	shared.answer = func(map[string]any) string {
		return `{"error":{"kind":"host_not_allowed","message":"this function has no network access"}}`
	}
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
	build := exec.Command("go", "build", "-o", wasm, "./examples/github")
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
		pending = []byte(w.answer(sent))
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

// github answers GitHub API requests with bodies by path, and saves a
// requested file into the volume as the worker would.
func (w *worker) github(bodies map[string]string) {
	w.answer = func(sent map[string]any) string {
		target, _ := url.Parse(sent["url"].(string))
		if output, ok := sent["output"].(map[string]any); ok && output["type"] == "file" {
			inside := strings.TrimPrefix(output["path"].(string), "/volume/")
			file := filepath.Join(w.volume, filepath.FromSlash(inside))
			os.MkdirAll(filepath.Dir(file), 0o755)
			os.WriteFile(file, []byte("\x89PNG"), 0o644)
			return fmt.Sprintf(`{"ok":{"status":200,"headers":{},"body":{"type":"file","path":%q,"bytes":4}}}`, output["path"])
		}
		body, ok := bodies[target.Path]
		if !ok {
			return `{"ok":{"status":404,"headers":{},"body":{"type":"inline","text":"{\"message\":\"Not Found\"}"}}}`
		}
		text, _ := json.Marshal(body)
		return fmt.Sprintf(`{"ok":{"status":200,"headers":{"content-type":"application/json"},"body":{"type":"inline","text":%s}}}`, text)
	}
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
	if got, _ := json.Marshal(names); string(got) != `["avatar","issues","repo"]` {
		t.Errorf("tools = %s", got)
	}
}

func TestARepositoryComesBackAsStructuredContent(t *testing.T) {
	w := newWorker(t)
	w.github(map[string]string{"/repos/golang/go": `{"full_name":"golang/go","stargazers_count":130000,"forks_count":18000,"open_issues_count":9000,"default_branch":"master","html_url":"https://github.com/golang/go","description":"The Go programming language"}`})
	got := result(t, w.session(t, call("repo", map[string]any{"owner": "golang", "repo": "go"}))[0])
	structured := got["structuredContent"].(map[string]any)
	if structured["full_name"] != "golang/go" || structured["stargazers_count"] != 130000.0 {
		t.Errorf("structuredContent = %v", structured)
	}
	if len(w.sent) != 1 || w.sent[0]["method"] != "GET" || w.sent[0]["url"] != "https://api.github.com/repos/golang/go" {
		t.Errorf("sent %v", w.sent)
	}
	if accept := w.sent[0]["headers"].(map[string]any)["accept"]; accept != "application/vnd.github+json" {
		t.Errorf("accept = %v", accept)
	}
}

func TestAMissingRepositoryIsAToolError(t *testing.T) {
	w := newWorker(t)
	w.github(nil)
	got := result(t, w.session(t, call("repo", map[string]any{"owner": "nobody", "repo": "nothing"}))[0])
	if got["isError"] != true {
		t.Errorf("isError = %v", got["isError"])
	}
}

func TestIssuesLeaveOutPullRequests(t *testing.T) {
	w := newWorker(t)
	w.github(map[string]string{"/repos/golang/go/issues": `[
		{"number":2,"title":"a bug","user":{"login":"gopher"},"comments":3,"html_url":"https://github.com/golang/go/issues/2"},
		{"number":1,"title":"a fix","user":{"login":"gopher"},"comments":0,"html_url":"https://github.com/golang/go/pull/1","pull_request":{}}
	]`})
	got := result(t, w.session(t, call("issues", map[string]any{"owner": "golang", "repo": "go", "label": "NeedsFix"}))[0])
	want := map[string]any{"issues": []any{map[string]any{
		"number": 2.0, "title": "a bug", "author": "gopher", "comments": 3.0, "url": "https://github.com/golang/go/issues/2",
	}}}
	if !reflect.DeepEqual(got["structuredContent"], want) {
		t.Errorf("structuredContent = %v", got["structuredContent"])
	}
	if sent := w.sent[0]["url"]; sent != "https://api.github.com/repos/golang/go/issues?labels=NeedsFix&per_page=10&state=open" {
		t.Errorf("url = %v", sent)
	}
}

func TestBadInputIsAToolError(t *testing.T) {
	w := newWorker(t)
	got := result(t, w.session(t, call("repo", map[string]any{"owner": "golang"}))[0])
	if got["isError"] != true {
		t.Errorf("isError = %v", got["isError"])
	}
	if len(w.sent) != 0 {
		t.Errorf("sent %v", w.sent)
	}
}

func TestAnAvatarIsSavedToTheVolumeAndShown(t *testing.T) {
	w := newWorker(t)
	w.github(nil)
	got := result(t, w.session(t, call("avatar", map[string]any{"owner": "golang"}))[0])
	content := got["content"].([]any)
	link := content[len(content)-1].(map[string]any)
	if link["type"] != "resource_link" || link["uri"] != "volume:///tmp/avatar-golang.png" {
		t.Errorf("last block = %v", link)
	}
	if sent := w.sent[0]["url"]; sent != "https://github.com/golang.png?size=200" {
		t.Errorf("url = %v", sent)
	}
	if _, err := os.Stat(filepath.Join(w.volume, "tmp", "avatar-golang.png")); err != nil {
		t.Error(err)
	}
}
