package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxFileBytes          = 1 << 20
	maxReadBytes          = 64 << 10
	maxSearchMatches      = 100
	maxListEntries        = 1000
	maxCommandBytes       = 8 << 10
	maxCommandOutputBytes = 24 << 10
	defaultCommandTimeout = 30 * time.Second
)

var allowed = map[string]bool{
	"list_files":  true,
	"read_file":   true,
	"search":      true,
	"write_file":  true,
	"edit_file":   true,
	"run_command": true,
}

var errResultLimit = errors.New("result limit reached")

type Call struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type Result struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Denied    bool   `json:"denied,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}
type Approval struct {
	Call    Call
	Summary string
	Risk    string
}
type Approver interface {
	Approve(context.Context, Approval) (bool, error)
}

type ApproverFunc func(context.Context, Approval) (bool, error)

func (f ApproverFunc) Approve(ctx context.Context, approval Approval) (bool, error) {
	return f(ctx, approval)
}

type Runner struct {
	root           string
	mu             sync.RWMutex
	commandTimeout time.Duration
}

func NewRunner(root string) (*Runner, error) {
	a, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	a, e = filepath.EvalSymlinks(a)
	if e != nil {
		return nil, e
	}
	st, e := os.Stat(a)
	if e != nil {
		return nil, e
	}
	if !st.IsDir() {
		return nil, errors.New("workspace is not directory")
	}
	return &Runner{root: a, commandTimeout: defaultCommandTimeout}, nil
}

// SetCommandTimeout configures the maximum duration of run_command calls.
func (r *Runner) SetCommandTimeout(d time.Duration) {
	if d > 0 {
		r.mu.Lock()
		r.commandTimeout = d
		r.mu.Unlock()
	}
}

func (r *Runner) Run(ctx context.Context, c Call, app Approver) Result {
	o := Result{ID: c.ID, Name: c.Name}
	if err := ctx.Err(); err != nil {
		o.Error = err.Error()
		return o
	}
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Name) == "" {
		o.Error = "invalid arguments"
		return o
	}
	if !allowed[c.Name] {
		o.Error = "unknown tool"
		return o
	}
	switch c.Name {
	case "list_files":
		return r.list(ctx, c, o)
	case "read_file":
		return r.read(ctx, c, o)
	case "search":
		return r.search(ctx, c, o)
	}
	switch c.Name {
	case "write_file":
		var a struct{ Path, Content string }
		if err := decode(c.Arguments, &a); err != nil || a.Path == "" || len(a.Content) > maxFileBytes {
			o.Error = "invalid arguments"
			return o
		}
		p, e := r.safePath(a.Path)
		if e != nil {
			o.Denied = true
			o.Error = e.Error()
			return o
		}
		if st, e := os.Stat(filepath.Dir(p)); e != nil || !st.IsDir() {
			o.Error = "parent missing"
			return o
		}
		if app == nil {
			o.Denied = true
			o.Error = "approval required"
			return o
		}
		ok, e := app.Approve(ctx, Approval{c, a.Path, "write"})
		if e != nil {
			o.Error = e.Error()
			return o
		}
		if !ok {
			o.Denied = true
			o.Error = "denied"
			return o
		}
		return r.atomicWrite(ctx, p, []byte(a.Content), o)
	case "edit_file":
		var a struct {
			Path string `json:"path"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := decode(c.Arguments, &a); err != nil || a.Path == "" || strings.TrimSpace(a.Old) == "" {
			o.Error = "invalid arguments"
			return o
		}
		p, e := r.safePath(a.Path)
		if e != nil {
			o.Denied = true
			o.Error = e.Error()
			return o
		}
		b, e := r.readBytes(ctx, p, maxFileBytes+1)
		if e != nil {
			o.Error = e.Error()
			return o
		}
		if strings.Count(string(b), a.Old) != 1 {
			o.Error = "requires exactly one match"
			return o
		}
		n := strings.Replace(string(b), a.Old, a.New, 1)
		if len(n) > maxFileBytes {
			o.Error = "content too large"
			return o
		}
		if app == nil {
			o.Denied = true
			o.Error = "approval required"
			return o
		}
		ok, e := app.Approve(ctx, Approval{c, a.Path, "edit"})
		if e != nil {
			o.Error = e.Error()
			return o
		}
		if !ok {
			o.Denied = true
			o.Error = "denied"
			return o
		}
		return r.atomicWrite(ctx, p, []byte(n), o)
	case "run_command":
		var a struct {
			Command string `json:"command"`
		}
		if err := decode(c.Arguments, &a); err != nil || strings.TrimSpace(a.Command) == "" || len(a.Command) > maxCommandBytes {
			o.Error = "invalid arguments"
			return o
		}
		if app == nil {
			o.Denied = true
			o.Error = "approval required"
			return o
		}
		ok, e := app.Approve(ctx, Approval{c, a.Command, "shell"})
		if e != nil {
			o.Error = e.Error()
			return o
		}
		if !ok {
			o.Denied = true
			o.Error = "denied"
			return o
		}
		return r.command(ctx, a.Command, o)
	}
	return o
}

func decode(raw json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
func (r *Runner) safePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("invalid path")
	}
	if filepath.IsAbs(p) {
		p = filepath.Clean(p)
	} else {
		p = filepath.Join(r.root, p)
	}
	rel, e := filepath.Rel(r.root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path outside workspace")
	}
	cur := r.root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, e := os.Lstat(cur)
		if e != nil {
			if os.IsNotExist(e) {
				break
			}
			return "", e
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink path denied")
		}
	}
	return p, nil
}
func parsePath(raw json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if e := decode(raw, &a); e != nil || a.Path == "" {
		return "", errors.New("invalid arguments")
	}
	return a.Path, nil
}

func (r *Runner) list(ctx context.Context, c Call, o Result) Result {
	var a struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
	}
	if decode(c.Arguments, &a) != nil {
		o.Error = "invalid arguments"
		return o
	}
	if a.Path == "" {
		a.Path = "."
	}
	if a.MaxDepth < 0 || a.MaxDepth > 10 {
		o.Error = "invalid arguments"
		return o
	}
	p, e := r.safePath(a.Path)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	vals := []string{}
	walkErr := filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
		if ce := ctx.Err(); ce != nil {
			return ce
		}
		if err != nil {
			return nil
		}
		if path == p {
			return nil
		}
		if fi.IsDir() && (fi.Name() == ".git" || fi.Name() == "node_modules" || fi.Name() == "bin") {
			return filepath.SkipDir
		}
		relToStart, relErr := filepath.Rel(p, path)
		if relErr != nil {
			return relErr
		}
		depth := len(strings.Split(relToStart, string(os.PathSeparator)))
		if len(vals) >= maxListEntries {
			o.Truncated = true
			return errResultLimit
		}
		rel, relErr := filepath.Rel(r.root, path)
		if relErr != nil {
			return relErr
		}
		vals = append(vals, filepath.ToSlash(rel))
		if fi.IsDir() && depth >= a.MaxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errResultLimit) {
		o.Error = walkErr.Error()
	}
	sort.Strings(vals)
	o.Output = strings.Join(vals, "\n")
	return o
}
func (r *Runner) readBytes(ctx context.Context, p string, n int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(n)))
}
func (r *Runner) read(ctx context.Context, c Call, o Result) Result {
	p, e := parsePath(c.Arguments)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	p, e = r.safePath(p)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	b, e := r.readBytes(ctx, p, maxReadBytes+1)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	if len(b) > maxReadBytes {
		b = b[:maxReadBytes]
		o.Truncated = true
	}
	o.Output = string(b)
	if !utf8.ValidString(o.Output) {
		o.Output = strings.ToValidUTF8(o.Output, "\uFFFD")
	}
	return o
}
func (r *Runner) search(ctx context.Context, c Call, o Result) Result {
	var a struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if decode(c.Arguments, &a) != nil || strings.TrimSpace(a.Query) == "" {
		o.Error = "invalid arguments"
		return o
	}
	if a.Path == "" {
		a.Path = "."
	}
	p, e := r.safePath(a.Path)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	lines := []string{}
	walkErr := filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if fi.Name() == ".git" || fi.Name() == "node_modules" || fi.Name() == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		b, e := r.readBytes(ctx, path, maxFileBytes+1)
		if e != nil || len(b) > maxFileBytes || !utf8.Valid(b) {
			if len(b) > maxFileBytes {
				o.Truncated = true
			}
			return nil
		}
		for i, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, a.Query) {
				rel, _ := filepath.Rel(r.root, path)
				lines = append(lines, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), i+1, l))
				if len(lines) >= maxSearchMatches {
					o.Truncated = true
					return errResultLimit
				}
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errResultLimit) {
		o.Error = walkErr.Error()
	}
	sort.Strings(lines)
	o.Output = strings.Join(lines, "\n")
	return o
}
func (r *Runner) atomicWrite(ctx context.Context, p string, b []byte, o Result) Result {
	if err := ctx.Err(); err != nil {
		o.Error = err.Error()
		return o
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".tmp-")
	if e != nil {
		o.Error = e.Error()
		return o
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Chmod(0600)
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(name, p)
	}
	if e != nil {
		o.Error = e.Error()
	}
	return o
}
func (r *Runner) command(ctx context.Context, command string, o Result) Result {
	r.mu.RLock()
	timeout := r.commandTimeout
	r.mu.RUnlock()
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	cc, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cc, "/bin/sh", "-c", command)
	cmd.Dir = r.root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir(), "LANG=" + os.Getenv("LANG"), "LC_ALL=" + os.Getenv("LC_ALL")}
	w := &limitWriter{n: maxCommandOutputBytes}
	cmd.Stdout = w
	cmd.Stderr = w
	e := cmd.Run()
	o.Output = string(w.b)
	if w.tr {
		o.Truncated = true
	}
	if err := cc.Err(); err != nil {
		o.Error = err.Error()
	} else if e != nil {
		o.Error = e.Error()
	}
	return o
}

type limitWriter struct {
	b  []byte
	n  int
	tr bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if len(w.b) >= w.n {
		w.tr = true
		return len(p), nil
	}
	k := len(p)
	if k > w.n-len(w.b) {
		k = w.n - len(w.b)
		w.tr = true
	}
	w.b = append(w.b, p[:k]...)
	return len(p), nil
}
