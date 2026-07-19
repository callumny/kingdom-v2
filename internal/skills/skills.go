// Package skills loads reusable Markdown instructions for the King.
package skills

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxFileBytes   = 64 << 10
	MaxPromptBytes = 32 << 10
	maxSkills      = 256
)

type Skill struct {
	Name         string
	Description  string
	Instructions string
	Path         string
	BuiltIn      bool
}

type Library struct {
	dir      string
	builtIns []Skill
}

func NewLibrary(dir string, builtIns []Skill) *Library {
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	return &Library{dir: filepath.Clean(dir), builtIns: append([]Skill(nil), builtIns...)}
}

func DefaultBuiltIns() []Skill {
	return []Skill{{
		Name:         "careful-coder",
		Description:  "Use a test-first workflow and explain important tradeoffs.",
		Instructions: strings.TrimSpace(`For code changes, first identify the observable behaviour and write a failing test. Implement the smallest clear change that makes it pass, then refactor while keeping tests green. State important security, performance, and maintainability tradeoffs in the final explanation.`),
		BuiltIn:      true,
	}}
}

func (l *Library) Dir() string { return l.dir }

func (l *Library) EnsureDir() error {
	return os.MkdirAll(l.dir, 0700)
}

// Load returns every valid skill even when one or more user files are invalid.
func (l *Library) Load() ([]Skill, error) {
	byName := make(map[string]Skill)
	priority := make(map[string]int)
	for _, skill := range l.builtIns {
		if skill.Name == "" || strings.TrimSpace(skill.Instructions) == "" {
			continue
		}
		key := strings.ToLower(skill.Name)
		byName[key] = skill
		priority[key] = 0
	}

	entries, err := os.ReadDir(l.dir)
	if os.IsNotExist(err) {
		return sortedSkills(byName), nil
	}
	if err != nil {
		return sortedSkills(byName), fmt.Errorf("read skills directory: %w", err)
	}

	var loadErrors []error
	considered := 0
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(l.dir, entry.Name())
		fallback := entry.Name()
		sourcePriority := 1
		switch {
		case entry.IsDir():
			path = filepath.Join(path, "SKILL.md")
			fallback = entry.Name()
			sourcePriority = 2
		case !isMarkdown(entry.Name()):
			continue
		}
		considered++
		if considered > maxSkills {
			loadErrors = append(loadErrors, fmt.Errorf("skill limit of %d reached", maxSkills))
			break
		}

		skill, parseErr := parseFile(path, fallback)
		if os.IsNotExist(parseErr) && entry.IsDir() {
			continue
		}
		if parseErr != nil {
			loadErrors = append(loadErrors, fmt.Errorf("%s: %w", filepath.Base(path), parseErr))
			continue
		}
		skill.Path = path
		key := strings.ToLower(skill.Name)
		if currentPriority, exists := priority[key]; !exists || sourcePriority > currentPriority {
			byName[key] = skill
			priority[key] = sourcePriority
		}
	}

	return sortedSkills(byName), errors.Join(loadErrors...)
}

func Parse(data []byte, fallbackName string) (Skill, error) {
	if len(data) == 0 {
		return Skill{}, errors.New("empty skill")
	}
	if len(data) > MaxFileBytes {
		return Skill{}, fmt.Errorf("skill exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(data) {
		return Skill{}, errors.New("skill is not valid UTF-8")
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	metadata := make(map[string]string)
	body := text
	if strings.HasPrefix(text, "---\n") {
		rest := strings.TrimPrefix(text, "---\n")
		end := strings.Index(rest, "\n---\n")
		if end < 0 {
			return Skill{}, errors.New("unterminated frontmatter")
		}
		for _, line := range strings.Split(rest[:end], "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				return Skill{}, fmt.Errorf("invalid frontmatter line %q", line)
			}
			key = strings.ToLower(strings.TrimSpace(key))
			if key != "name" && key != "description" {
				return Skill{}, fmt.Errorf("unknown frontmatter key %q", key)
			}
			metadata[key] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
		body = rest[end+len("\n---\n"):]
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return Skill{}, errors.New("empty instructions")
	}
	name := metadata["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(fallbackName), filepath.Ext(fallbackName))
	}
	name = slug(name)
	if name == "" {
		return Skill{}, errors.New("invalid skill name")
	}
	description := strings.TrimSpace(metadata["description"])
	if description == "" {
		description = firstSentence(body)
	}
	return Skill{Name: name, Description: description, Instructions: body}, nil
}

// Render creates the bounded system-prompt block for active skills.
func Render(active []Skill) (string, bool) {
	var builder strings.Builder
	for _, skill := range active {
		var block strings.Builder
		fmt.Fprintf(&block, "Skill: %s\n", skill.Name)
		if skill.Description != "" {
			fmt.Fprintf(&block, "Description: %s\n", skill.Description)
		}
		block.WriteString("Instructions:\n")
		block.WriteString(strings.TrimSpace(skill.Instructions))
		separator := ""
		if builder.Len() > 0 {
			separator = "\n\n"
		}
		value := separator + block.String()
		remaining := MaxPromptBytes - builder.Len()
		if len(value) > remaining {
			value = value[:remaining]
			for !utf8.ValidString(value) {
				value = value[:len(value)-1]
			}
			builder.WriteString(value)
			return builder.String(), true
		}
		builder.WriteString(value)
	}
	return builder.String(), false
}

func parseFile(path, fallback string) (Skill, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Skill{}, err
	}
	if !info.Mode().IsRegular() {
		return Skill{}, errors.New("skill must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return Skill{}, err
	}
	return Parse(data, fallback)
}

func sortedSkills(byName map[string]Skill) []Skill {
	out := make([]Skill, 0, len(byName))
	for _, skill := range byName {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left == right {
			return out[i].Name < out[j].Name
		}
		return left < right
	})
	return out
}

func isMarkdown(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".md" || extension == ".markdown"
}

func slug(value string) string {
	var builder strings.Builder
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			builder.WriteRune(char)
			dash = false
		case builder.Len() > 0 && !dash:
			builder.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func firstSentence(value string) string {
	value = strings.TrimSpace(value)
	for index, char := range value {
		if char == '.' || char == '!' || char == '?' || char == '\n' {
			return strings.TrimSpace(value[:index+1])
		}
	}
	const maxDescription = 160
	runes := []rune(value)
	if len(runes) > maxDescription {
		return strings.TrimSpace(string(runes[:maxDescription])) + "…"
	}
	return value
}
