package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatterAndFallbackMetadata(t *testing.T) {
	skill, err := Parse([]byte("---\nname: careful-coder\ndescription: Work test-first.\n---\n\nWrite a failing test before implementation.\n"), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "careful-coder" || skill.Description != "Work test-first." || skill.Instructions != "Write a failing test before implementation." {
		t.Fatalf("skill=%+v", skill)
	}

	fallback, err := Parse([]byte("Review the result carefully. Then explain risks."), "Code Review.md")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Name != "code-review" || fallback.Description != "Review the result carefully." {
		t.Fatalf("fallback=%+v", fallback)
	}
}

func TestParseRejectsMalformedEmptyAndOversizedSkills(t *testing.T) {
	cases := [][]byte{
		{},
		{0xff, 0xfe},
		[]byte("---\nname: broken\nbody without closing delimiter"),
		[]byte("---\nname: !!!\n---\nbody"),
		[]byte("---\nname: valid\nunknown: no\n---\nbody"),
		[]byte(strings.Repeat("x", MaxFileBytes+1)),
	}
	for i, input := range cases {
		if _, err := Parse(input, "fallback.md"); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestLibraryLoadsFlatAndDirectorySkillsDeterministically(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "z-last.md"), "---\nname: z-last\ndescription: Last.\n---\nZ")
	writeSkill(t, filepath.Join(dir, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: First.\n---\nA")
	writeSkill(t, filepath.Join(dir, "ignored.txt"), "ignore")

	library := NewLibrary(dir, nil)
	got, err := library.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "z-last" {
		t.Fatalf("skills=%+v", got)
	}
	if got[0].Path != filepath.Join(dir, "alpha", "SKILL.md") || got[1].Path != filepath.Join(dir, "z-last.md") {
		t.Fatalf("paths=%+v", got)
	}
}

func TestLibrarySkipsSymlinksAndUserSkillOverridesBuiltIn(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeSkill(t, outside, "---\nname: escaped\n---\nNever load me")
	if err := os.Symlink(outside, filepath.Join(dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(dir, "careful-coder.md"), "---\nname: careful-coder\ndescription: Custom.\n---\nCustom instructions")

	builtIn := Skill{Name: "careful-coder", Description: "Built in.", Instructions: "Built-in instructions", BuiltIn: true}
	got, err := NewLibrary(dir, []Skill{builtIn}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].BuiltIn || got[0].Instructions != "Custom instructions" {
		t.Fatalf("skills=%+v", got)
	}
}

func TestDirectorySkillWinsOverFlatSkillWithSameName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "same.md"), "---\nname: same\n---\nFlat instructions")
	writeSkill(t, filepath.Join(dir, "same", "SKILL.md"), "---\nname: same\n---\nDirectory instructions")

	got, err := NewLibrary(dir, nil).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Instructions != "Directory instructions" {
		t.Fatalf("skills=%+v", got)
	}
}

func TestLibraryReturnsValidSkillsAlongsideMalformedFileError(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "good.md"), "---\nname: good\n---\nUseful instructions")
	writeSkill(t, filepath.Join(dir, "bad.md"), "---\nname: bad\nmissing delimiter")

	got, err := NewLibrary(dir, nil).Load()
	if err == nil || !strings.Contains(err.Error(), "bad.md") {
		t.Fatalf("error=%v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("skills=%+v", got)
	}
}

func TestDefaultBuiltInsAndBoundedPromptRendering(t *testing.T) {
	builtIns := DefaultBuiltIns()
	if len(builtIns) == 0 || builtIns[0].Name == "" || !builtIns[0].BuiltIn {
		t.Fatalf("built-ins=%+v", builtIns)
	}
	active := []Skill{
		{Name: "one", Description: "First.", Instructions: strings.Repeat("a", MaxPromptBytes)},
		{Name: "two", Description: "Second.", Instructions: "must be truncated"},
	}
	prompt, truncated := Render(active)
	if !truncated || len(prompt) > MaxPromptBytes || !strings.Contains(prompt, "Skill: one") {
		t.Fatalf("len=%d truncated=%v prompt=%q", len(prompt), truncated, prompt)
	}
}

func TestBoundedPromptEndsOnUTF8Boundary(t *testing.T) {
	prompt, truncated := Render([]Skill{{Name: "unicode", Instructions: strings.Repeat("界", MaxPromptBytes)}})
	if !truncated || !strings.HasPrefix(prompt, "Skill: unicode") || strings.Contains(prompt, "�") {
		t.Fatalf("invalid bounded prompt: len=%d truncated=%v", len(prompt), truncated)
	}
}

func writeSkill(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
