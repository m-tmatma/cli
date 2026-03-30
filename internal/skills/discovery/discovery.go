package discovery

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/skills/frontmatter"
)

// specNamePattern matches the strict agentskills.io name spec:
// 1-64 chars, lowercase alphanumeric + hyphens, no leading/trailing/consecutive hyphens.
var specNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// safeNamePattern matches names that are safe for filesystem use during discovery.
// Allows letters (any case), numbers, hyphens, underscores, dots, and spaces.
// Must start with a letter or number. This matches copilot-agent-runtime's SKILL_NAME_REGEX.
var safeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\- ]*$`)

// Skill represents a discovered skill in a repository.
type Skill struct {
	Name        string
	Namespace   string // author/scope prefix for namespaced skills
	Description string
	Path        string // path within the repo, e.g. "skills/git-commit"
	BlobSHA     string // SHA of the SKILL.md blob
	TreeSHA     string // SHA of the skill directory tree
	Convention  string // which directory convention matched
}

// DisplayName returns the skill name, prefixed with namespace if present
// to disambiguate skills from different authors in the same repository.
// Skills discovered via non-standard conventions (plugins, root) include
// a convention tag to distinguish them from identically-named skills in
// the standard skills/ directory.
func (s Skill) DisplayName() string {
	name := s.Name
	if s.Namespace != "" {
		name = s.Namespace + "/" + name
	}
	switch s.Convention {
	case "plugins":
		return "[plugins] " + name
	case "root":
		return "[root] " + name
	default:
		return name
	}
}

// InstallName returns the relative path used for the install directory.
// For namespaced skills it returns "namespace/name" (creating a nested directory),
// otherwise it returns the plain name. Callers should use filepath.FromSlash
// when building OS-specific paths from this value.
func (s Skill) InstallName() string {
	if s.Namespace != "" {
		return s.Namespace + "/" + s.Name
	}
	return s.Name
}

// ResolvedRef contains the resolved git reference and its SHA.
type ResolvedRef struct {
	Ref string // tag name, branch name, or SHA
	SHA string // commit SHA
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

// SkillFile represents a file within a skill directory.
type SkillFile struct {
	Path string // relative path within the skill directory
	SHA  string // blob SHA for fetching content
	Size int    // file size in bytes
}

type treeResponse struct {
	SHA       string      `json:"sha"`
	Tree      []treeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

type blobResponse struct {
	SHA      string `json:"sha"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
}

type repoResponse struct {
	DefaultBranch string `json:"default_branch"`
}

// ResolveRef determines the git ref to use for a given owner/repo.
// Priority: explicit version → latest release tag → default branch.
func ResolveRef(client *api.Client, host, owner, repo, version string) (*ResolvedRef, error) {
	if version != "" {
		return resolveExplicitRef(client, host, owner, repo, version)
	}
	ref, err := resolveLatestRelease(client, host, owner, repo)
	if err == nil {
		return ref, nil
	}
	return resolveDefaultBranch(client, host, owner, repo)
}

// resolveExplicitRef resolves a user-supplied --pin value. It tries, in order:
// tag → commit SHA. Branches are deliberately excluded because they are mutable
// and pinning to one gives a false sense of reproducibility.
func resolveExplicitRef(client *api.Client, host, owner, repo, ref string) (*ResolvedRef, error) {
	tagPath := fmt.Sprintf("repos/%s/%s/git/ref/tags/%s", owner, repo, ref)
	var refResp struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := client.REST(host, "GET", tagPath, nil, &refResp); err == nil {
		sha := refResp.Object.SHA
		if refResp.Object.Type == "tag" {
			derefPath := fmt.Sprintf("repos/%s/%s/git/tags/%s", owner, repo, sha)
			var tagResp struct {
				Object struct {
					SHA string `json:"sha"`
				} `json:"object"`
			}
			if err := client.REST(host, "GET", derefPath, nil, &tagResp); err != nil {
				return nil, fmt.Errorf("could not dereference annotated tag %q: %w", ref, err)
			}
			sha = tagResp.Object.SHA
		}
		return &ResolvedRef{Ref: ref, SHA: sha}, nil
	}

	commitPath := fmt.Sprintf("repos/%s/%s/commits/%s", owner, repo, ref)
	var commitResp struct {
		SHA string `json:"sha"`
	}
	if err := client.REST(host, "GET", commitPath, nil, &commitResp); err == nil {
		return &ResolvedRef{Ref: commitResp.SHA, SHA: commitResp.SHA}, nil
	}

	return nil, fmt.Errorf("ref %q not found as tag or commit in %s/%s", ref, owner, repo)
}

func resolveLatestRelease(client *api.Client, host, owner, repo string) (*ResolvedRef, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/releases/latest", owner, repo)
	var release releaseResponse
	if err := client.REST(host, "GET", apiPath, nil, &release); err != nil {
		return nil, fmt.Errorf("no releases found: %w", err)
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("latest release has no tag")
	}
	return resolveExplicitRef(client, host, owner, repo, release.TagName)
}

func resolveDefaultBranch(client *api.Client, host, owner, repo string) (*ResolvedRef, error) {
	apiPath := fmt.Sprintf("repos/%s/%s", owner, repo)
	var repoResp repoResponse
	if err := client.REST(host, "GET", apiPath, nil, &repoResp); err != nil {
		return nil, fmt.Errorf("could not determine default branch: %w", err)
	}
	branch := repoResp.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	refPath := fmt.Sprintf("repos/%s/%s/git/ref/heads/%s", owner, repo, branch)
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := client.REST(host, "GET", refPath, nil, &refResp); err != nil {
		return nil, fmt.Errorf("could not resolve branch %q: %w", branch, err)
	}

	return &ResolvedRef{Ref: branch, SHA: refResp.Object.SHA}, nil
}

// skillMatch represents a matched SKILL.md file and its convention.
type skillMatch struct {
	entry      treeEntry
	name       string
	namespace  string
	skillDir   string
	convention string
}

// MatchesSkillPath checks if a file path matches any known skill convention
// and returns the skill name. Returns empty string if the path doesn't match.
func MatchesSkillPath(filePath string) string {
	m := matchSkillConventions(treeEntry{Path: filePath})
	if m == nil {
		return ""
	}
	return m.name
}

// matchSkillConventions checks if a blob path matches any known skill convention.
func matchSkillConventions(entry treeEntry) *skillMatch {
	if path.Base(entry.Path) != "SKILL.md" {
		return nil
	}

	dir := path.Dir(entry.Path)
	parentDir := path.Dir(dir)
	skillName := path.Base(dir)

	if !validateName(skillName) {
		return nil
	}

	if parentDir == "skills" {
		return &skillMatch{entry: entry, name: skillName, skillDir: dir, convention: "skills"}
	}

	grandparentDir := path.Dir(parentDir)
	if grandparentDir == "skills" {
		namespace := path.Base(parentDir)
		if !validateName(namespace) {
			return nil
		}
		return &skillMatch{entry: entry, name: skillName, namespace: namespace, skillDir: dir, convention: "skills-namespaced"}
	}

	if path.Base(parentDir) == "skills" && path.Dir(grandparentDir) == "plugins" {
		namespace := path.Base(grandparentDir)
		if !validateName(namespace) {
			return nil
		}
		return &skillMatch{entry: entry, name: skillName, namespace: namespace, skillDir: dir, convention: "plugins"}
	}

	if parentDir == "." && skillName != "skills" && skillName != "plugins" && !strings.HasPrefix(skillName, ".") {
		return &skillMatch{entry: entry, name: skillName, skillDir: dir, convention: "root"}
	}

	return nil
}

// DiscoverSkills finds all skills in a repository at the given commit SHA.
func DiscoverSkills(client *api.Client, host, owner, repo, commitSHA string) ([]Skill, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/git/trees/%s?recursive=true", owner, repo, commitSHA)
	var tree treeResponse
	if err := client.REST(host, "GET", apiPath, nil, &tree); err != nil {
		return nil, fmt.Errorf("could not fetch repository tree: %w", err)
	}

	if tree.Truncated {
		return nil, fmt.Errorf(
			"repository tree for %s/%s is too large for full discovery\n"+
				"  Use path-based install instead: gh skills install %s/%s skills/<skill-name>",
			owner, repo, owner, repo,
		)
	}

	treeSHAs := make(map[string]string)
	for _, entry := range tree.Tree {
		if entry.Type == "tree" {
			treeSHAs[entry.Path] = entry.SHA
		}
	}

	seen := make(map[string]bool)
	var matches []skillMatch
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		m := matchSkillConventions(entry)
		if m == nil {
			continue
		}
		if seen[m.skillDir] {
			continue
		}
		seen[m.skillDir] = true
		matches = append(matches, *m)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf(
			"no skills found in %s/%s\n"+
				"  Expected skills in skills/*/SKILL.md, skills/{scope}/*/SKILL.md,\n"+
				"  */SKILL.md, or plugins/*/skills/*/SKILL.md\n"+
				"  This repository may be a curated list rather than a skills publisher",
			owner, repo,
		)
	}

	var skills []Skill
	for _, m := range matches {
		skills = append(skills, Skill{
			Name:       m.name,
			Namespace:  m.namespace,
			Path:       m.skillDir,
			BlobSHA:    m.entry.SHA,
			TreeSHA:    treeSHAs[m.skillDir],
			Convention: m.convention,
		})
	}

	return skills, nil
}

// fetchDescription fetches and parses the frontmatter description for a skill.
func fetchDescription(client *api.Client, host, owner, repo string, skill *Skill) string {
	if skill.BlobSHA == "" {
		return ""
	}
	content, err := FetchBlob(client, host, owner, repo, skill.BlobSHA)
	if err != nil {
		return ""
	}
	result, err := frontmatter.Parse(content)
	if err != nil {
		return ""
	}
	return result.Metadata.Description
}

// FetchDescriptionsConcurrent fetches descriptions with bounded concurrency.
func FetchDescriptionsConcurrent(client *api.Client, host, owner, repo string, skills []Skill, onProgress func(done, total int)) {
	total := 0
	for _, s := range skills {
		if s.Description == "" {
			total++
		}
	}
	if total == 0 {
		return
	}

	const maxWorkers = 10
	sem := make(chan struct{}, maxWorkers)
	var mu sync.Mutex
	done := 0

	var wg sync.WaitGroup
	for i := range skills {
		if skills[i].Description != "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			desc := fetchDescription(client, host, owner, repo, &skills[idx])

			mu.Lock()
			skills[idx].Description = desc
			done++
			d := done
			mu.Unlock()
			if onProgress != nil {
				onProgress(d, total)
			}
		}(i)
	}
	wg.Wait()
}

// DiscoverSkillByPath looks up a single skill by its exact path in the repository.
func DiscoverSkillByPath(client *api.Client, host, owner, repo, commitSHA, skillPath string) (*Skill, error) {
	skillPath = strings.TrimSuffix(skillPath, "/SKILL.md")
	skillPath = strings.TrimSuffix(skillPath, "/")

	skillName := path.Base(skillPath)
	if !validateName(skillName) {
		return nil, fmt.Errorf("invalid skill name %q", skillName)
	}

	parentPath := path.Dir(skillPath)
	apiPath := fmt.Sprintf("repos/%s/%s/contents/%s?ref=%s", owner, repo, parentPath, commitSHA)

	var contents []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	}
	if err := client.REST(host, "GET", apiPath, nil, &contents); err != nil {
		return nil, fmt.Errorf("path %q not found in %s/%s: %w", parentPath, owner, repo, err)
	}

	var treeSHA string
	for _, entry := range contents {
		if entry.Name == skillName && entry.Type == "dir" {
			treeSHA = entry.SHA
			break
		}
	}
	if treeSHA == "" {
		return nil, fmt.Errorf("skill directory %q not found in %s/%s", skillPath, owner, repo)
	}

	skillTreePath := fmt.Sprintf("repos/%s/%s/git/trees/%s", owner, repo, treeSHA)
	var skillTree treeResponse
	if err := client.REST(host, "GET", skillTreePath, nil, &skillTree); err != nil {
		return nil, fmt.Errorf("could not read skill directory: %w", err)
	}

	var blobSHA string
	for _, entry := range skillTree.Tree {
		if entry.Path == "SKILL.md" && entry.Type == "blob" {
			blobSHA = entry.SHA
			break
		}
	}
	if blobSHA == "" {
		return nil, fmt.Errorf("no SKILL.md found in %s", skillPath)
	}

	var namespace string
	parts := strings.Split(skillPath, "/")
	if len(parts) >= 3 && parts[0] == "skills" {
		namespace = parts[1]
	}

	skill := &Skill{
		Name:      skillName,
		Namespace: namespace,
		Path:      skillPath,
		BlobSHA:   blobSHA,
		TreeSHA:   treeSHA,
	}

	skill.Description = fetchDescription(client, host, owner, repo, skill)

	return skill, nil
}

// DiscoverSkillFiles returns all file paths belonging to a skill directory
// by fetching the skill's subtree directly using its tree SHA.
func DiscoverSkillFiles(client *api.Client, host, owner, repo, treeSHA, skillPath string) ([]SkillFile, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/git/trees/%s?recursive=true", owner, repo, treeSHA)
	var tree treeResponse
	if err := client.REST(host, "GET", apiPath, nil, &tree); err != nil {
		return nil, fmt.Errorf("could not fetch skill tree: %w", err)
	}

	if tree.Truncated {
		// Recursive fetch was truncated — fall back to walking subtrees individually.
		return walkTree(client, host, owner, repo, treeSHA, skillPath)
	}

	var files []SkillFile
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			files = append(files, SkillFile{
				Path: skillPath + "/" + entry.Path,
				SHA:  entry.SHA,
				Size: entry.Size,
			})
		}
	}

	return files, nil
}

// ListSkillFiles returns all files in a skill directory as public SkillFile
// structs with paths relative to the skill root.
func ListSkillFiles(client *api.Client, host, owner, repo, treeSHA string) ([]SkillFile, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/git/trees/%s?recursive=true", owner, repo, treeSHA)
	var tree treeResponse
	if err := client.REST(host, "GET", apiPath, nil, &tree); err != nil {
		return nil, fmt.Errorf("could not fetch skill tree: %w", err)
	}

	if tree.Truncated {
		// Fall back to non-recursive traversal when the tree is too large.
		return walkTree(client, host, owner, repo, treeSHA, "")
	}

	var files []SkillFile
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			files = append(files, SkillFile{
				Path: entry.Path,
				SHA:  entry.SHA,
				Size: entry.Size,
			})
		}
	}
	return files, nil
}

// walkTree enumerates files by fetching each tree level individually,
// avoiding the truncation limit of the recursive tree API.
func walkTree(client *api.Client, host, owner, repo, sha, prefix string) ([]SkillFile, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/git/trees/%s", owner, repo, sha)
	var tree treeResponse
	if err := client.REST(host, "GET", apiPath, nil, &tree); err != nil {
		return nil, fmt.Errorf("could not fetch tree %s: %w", prefix, err)
	}

	var files []SkillFile
	for _, entry := range tree.Tree {
		entryPath := entry.Path
		if prefix != "" {
			entryPath = prefix + "/" + entry.Path
		}
		switch entry.Type {
		case "blob":
			files = append(files, SkillFile{Path: entryPath, SHA: entry.SHA, Size: entry.Size})
		case "tree":
			sub, err := walkTree(client, host, owner, repo, entry.SHA, entryPath)
			if err != nil {
				return nil, err
			}
			files = append(files, sub...)
		}
	}
	return files, nil
}

// FetchBlob retrieves the content of a blob by SHA.
func FetchBlob(client *api.Client, host, owner, repo, sha string) (string, error) {
	apiPath := fmt.Sprintf("repos/%s/%s/git/blobs/%s", owner, repo, sha)
	var blob blobResponse
	if err := client.REST(host, "GET", apiPath, nil, &blob); err != nil {
		return "", fmt.Errorf("could not fetch blob: %w", err)
	}

	if blob.Encoding != "base64" {
		return "", fmt.Errorf("unexpected blob encoding: %s", blob.Encoding)
	}

	// GitHub API returns base64 with embedded newlines; use the StdEncoding
	// decoder via a reader to handle them transparently.
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(blob.Content)))
	if err != nil {
		return "", fmt.Errorf("could not decode blob content: %w", err)
	}

	return string(decoded), nil
}

// DiscoverLocalSkills finds skills in a local directory using the same
// conventions as remote discovery.
func DiscoverLocalSkills(dir string) ([]Skill, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("could not resolve path: %w", err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("could not access %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	if _, err := os.Stat(filepath.Join(absDir, "SKILL.md")); err == nil {
		skill, err := localSkillFromDir(absDir)
		if err != nil {
			return nil, err
		}
		skill.Path = "."
		return []Skill{*skill}, nil
	}

	var skills []Skill
	seen := make(map[string]bool)

	err = filepath.Walk(absDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip symlinks to avoid following links outside the source tree.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() || info.Name() != "SKILL.md" {
			return nil
		}

		relPath, relErr := filepath.Rel(absDir, p)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)

		entry := treeEntry{Path: relPath, Type: "blob"}
		m := matchSkillConventions(entry)
		if m == nil {
			return nil
		}
		if seen[m.skillDir] {
			return nil
		}
		seen[m.skillDir] = true

		skill, skillErr := localSkillFromDir(filepath.Join(absDir, filepath.FromSlash(m.skillDir)))
		if skillErr != nil {
			return nil //nolint:nilerr // intentionally skip files that aren't valid skills
		}
		skill.Path = m.skillDir
		skill.Namespace = m.namespace
		skill.Convention = m.convention
		skills = append(skills, *skill)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not walk directory: %w", err)
	}

	if len(skills) == 0 {
		return nil, fmt.Errorf(
			"no skills found in %s\n"+
				"  Expected SKILL.md in the directory, or skills in skills/*/SKILL.md,\n"+
				"  skills/{scope}/*/SKILL.md, */SKILL.md, or plugins/*/skills/*/SKILL.md",
			dir,
		)
	}

	return skills, nil
}

func localSkillFromDir(dir string) (*Skill, error) {
	skillFile := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", skillFile, err)
	}

	name := filepath.Base(dir)
	var description string

	result, parseErr := frontmatter.Parse(string(data))
	if parseErr == nil {
		if result.Metadata.Name != "" {
			name = result.Metadata.Name
		}
		description = result.Metadata.Description
	}

	if !validateName(name) {
		return nil, fmt.Errorf("invalid skill name %q in %s", name, dir)
	}

	return &Skill{
		Name:        name,
		Description: description,
		Path:        filepath.Base(dir),
	}, nil
}

// validateName checks if a skill name is safe for use (filesystem-safe).
func validateName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return false
	}
	return safeNamePattern.MatchString(name)
}

// IsSpecCompliant checks if a skill name matches the strict agentskills.io spec.
func IsSpecCompliant(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	if strings.Contains(name, "--") {
		return false
	}
	return specNamePattern.MatchString(name)
}
