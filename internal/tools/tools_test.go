package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testApprover struct {
	n  int
	ok bool
}

func (a *testApprover) Approve(_ context.Context, _ Approval) (bool, error) { a.n++; return a.ok, nil }
func runner(t *testing.T, a Approver) (*Runner, string) {
	d := t.TempDir()
	r, e := NewRunner(d)
	if e != nil {
		t.Fatal(e)
	}
	return r, d
}
func run(r *Runner, a Approver, c Call) Result { return r.Run(context.Background(), c, a) }
func call(n string, v any) Call                { b, _ := json.Marshal(v); return Call{ID: "1", Name: n, Arguments: b} }
func TestUnknownAndInvalidCallsNeverApprove(t *testing.T) {
	a := &testApprover{ok: true}
	r, _ := runner(t, a)
	r.Run(context.Background(), Call{Name: "bogus"}, a)
	r.Run(context.Background(), call("write_file", map[string]any{"path": ""}), a)
	if a.n != 0 {
		t.Fatal(a.n)
	}
}
func TestReadToolsAutoAllowInsideWorkspace(t *testing.T) {
	r, d := runner(t, nil)
	os.WriteFile(filepath.Join(d, "x"), []byte("ok"), 0600)
	if run(r, nil, call("read_file", map[string]string{"path": "x"})).Output != "ok" {
		t.Fatal()
	}
}
func TestWriteEditCommandRequireApproval(t *testing.T) {
	a := &testApprover{ok: true}
	r, _ := runner(t, a)
	r.Run(context.Background(), call("write_file", map[string]string{"path": "x", "content": "x"}), a)
	r.Run(context.Background(), call("edit_file", map[string]string{"path": "x", "old": "x", "new": "y"}), a)
	r.Run(context.Background(), call("run_command", map[string]string{"command": "true"}), a)
	if a.n != 3 {
		t.Fatal(a.n)
	}
}
func TestDeniedCallsHaveNoSideEffects(t *testing.T) {
	a := &testApprover{}
	r, d := runner(t, a)
	res := r.Run(context.Background(), call("write_file", map[string]string{"path": "x", "content": "x"}), a)
	if !res.Denied {
		t.Fatal()
	}
	if _, e := os.Stat(filepath.Join(d, "x")); !os.IsNotExist(e) {
		t.Fatal()
	}
}
func TestTraversalAbsoluteAndSymlinkEscapesDenied(t *testing.T) {
	r, d := runner(t, nil)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../x", outside, "link"} {
		res := run(r, nil, call("read_file", map[string]string{"path": path}))
		if res.Error == "" || res.Output != "" {
			t.Fatalf("path %q escaped: %+v", path, res)
		}
	}
}
func TestMissingLeafSymlinkParentDenied(t *testing.T) {
	r, d := runner(t, &testApprover{ok: true})
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background(), call("write_file", map[string]string{"path": "link/new", "content": "x"}), &testApprover{ok: true})
	if !res.Denied || !strings.Contains(res.Error, "symlink") {
		t.Fatalf("%+v", res)
	}
}

func TestSearchDoesNotFollowSymlinkFiles(t *testing.T) {
	r, d := runner(t, nil)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("private-token"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d, "linked-secret")); err != nil {
		t.Fatal(err)
	}
	result := run(r, nil, call("search", map[string]string{"path": ".", "query": "private-token"}))
	if result.Error != "" || result.Output != "" {
		t.Fatalf("search followed workspace symlink: %+v", result)
	}
}
func TestListDeterministicDepthSkipAndLimit(t *testing.T) {
	r, d := runner(t, nil)
	for _, path := range []string{"sub/a", "sub/nested/b", "sub/.git/hidden", "sub/node_modules/hidden", "sub/bin/hidden"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(d, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, path), []byte{}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	res := run(r, nil, call("list_files", map[string]any{"path": "sub", "max_depth": 1}))
	if res.Error != "" || res.Output != "sub/a\nsub/nested" {
		t.Fatal(res)
	}
}

func TestListLimitIsEnforcedDuringWalk(t *testing.T) {
	r, d := runner(t, nil)
	for i := 0; i < 1001; i++ {
		path := filepath.Join(d, fmt.Sprintf("%04d", i))
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := run(r, nil, call("list_files", map[string]any{"path": ".", "max_depth": 1}))
	if !result.Truncated || len(strings.Split(result.Output, "\n")) != 1000 {
		t.Fatalf("list was not capped: entries=%d result=%+v", len(strings.Split(result.Output, "\n")), result)
	}
}
func TestReadLimitAndUTF8(t *testing.T) {
	r, d := runner(t, nil)
	os.WriteFile(filepath.Join(d, "x"), append([]byte{0xff}, make([]byte, 65536)...), 0600)
	res := run(r, nil, call("read_file", map[string]string{"path": "x"}))
	if !res.Truncated || !strings.HasPrefix(res.Output, "�") {
		t.Fatal()
	}
}
func TestSearchLiteralDeterministicAndLimited(t *testing.T) {
	r, d := runner(t, nil)
	content := strings.Repeat("a[b]\n", 101)
	if err := os.WriteFile(filepath.Join(d, "x"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	result := run(r, nil, call("search", map[string]string{"path": ".", "query": "[b]"}))
	if !strings.Contains(result.Output, "x:1") || !result.Truncated || len(strings.Split(result.Output, "\n")) != 100 {
		t.Fatalf("literal search was not capped: %+v", result)
	}
}
func TestWriteAtomic0600(t *testing.T) {
	a := &testApprover{ok: true}
	r, d := runner(t, a)
	r.Run(context.Background(), call("write_file", map[string]string{"path": "x", "content": "x"}), a)
	st, e := os.Stat(filepath.Join(d, "x"))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal(e, st.Mode())
	}
}
func TestEditRequiresExactlyOneMatch(t *testing.T) {
	a := &testApprover{ok: true}
	r, d := runner(t, a)
	os.WriteFile(filepath.Join(d, "x"), []byte("xx"), 0600)
	if r.Run(context.Background(), call("edit_file", map[string]string{"path": "x", "old": "x", "new": "y"}), a).Error == "" {
		t.Fatal()
	}
}
func TestCommandTimeoutCancellationOutputAndEnvironment(t *testing.T) {
	a := &testApprover{ok: true}
	r, _ := runner(t, a)
	r.SetCommandTimeout(20 * time.Millisecond)
	if result := r.Run(context.Background(), call("run_command", map[string]string{"command": "sleep 1"}), a); !strings.Contains(result.Error, "deadline") {
		t.Fatalf("timeout result=%+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := r.Run(ctx, call("run_command", map[string]string{"command": "echo hi"}), a); !strings.Contains(result.Error, "canceled") {
		t.Fatalf("cancellation result=%+v", result)
	}
}

func TestCommandUsesSanitizedEnvironment(t *testing.T) {
	t.Setenv("KINGDOM_PRIVATE_TEST_VALUE", "must-not-leak")
	a := &testApprover{ok: true}
	r, _ := runner(t, a)
	result := r.Run(context.Background(), call("run_command", map[string]string{"command": `if [ -z "$KINGDOM_PRIVATE_TEST_VALUE" ]; then printf clean; fi`}), a)
	if result.Error != "" || result.Output != "clean" {
		t.Fatalf("environment was not sanitized: %+v", result)
	}
}

type errorApprover struct{}

func (errorApprover) Approve(context.Context, Approval) (bool, error) {
	return false, errors.New("approval transport failed")
}

func TestApprovalErrorsRemainDistinctFromDenials(t *testing.T) {
	r, d := runner(t, nil)
	result := r.Run(context.Background(), call("write_file", map[string]string{"path": "x", "content": "x"}), errorApprover{})
	if result.Denied || !strings.Contains(result.Error, "approval transport failed") {
		t.Fatalf("approval error collapsed into denial: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(d, "x")); !os.IsNotExist(err) {
		t.Fatal("approval error caused a write")
	}
}

func TestStrictJSONAndValidationNeverApprove(t *testing.T) {
	a := &testApprover{ok: true}
	r, d := runner(t, a)
	for _, c := range []Call{
		{ID: "", Name: "read_file", Arguments: json.RawMessage(`{"path":"x"}`)},
		{ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x","content":"x","extra":1}`)},
		call("write_file", map[string]string{"path": "missing/x", "content": "x"}),
		call("edit_file", map[string]string{"path": "x", "old": "x", "new": "y"}),
	} {
		r.Run(context.Background(), c, a)
	}
	if a.n != 0 {
		t.Fatalf("approval count=%d", a.n)
	}
	if _, err := os.Stat(filepath.Join(d, "x")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestInvalidMutationIsReportedBeforeMissingApproval(t *testing.T) {
	r, _ := runner(t, nil)
	result := r.Run(context.Background(), call("write_file", map[string]string{"path": "", "content": "x"}), nil)
	if result.Denied || result.Error != "invalid arguments" {
		t.Fatalf("validation order is unclear: %+v", result)
	}
}

func TestCommandOutputTruncatedAndCwd(t *testing.T) {
	a := &testApprover{ok: true}
	r, d := runner(t, a)
	r.SetCommandTimeout(time.Second)
	res := r.Run(context.Background(), call("run_command", map[string]string{"command": "pwd; printf 'x%.0s' $(seq 1 50000)"}), a)
	realD, _ := filepath.EvalSymlinks(d)
	if res.Error != "" || !res.Truncated || !strings.HasPrefix(res.Output, realD) {
		t.Fatalf("%+v", res)
	}
}

func TestCommandTimeoutDoesNotWaitForChildProcess(t *testing.T) {
	a := &testApprover{ok: true}
	r, _ := runner(t, a)
	r.SetCommandTimeout(20 * time.Millisecond)
	started := time.Now()
	result := r.Run(context.Background(), call("run_command", map[string]string{"command": "sleep 2"}), a)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond || !strings.Contains(result.Error, "deadline") {
		t.Fatalf("timeout took %s: %+v", elapsed, result)
	}
}
func TestNilApproverDenies(t *testing.T) {
	r, _ := runner(t, nil)
	if !r.Run(context.Background(), call("write_file", map[string]string{"path": "x", "content": "x"}), nil).Denied {
		t.Fatal()
	}
}
func TestConcurrentCallsRaceSafe(t *testing.T) {
	r, d := runner(t, nil)
	os.WriteFile(filepath.Join(d, "x"), []byte("x"), 0600)
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func() {
			r.Run(context.Background(), call("read_file", map[string]string{"path": "x"}), nil)
			done <- true
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
