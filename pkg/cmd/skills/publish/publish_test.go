package publish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/require"
)

func testPublishGitClient(t *testing.T, remoteURLs map[string]string) *git.Client {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	runGit("init", "--initial-branch=main")
	runGit("config", "user.email", "monalisa@github.com")
	runGit("config", "user.name", "Monalisa Octocat")
	for name, url := range remoteURLs {
		runGit("remote", "add", name, url)
	}
	return &git.Client{RepoDir: dir}
}

func TestPublishCmd_Help(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := stubFactory(ios)
	cmd := NewCmdPublish(&f, nil)
	if cmd.Use == "" {
		t.Error("publish command has no Use string")
	}
	if cmd.Short == "" {
		t.Error("publish command has no Short description")
	}
}

func TestPublishCmd_Alias(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := stubFactory(ios)
	cmd := NewCmdPublish(&f, nil)
	found := false
	for _, alias := range cmd.Aliases {
		if alias == "validate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("publish command should have 'validate' alias")
	}
}

func TestPublish_ValidSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "git-commit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: git-commit
description: A skill for writing good git commits
allowed-tools: git
license: MIT
---
You are a git commit expert.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/topics"),
		httpmock.JSONResponse(map[string]interface{}{
			"names": []string{"agent-skills"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/tags"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"name": "v1.0.0"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/rulesets"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
		GitClient: testPublishGitClient(t, map[string]string{
			"origin": "https://github.com/test/skills-repo.git",
		}),
		client: api.NewClientFromHTTP(&http.Client{Transport: reg}),
		host:   "github.com",
	}

	err := publishRun(opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "ok") {
		t.Errorf("expected 'ok' output, got: %s", out)
	}
}

func TestPublish_MissingName(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "git-commit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
description: A skill for writing good git commits
---
Body text.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	out := stdout.String()
	if !strings.Contains(out, "missing required field: name") {
		t.Errorf("expected name error in output, got: %s", out)
	}
}

func TestPublish_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "git-commit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: wrong-name
description: A skill
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for name mismatch")
	}

	out := stdout.String()
	if !strings.Contains(out, "does not match directory name") {
		t.Errorf("expected name mismatch error, got: %s", out)
	}
}

func TestPublish_NonSpecCompliantName(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "My_Skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: My_Skill
description: A skill with non-compliant name
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for non-spec-compliant name")
	}

	out := stdout.String()
	if !strings.Contains(out, "naming convention") {
		t.Errorf("expected naming convention error, got: %s", out)
	}
}

func TestPublish_AllowedToolsArray(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "bad-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: bad-tools
description: A skill with array allowed-tools
allowed-tools:
  - git
  - curl
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for array allowed-tools")
	}

	out := stdout.String()
	if !strings.Contains(out, "allowed-tools must be a string") {
		t.Errorf("expected allowed-tools error, got: %s", out)
	}
}

func TestPublish_StripMetadata(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: test-skill
description: A test skill
metadata:
    github-owner: someone
    github-repo: something
    github-ref: v1.0.0
    github-sha: abc123
    github-tree-sha: def456
---
Body.
`
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, _, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
		Fix: true,
	}

	err := publishRun(opts)
	if err != nil {
		t.Fatalf("expected no error with --fix, got: %v", err)
	}

	fixed, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}

	fixedStr := string(fixed)
	if strings.Contains(fixedStr, "github-owner") {
		t.Errorf("expected github-owner to be stripped, got:\n%s", fixedStr)
	}
	if strings.Contains(fixedStr, "github-sha") {
		t.Errorf("expected github-sha to be stripped, got:\n%s", fixedStr)
	}
	if strings.Contains(fixedStr, "metadata:") {
		t.Errorf("expected empty metadata map to be removed, got:\n%s", fixedStr)
	}
}

func TestPublish_MetadataWithoutFix(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: test-skill
description: A test skill
metadata:
    github-owner: someone
    github-sha: abc123
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
		Fix: false,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error without --fix when metadata present")
	}

	out := stdout.String()
	if !strings.Contains(out, "install metadata") {
		t.Errorf("expected install metadata error, got: %s", out)
	}
	if !strings.Contains(out, "--fix") {
		t.Errorf("expected --fix suggestion, got: %s", out)
	}
}

func TestPublish_NoSkillsDir(t *testing.T) {
	dir := t.TempDir()
	ios, _, _, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for missing skills/ directory")
	}
	if !strings.Contains(err.Error(), "no skills/ directory") {
		t.Errorf("expected 'no skills/ directory' error, got: %v", err)
	}
}

func TestPublish_MissingSKILLMD(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "empty-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err == nil {
		t.Fatal("expected error for missing SKILL.md")
	}

	out := stdout.String()
	if !strings.Contains(out, "missing SKILL.md") {
		t.Errorf("expected missing SKILL.md error, got: %s", out)
	}
}

func TestPublish_DryRun(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "good-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: good-skill
description: A good skill
license: MIT
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, _, stderr := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/topics"),
		httpmock.JSONResponse(map[string]interface{}{
			"names": []string{"agent-skills"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/tags"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"name": "v1.0.0"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo/rulesets"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/test/skills-repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)

	opts := &publishOptions{
		IO:     ios,
		Dir:    dir,
		DryRun: true,
		GitClient: testPublishGitClient(t, map[string]string{
			"origin": "https://github.com/test/skills-repo.git",
		}),
		client: api.NewClientFromHTTP(&http.Client{Transport: reg}),
		host:   "github.com",
	}

	err := publishRun(opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "Dry run complete") {
		t.Errorf("stderr should confirm dry run, got: %s", errOut)
	}
}

func TestPublish_LicenseWarning(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "no-license")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: no-license
description: A skill without license
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ios, _, stdout, _ := iostreams.Test()

	opts := &publishOptions{
		IO:  ios,
		Dir: dir,
	}

	err := publishRun(opts)
	if err != nil {
		t.Fatalf("expected no error (warnings only), got: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "license") {
		t.Errorf("expected license warning, got: %s", out)
	}
}

func TestSuggestNextTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "v1.0.1"},
		{"v2.3.4", "v2.3.5"},
		{"1.0.0", "1.0.1"},
		{"v0.0.9", "v0.0.10"},
		{"not-semver", ""},
		{"v1", ""},
		{"v1.0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := suggestNextTag(tt.input)
			if got != tt.want {
				t.Errorf("suggestNextTag(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
	}{
		{"git@github.com:github/gh-skills.git", "github", "gh-skills"},
		{"https://github.com/github/gh-skills.git", "github", "gh-skills"},
		{"https://github.com/github/gh-skills", "github", "gh-skills"},
		{"git@github.com:owner/repo.git", "owner", "repo"},
		{"https://gitlab.com/owner/repo.git", "", ""},
		{"not-a-url", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo := parseGitHubURL(tt.url)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("parseGitHubURL(%q) = (%q, %q), want (%q, %q)", tt.url, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestRepoHasTopic(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/topics"),
		httpmock.JSONResponse(map[string]interface{}{
			"names": []string{"golang", "agent-skills"},
		}),
	)

	if !repoHasTopic(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo") {
		t.Error("expected true when topic present")
	}
}

func TestRepoHasTopic_Missing(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/topics"),
		httpmock.JSONResponse(map[string]interface{}{
			"names": []string{"golang"},
		}),
	)

	if repoHasTopic(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo") {
		t.Error("expected false when topic missing")
	}
}

func TestFetchTags_NoTags(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/tags"),
		httpmock.JSONResponse([]interface{}{}),
	)

	tags := fetchTags(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo")
	if len(tags) != 0 {
		t.Errorf("expected no tags, got %d", len(tags))
	}
}

func TestFetchTags_WithTags(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/tags"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"name": "v1.2.3"},
		}),
	)

	tags := fetchTags(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo")
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "v1.2.3" {
		t.Errorf("expected v1.2.3, got %s", tags[0].Name)
	}
}

func TestCheckTagProtection_Active(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/rulesets"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"id": 1, "name": "protect-tags", "target": "tag", "enforcement": "active"},
		}),
	)

	diags := checkTagProtection(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo")
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics when tag protection active, got: %v", diags)
	}
}

func TestCheckTagProtection_Missing(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/rulesets"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"id": 1, "name": "branch-protection", "target": "branch", "enforcement": "active"},
		}),
	)

	diags := checkTagProtection(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if !strings.Contains(diags[0].message, "tag protection") {
		t.Errorf("expected tag protection warning, got: %s", diags[0].message)
	}
}

func TestCheckSecuritySettings_AllEnabled(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)

	skillsDir := t.TempDir()

	diags := checkSecuritySettings(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo", skillsDir)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics when all security enabled, got %d: %v", len(diags), diags)
	}
}

func TestCheckSecuritySettings_NoneEnabled(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "disabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "disabled"},
			},
		}),
	)

	skillsDir := t.TempDir()

	diags := checkSecuritySettings(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo", skillsDir)
	if len(diags) != 2 {
		t.Errorf("expected 2 diagnostics (secret scanning + push protection), got %d: %v", len(diags), diags)
	}
	for _, d := range diags {
		if d.severity != "warning" {
			t.Errorf("secret scanning diagnostics should be warnings, got %q: %s", d.severity, d.message)
		}
	}
}

func TestCheckSecuritySettings_WithCodeFiles(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/code-scanning/alerts"),
		httpmock.StatusStringResponse(404, "not found"),
	)

	skillsDir := t.TempDir()
	scriptDir := filepath.Join(skillsDir, "my-skill", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "helper.sh"), []byte("#!/bin/bash"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := checkSecuritySettings(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo", skillsDir)
	hasCodeScanInfo := false
	for _, d := range diags {
		if strings.Contains(d.message, "code scanning") {
			hasCodeScanInfo = true
			if d.severity != "info" {
				t.Errorf("code scanning suggestion should be info, got %q", d.severity)
			}
		}
	}
	if !hasCodeScanInfo {
		t.Error("expected code scanning info when code files present")
	}
}

func TestCheckSecuritySettings_WithManifests(t *testing.T) {
	reg := &httpmock.Registry{}
	defer reg.Verify(t)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo"),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/vulnerability-alerts"),
		httpmock.StatusStringResponse(404, "not found"),
	)

	skillsDir := t.TempDir()
	skillDir := filepath.Join(skillsDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := checkSecuritySettings(api.NewClientFromHTTP(&http.Client{Transport: reg}), "github.com", "owner", "repo", skillsDir)
	hasDependabotInfo := false
	for _, d := range diags {
		if strings.Contains(d.message, "Dependabot") {
			hasDependabotInfo = true
			if d.severity != "info" {
				t.Errorf("Dependabot suggestion should be info, got %q", d.severity)
			}
		}
	}
	if !hasDependabotInfo {
		t.Error("expected Dependabot info when manifest files present")
	}
}

func TestDetectCodeAndManifests(t *testing.T) {
	dir := t.TempDir()

	hasCode, hasManifests := detectCodeAndManifests(dir)
	if hasCode || hasManifests {
		t.Error("empty dir should have no code or manifests")
	}

	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasCode, hasManifests = detectCodeAndManifests(dir)
	if !hasCode {
		t.Error("should detect .sh as code")
	}
	if hasManifests {
		t.Error("should not detect manifests")
	}

	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasCode, hasManifests = detectCodeAndManifests(dir)
	if !hasCode || !hasManifests {
		t.Error("should detect both code and manifests")
	}
}

func TestCheckInstalledSkillDirs_NotPresent(t *testing.T) {
	dir := t.TempDir()
	diags := checkInstalledSkillDirs(nil, dir)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for empty dir, got %d", len(diags))
	}
}

func TestCheckInstalledSkillDirs_PresentNotIgnored(t *testing.T) {
	gitClient := testPublishGitClient(t, nil)
	dir := gitClient.RepoDir

	installedDir := filepath.Join(dir, ".github", "skills", "some-skill")
	if err := os.MkdirAll(installedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	diags := checkInstalledSkillDirs(gitClient, dir)
	if len(diags) == 0 {
		t.Fatal("expected warning for unignored .github/skills/")
	}
	if diags[0].severity != "warning" {
		t.Errorf("expected warning, got %q", diags[0].severity)
	}
	if !strings.Contains(diags[0].message, ".gitignore") {
		t.Errorf("expected .gitignore mention, got: %s", diags[0].message)
	}
}

func TestCheckInstalledSkillDirs_PresentAndIgnored(t *testing.T) {
	gitClient := testPublishGitClient(t, nil)
	dir := gitClient.RepoDir

	installedDir := filepath.Join(dir, ".github", "skills", "some-skill")
	if err := os.MkdirAll(installedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Add .gitignore so git check-ignore recognises the path.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".github/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	runGit("add", ".gitignore")
	runGit("commit", "-m", "init")

	diags := checkInstalledSkillDirs(gitClient, dir)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics when gitignored, got %d: %v", len(diags), diags)
	}
}

func TestGenerateClaudePlugin(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"git-commit", "code-review"} {
		skillDir := filepath.Join(dir, "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: A %s skill\nlicense: MIT\n---\nBody.\n", name, name)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	diags := generateClaudePlugin(dir, []string{"git-commit", "code-review"}, "testowner", "testrepo")

	var generated int
	for _, d := range diags {
		if d.severity == "error" {
			t.Errorf("unexpected error: %s", d.message)
		}
		if d.severity == "info" && strings.Contains(d.message, "generated") {
			generated++
		}
	}
	if generated != 2 {
		t.Errorf("expected 2 generated files, got %d", generated)
	}

	pluginData, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("plugin.json not created: %v", err)
	}
	var plugin claudePluginJSON
	if err := json.Unmarshal(pluginData, &plugin); err != nil {
		t.Fatalf("invalid plugin.json: %v", err)
	}
	if plugin.Name != "testrepo" {
		t.Errorf("plugin.Name = %q, want %q", plugin.Name, "testrepo")
	}
	if plugin.License != "MIT" {
		t.Errorf("plugin.License = %q, want %q", plugin.License, "MIT")
	}
	if plugin.Repository != "https://github.com/testowner/testrepo" {
		t.Errorf("plugin.Repository = %q", plugin.Repository)
	}

	marketData, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatalf("marketplace.json not created: %v", err)
	}
	var marketplace claudeMarketplaceJSON
	if err := json.Unmarshal(marketData, &marketplace); err != nil {
		t.Fatalf("invalid marketplace.json: %v", err)
	}
	if marketplace.Name != "testrepo" {
		t.Errorf("marketplace.Name = %q, want %q", marketplace.Name, "testrepo")
	}
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Source != "." {
		t.Errorf("marketplace.Plugins = %+v", marketplace.Plugins)
	}
}

func TestGenerateClaudePlugin_SkipsExisting(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: test\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"existing"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := generateClaudePlugin(dir, []string{"my-skill"}, "owner", "repo")

	for _, d := range diags {
		if d.severity == "error" {
			t.Errorf("unexpected error: %s", d.message)
		}
		if strings.Contains(d.message, "generated") {
			t.Error("should not regenerate existing plugin.json")
		}
	}
}

func TestDetectGitHubRemote(t *testing.T) {
	gitClient := testPublishGitClient(t, map[string]string{
		"origin": "https://github.com/myorg/myrepo.git",
	})

	owner, repo := detectGitHubRemote(gitClient)
	if owner != "myorg" || repo != "myrepo" {
		t.Errorf("expected myorg/myrepo, got %s/%s", owner, repo)
	}
}

func TestDetectGitHubRemote_Fallback(t *testing.T) {
	gitClient := testPublishGitClient(t, map[string]string{
		"origin":   "https://gitlab.com/foo/bar.git",
		"upstream": "git@github.com:org/repo.git",
	})

	owner, repo := detectGitHubRemote(gitClient)
	if owner != "org" || repo != "repo" {
		t.Errorf("expected org/repo, got %s/%s", owner, repo)
	}
}

func TestDetectGitHubRemote_NoGitHub(t *testing.T) {
	gitClient := testPublishGitClient(t, map[string]string{
		"origin": "https://gitlab.com/foo/bar.git",
	})

	owner, repo := detectGitHubRemote(gitClient)
	if owner != "" || repo != "" {
		t.Errorf("expected empty, got %s/%s", owner, repo)
	}
}

func TestPublishCmd_RunFHook(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := stubFactory(ios)

	var capturedOpts *publishOptions
	cmd := NewCmdPublish(&f, func(opts *publishOptions) error {
		capturedOpts = opts
		return nil
	})

	cmd.SetArgs([]string{"./my-skills", "--dry-run", "--fix", "--tag", "v1.0.0"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts == nil {
		t.Fatal("runF was not called")
	}
	if capturedOpts.Dir != "./my-skills" {
		t.Errorf("Dir = %q, want %q", capturedOpts.Dir, "./my-skills")
	}
	if !capturedOpts.DryRun {
		t.Error("expected DryRun to be true")
	}
	if !capturedOpts.Fix {
		t.Error("expected Fix to be true")
	}
	if capturedOpts.Tag != "v1.0.0" {
		t.Errorf("Tag = %q, want %q", capturedOpts.Tag, "v1.0.0")
	}
}

// stubFactory creates a minimal cmdutil.Factory for tests.
func stubFactory(ios *iostreams.IOStreams) cmdutil.Factory {
	return cmdutil.Factory{
		IOStreams: ios,
	}
}
