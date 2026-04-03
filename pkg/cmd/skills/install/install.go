package install

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	ghContext "github.com/cli/cli/v2/context"
	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/internal/skills/discovery"
	"github.com/cli/cli/v2/internal/skills/frontmatter"
	"github.com/cli/cli/v2/internal/skills/installer"
	"github.com/cli/cli/v2/internal/skills/registry"
	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

const (
	// allSkillsKey is the persistent option label for selecting all skills.
	allSkillsKey = "(all skills)"

	// maxSearchResults caps how many skills are shown per search page in
	// interactive selection, keeping the prompt readable.
	maxSearchResults = 30
)

// installOptions holds all dependencies and user-provided flags for the install command.
type installOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	Prompter   prompter.Prompter
	GitClient  *git.Client
	Remotes    func() (ghContext.Remotes, error)

	// Arguments
	SkillSource string // owner/repo or local path
	SkillName   string // skill name, possibly with @version

	// Flags
	Agent        string // --agent flag
	Scope        string // --scope flag
	ScopeChanged bool   // true when --scope was explicitly set
	Pin          string // --pin flag
	Dir          string // --dir flag (overrides host+scope)
	All          bool   // --all flag
	Force        bool   // --force flag

	// Resolved at runtime
	repo      ghrepo.Interface // set when SkillSource is a GitHub repository
	localPath string           // set when SkillSource is a local directory
	version   string
}

// NewCmdInstall creates the "skills install" command.
func NewCmdInstall(f *cmdutil.Factory, runF func(*installOptions) error) *cobra.Command {
	opts := &installOptions{
		IO:         f.IOStreams,
		Prompter:   f.Prompter,
		GitClient:  f.GitClient,
		Remotes:    f.Remotes,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "install <repository> [<skill[@version]>]",
		Short: "Install agent skills from a GitHub repository",
		Long: heredoc.Docf(`
			Install agent skills from a GitHub repository or local directory into
			your local environment. Skills are placed in a host-specific directory
			at either project scope (inside the current git repository) or user
			scope (in your home directory, available everywhere):

			  Host             Project                 User
			  GitHub Copilot   .github/skills          ~/.copilot/skills
			  Claude Code      .claude/skills          ~/.claude/skills
			  Cursor           .cursor/skills          ~/.cursor/skills
			  Codex            .agents/skills          ~/.codex/skills
			  Gemini CLI       .agent/skills           ~/.gemini/skills
			  Antigravity      .agent/skills           ~/.gemini/antigravity/skills

			Use %[1]s--agent%[1]s and %[1]s--scope%[1]s to control placement, or %[1]s--dir%[1]s for a
			custom directory. The default scope is %[1]sproject%[1]s, and the default
			agent is %[1]sgithub-copilot%[1]s (when running non-interactively).

			The first argument can be a GitHub repository in %[1]sOWNER/REPO%[1]s format
			or a local directory path (e.g. %[1]s.%[1]s, %[1]s./my-skills%[1]s, %[1]s~/skills%[1]s).
			For local directories, skills are auto-discovered using the same
			conventions as remote repositories, and files are copied (not symlinked)
			with local-path tracking metadata injected into frontmatter.

			Skills are discovered automatically using the %[1]sskills/*/SKILL.md%[1]s convention
			defined by the Agent Skills specification. For more information on the specification, 
			see: https://agentskills.io/specification

			The skill argument can be a name, a namespaced name (%[1]sauthor/skill%[1]s),
			or an exact path within the repository (%[1]sskills/author/skill%[1]s or
			%[1]sskills/author/skill/SKILL.md%[1]s).

			Performance tip: when installing from a large repository with many
			skills, providing an exact path instead of a skill name avoids a
			full tree traversal of the repository, making the install significantly faster.

			When a skill name is provided without a version, the CLI resolves the
			version in this order:

			  1. Latest tagged release in the repository
			  2. Default branch HEAD

			To pin to a specific version, either append %[1]s@VERSION%[1]s to the skill
			name or use the %[1]s--pin%[1]s flag. The version is resolved as a git tag or commit SHA.

			Installed skills have GitHub tracking metadata injected into their
			frontmatter (%[1]sgithub-owner%[1]s, %[1]sgithub-repo%[1]s, %[1]sgithub-ref%[1]s,
			%[1]sgithub-sha%[1]s, %[1]sgithub-tree-sha%[1]s, %[1]sgithub-path%[1]s). This
			metadata identifies the source repository and enables %[1]sgh skills update%[1]s
			to detect changes — the tree SHA serves as an ETag for staleness checks.

			When run interactively, the command prompts for any missing arguments.
			When run non-interactively, %[1]srepository%[1]s is required, and either a
			skill name or %[1]s--all%[1]s must be specified.
		`, "`"),
		Example: heredoc.Doc(`
			# Interactive: choose repo, skill, and agent
			$ gh skills install

			# Choose a skill from the repo interactively
			$ gh skills install github/awesome-copilot

			# Install a specific skill
			$ gh skills install github/awesome-copilot git-commit

			# Install a specific version
			$ gh skills install github/awesome-copilot git-commit@v1.2.0

			# Install all skills from a repo
			$ gh skills install github/awesome-copilot --all

			# Install from a large namespaced repo by path (efficient, skips full discovery)
			$ gh skills install github/awesome-copilot skills/monalisa/code-review

			# Install from a local directory (auto-discovers skills)
			$ gh skills install ./my-skills-repo

			# Install from current directory
			$ gh skills install .

			# Install a single local skill directory
			$ gh skills install ./skills/git-commit

			# Install for Claude Code at user scope
			$ gh skills install github/awesome-copilot git-commit --agent claude-code --scope user

			# Pin to a specific git ref
			$ gh skills install github/awesome-copilot git-commit --pin v2.0.0
		`),
		Aliases: []string{"add"},
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !opts.IO.CanPrompt() {
				return cmdutil.FlagErrorf("must specify a repository to install from")
			}
			if len(args) >= 1 {
				opts.SkillSource = args[0]
			}
			if len(args) >= 2 {
				opts.SkillName = args[1]
			}
			opts.ScopeChanged = cmd.Flags().Changed("scope")

			// Resolve the source type early so installRun can branch directly.
			if isLocalPath(opts.SkillSource) {
				opts.localPath = opts.SkillSource
			}

			if opts.Agent != "" {
				if _, err := registry.FindByID(opts.Agent); err != nil {
					return cmdutil.FlagErrorf("invalid value for --agent: %s", err)
				}
			}

			if opts.Pin != "" && opts.SkillName != "" && strings.Contains(opts.SkillName, "@") {
				return cmdutil.FlagErrorf("cannot use --pin with an inline @version in the skill name")
			}

			if runF != nil {
				return runF(opts)
			}
			return installRun(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Agent, "agent", "", fmt.Sprintf("target agent (%s)", registry.ValidAgentIDs()))
	_ = cmd.RegisterFlagCompletionFunc("agent", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return registry.AgentIDs(), cobra.ShellCompDirectiveNoFileComp
	})
	cmdutil.StringEnumFlag(cmd, &opts.Scope, "scope", "", "project", []string{"project", "user"}, "Installation scope")
	cmd.Flags().StringVar(&opts.Pin, "pin", "", "pin to a specific git tag or commit SHA")
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "install to a custom directory (overrides --agent and --scope)")
	cmd.Flags().BoolVar(&opts.All, "all", false, "install all skills from the repository")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "overwrite existing skills without prompting")

	return cmd
}

func installRun(opts *installOptions) error {
	cs := opts.IO.ColorScheme()
	canPrompt := opts.IO.CanPrompt()

	if opts.localPath != "" {
		return runLocalInstall(opts)
	}

	repo, source, err := resolveRepoArg(opts.SkillSource, canPrompt, opts.Prompter)
	if err != nil {
		return err
	}
	opts.repo = repo
	opts.SkillSource = source

	parseSkillFromOpts(opts)

	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	apiClient := api.NewClientFromHTTP(httpClient)

	hostname := opts.repo.RepoHost()

	resolved, err := resolveVersion(opts, apiClient, hostname)
	if err != nil {
		return err
	}

	var selectedSkills []discovery.Skill

	if isSkillPath(opts.SkillName) {
		opts.IO.StartProgressIndicatorWithLabel("Looking up skill")
		skill, err := discovery.DiscoverSkillByPath(apiClient, hostname, opts.repo.RepoOwner(), opts.repo.RepoName(), resolved.SHA, opts.SkillName)
		opts.IO.StopProgressIndicator()
		if err != nil {
			return err
		}
		selectedSkills = []discovery.Skill{*skill}
	} else {
		skills, err := discoverSkills(opts, apiClient, hostname, resolved)
		if err != nil {
			return err
		}

		selectedSkills, err = selectSkillsWithSelector(opts, skills, canPrompt, skillSelector{
			matchByName: matchSkillByName,
			sourceHint:  ghrepo.FullName(opts.repo),
			fetchDescriptions: func() {
				opts.IO.StartProgressIndicatorWithLabel("Fetching skill info")
				discovery.FetchDescriptionsConcurrent(apiClient, hostname, opts.repo.RepoOwner(), opts.repo.RepoName(), skills, nil)
				opts.IO.StopProgressIndicator()
			},
		})
		if err != nil {
			return err
		}
	}

	selectedHosts, err := resolveHosts(opts, canPrompt)
	if err != nil {
		return err
	}

	scope, err := resolveScope(opts, canPrompt)
	if err != nil {
		return err
	}

	gitRoot := installer.ResolveGitRoot(opts.GitClient)
	homeDir := installer.ResolveHomeDir()
	source = ghrepo.FullName(opts.repo)

	type hostPlan struct {
		host   *registry.AgentHost
		skills []discovery.Skill
	}
	var plans []hostPlan
	for _, host := range selectedHosts {
		installSkills, err := checkOverwrite(opts, selectedSkills, host, scope, gitRoot, homeDir, canPrompt)
		if err != nil {
			return err
		}
		if len(installSkills) == 0 {
			fmt.Fprintf(opts.IO.ErrOut, "No skills to install for %s.\n", host.Name)
			continue
		}
		plans = append(plans, hostPlan{host: host, skills: installSkills})
	}

	for _, plan := range plans {
		if len(plans) > 1 {
			fmt.Fprintf(opts.IO.ErrOut, "\nInstalling to %s...\n", plan.host.Name)
		}

		result, err := installer.Install(&installer.Options{
			Host:       hostname,
			Owner:      opts.repo.RepoOwner(),
			Repo:       opts.repo.RepoName(),
			Ref:        resolved.Ref,
			SHA:        resolved.SHA,
			PinnedRef:  opts.Pin,
			Skills:     plan.skills,
			AgentHost:  plan.host,
			Scope:      scope,
			Dir:        opts.Dir,
			GitRoot:    gitRoot,
			HomeDir:    homeDir,
			Client:     apiClient,
			OnProgress: installProgress(opts.IO, len(plan.skills)),
		})

		if result != nil {
			for _, w := range result.Warnings {
				fmt.Fprintf(opts.IO.ErrOut, "%s %s\n", cs.WarningIcon(), w)
			}

			for _, name := range result.Installed {
				fmt.Fprintf(opts.IO.Out, "%s Installed %s (from %s@%s) in %s\n",
					cs.SuccessIcon(), name, source, resolved.Ref, friendlyDir(result.Dir))
			}

			printFileTree(opts.IO.Out, cs, result.Dir, result.Installed)
			printReviewHint(opts.IO.ErrOut, cs, source, result.Installed)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// isLocalPath returns true if the argument looks like a local filesystem path
// rather than a GitHub owner/repo reference.
func isLocalPath(arg string) bool {
	if arg == "" {
		return false
	}
	sep := string(filepath.Separator)
	if arg == "." || arg == ".." ||
		strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, "."+sep) || strings.HasPrefix(arg, ".."+sep) {
		return true
	}
	// filepath.IsAbs on Windows requires a drive letter, so "/tmp/foo"
	// would not be recognized. Check explicitly for a leading "/" so that
	// Unix-style absolute paths are never mistaken for owner/repo refs.
	if filepath.IsAbs(arg) || arg[0] == '/' || strings.HasPrefix(arg, "~") {
		return true
	}
	info, err := os.Stat(arg)
	if err == nil && info.IsDir() {
		return true
	}
	return false
}

// runLocalInstall handles installation from a local directory path.
func runLocalInstall(opts *installOptions) error {
	cs := opts.IO.ColorScheme()
	canPrompt := opts.IO.CanPrompt()
	sourcePath := opts.localPath
	if sourcePath == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			sourcePath = home
		}
	} else if after, ok := strings.CutPrefix(sourcePath, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			sourcePath = filepath.Join(home, after)
		}
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("could not resolve path: %w", err)
	}

	opts.IO.StartProgressIndicatorWithLabel("Discovering skills")
	skills, err := discovery.DiscoverLocalSkills(absSource)
	opts.IO.StopProgressIndicator()
	if err != nil {
		return err
	}

	if canPrompt {
		fmt.Fprintf(opts.IO.ErrOut, "Found %d skill(s)\n", len(skills))
	}

	selectedSkills, err := selectSkillsWithSelector(opts, skills, canPrompt, skillSelector{
		matchByName: matchLocalSkillByName,
		sourceHint:  absSource,
	})
	if err != nil {
		return err
	}

	selectedHosts, err := resolveHosts(opts, canPrompt)
	if err != nil {
		return err
	}

	scope, err := resolveScope(opts, canPrompt)
	if err != nil {
		return err
	}

	gitRoot := installer.ResolveGitRoot(opts.GitClient)
	homeDir := installer.ResolveHomeDir()

	type hostPlan struct {
		host   *registry.AgentHost
		skills []discovery.Skill
	}
	var plans []hostPlan
	for _, host := range selectedHosts {
		installSkills, err := checkOverwrite(opts, selectedSkills, host, scope, gitRoot, homeDir, canPrompt)
		if err != nil {
			return err
		}
		if len(installSkills) == 0 {
			fmt.Fprintf(opts.IO.ErrOut, "No skills to install for %s.\n", host.Name)
			continue
		}
		plans = append(plans, hostPlan{host: host, skills: installSkills})
	}

	for _, plan := range plans {
		if len(plans) > 1 {
			fmt.Fprintf(opts.IO.ErrOut, "\nInstalling to %s...\n", plan.host.Name)
		}

		result, err := installer.InstallLocal(&installer.LocalOptions{
			SourceDir: absSource,
			Skills:    plan.skills,
			AgentHost: plan.host,
			Scope:     scope,
			Dir:       opts.Dir,
			GitRoot:   gitRoot,
			HomeDir:   homeDir,
		})
		if err != nil {
			return err
		}

		for _, name := range result.Installed {
			fmt.Fprintf(opts.IO.Out, "Installed %s (from %s) in %s\n",
				name, opts.SkillSource, friendlyDir(result.Dir))
		}

		printFileTree(opts.IO.Out, cs, result.Dir, result.Installed)
		printReviewHint(opts.IO.ErrOut, cs, "", result.Installed)
	}

	return nil
}

// isSkillPath returns true if the argument looks like a repo-relative path
// rather than a simple skill name.
func isSkillPath(name string) bool {
	if name == "" {
		return false
	}
	if name == "SKILL.md" || strings.HasSuffix(name, "/SKILL.md") {
		return true
	}
	if strings.HasPrefix(name, "skills/") || strings.HasPrefix(name, "plugins/") {
		return true
	}
	return false
}

func resolveRepoArg(skillSource string, canPrompt bool, p prompter.Prompter) (ghrepo.Interface, string, error) {
	if skillSource == "" {
		if !canPrompt {
			return nil, "", cmdutil.FlagErrorf("must specify a repository to install from")
		}
		repoInput, err := p.Input("Repository (owner/repo):", "")
		if err != nil {
			return nil, "", err
		}
		skillSource = strings.TrimSpace(repoInput)
		if skillSource == "" {
			return nil, "", fmt.Errorf("must specify a repository to install from")
		}
	}
	repo, err := ghrepo.FromFullName(skillSource)
	if err != nil {
		return nil, "", cmdutil.FlagErrorf("invalid repository reference %q: expected OWNER/REPO, HOST/OWNER/REPO, or a full URL", skillSource)
	}
	return repo, skillSource, nil
}

func parseSkillFromOpts(opts *installOptions) {
	if opts.SkillName != "" {
		if name, version, ok := cutLast(opts.SkillName, "@"); ok && name != "" {
			opts.version = version
			opts.SkillName = name
			return
		}
	}
	if opts.Pin != "" {
		opts.version = opts.Pin
	}
}

// cutLast splits s around the last occurrence of sep,
// returning the text before and after sep, and whether sep was found.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

func resolveVersion(opts *installOptions, client *api.Client, hostname string) (*discovery.ResolvedRef, error) {
	opts.IO.StartProgressIndicatorWithLabel("Resolving version")
	resolved, err := discovery.ResolveRef(client, hostname, opts.repo.RepoOwner(), opts.repo.RepoName(), opts.version)
	opts.IO.StopProgressIndicator()
	if err != nil {
		return nil, fmt.Errorf("could not resolve version: %w", err)
	}
	fmt.Fprintf(opts.IO.ErrOut, "Using ref %s (%s)\n", resolved.Ref, git.ShortSHA(resolved.SHA))
	return resolved, nil
}

func discoverSkills(opts *installOptions, client *api.Client, hostname string, resolved *discovery.ResolvedRef) ([]discovery.Skill, error) {
	opts.IO.StartProgressIndicatorWithLabel("Discovering skills")
	skills, err := discovery.DiscoverSkills(client, hostname, opts.repo.RepoOwner(), opts.repo.RepoName(), resolved.SHA)
	opts.IO.StopProgressIndicator()
	if err != nil {
		return nil, err
	}
	logConventions(opts.IO, skills)
	for _, s := range skills {
		if !discovery.IsSpecCompliant(s.Name) {
			fmt.Fprintf(opts.IO.ErrOut, "Warning: skill %q does not follow the agentskills.io naming convention\n", s.DisplayName())
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].DisplayName() < skills[j].DisplayName()
	})
	return skills, nil
}

func logConventions(io *iostreams.IOStreams, skills []discovery.Skill) {
	conventions := make(map[string]int)
	for _, s := range skills {
		conventions[s.Convention]++
	}
	if n, ok := conventions["skills-namespaced"]; ok {
		fmt.Fprintf(io.ErrOut, "Note: found %d namespaced skill(s) in skills/{author}/ directories\n", n)
	}
	if n, ok := conventions["plugins"]; ok {
		fmt.Fprintf(io.ErrOut, "Note: found %d skill(s) using the plugins/ convention\n", n)
	}
	if n, ok := conventions["root"]; ok {
		fmt.Fprintf(io.ErrOut, "Note: found %d skill(s) at the repository root\n", n)
	}
}

// skillSelector holds the callbacks that differ between remote and local skill selection.
type skillSelector struct {
	// matchByName resolves a skill name to matching skills.
	matchByName func(opts *installOptions, skills []discovery.Skill) ([]discovery.Skill, error)
	// sourceHint is shown in collision error guidance (e.g. "owner/repo" or "/path/to/skills").
	sourceHint string
	// fetchDescriptions, if non-nil, is called before prompting to pre-populate descriptions.
	fetchDescriptions func()
}

func selectSkillsWithSelector(opts *installOptions, skills []discovery.Skill, canPrompt bool, sel skillSelector) ([]discovery.Skill, error) {
	checkCollisions := func(ss []discovery.Skill) error {
		return collisionError(ss, sel.sourceHint)
	}

	if opts.All {
		if err := checkCollisions(skills); err != nil {
			return nil, err
		}
		return skills, nil
	}

	if opts.SkillName != "" {
		return sel.matchByName(opts, skills)
	}

	if !canPrompt {
		return nil, cmdutil.FlagErrorf("must specify a skill name or use --all when not running interactively")
	}

	if sel.fetchDescriptions != nil {
		sel.fetchDescriptions()
	}

	tw := opts.IO.TerminalWidth()
	descWidth := tw - 35
	if descWidth < 20 {
		descWidth = 20
	}

	selected, err := opts.Prompter.MultiSelectWithSearch(
		"Select skill(s) to install:",
		"Filter skills",
		nil,
		[]string{allSkillsKey},
		skillSearchFunc(skills, descWidth),
	)
	if err != nil {
		return nil, err
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("must select at least one skill")
	}

	for _, s := range selected {
		if s == allSkillsKey {
			if err := checkCollisions(skills); err != nil {
				return nil, err
			}
			return skills, nil
		}
	}

	result, err := matchSelectedSkills(skills, selected)
	if err != nil {
		return nil, err
	}
	return result, checkCollisions(result)
}

func matchSkillByName(opts *installOptions, skills []discovery.Skill) ([]discovery.Skill, error) {
	for _, s := range skills {
		if s.DisplayName() == opts.SkillName {
			return []discovery.Skill{s}, nil
		}
	}

	var matches []discovery.Skill
	for _, s := range skills {
		if s.Name == opts.SkillName {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("skill %q not found in %s", opts.SkillName, ghrepo.FullName(opts.repo))
	case 1:
		return matches, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.DisplayName()
		}
		return nil, fmt.Errorf(
			"skill name %q is ambiguous — multiple matches found:\n  %s\n  Specify the full name (e.g. %s) to disambiguate",
			opts.SkillName, strings.Join(names, "\n  "), names[0],
		)
	}
}

func matchLocalSkillByName(opts *installOptions, skills []discovery.Skill) ([]discovery.Skill, error) {
	for _, s := range skills {
		if s.DisplayName() == opts.SkillName || s.Name == opts.SkillName {
			return []discovery.Skill{s}, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found in local directory", opts.SkillName)
}

// skillSearchFunc returns a search function for MultiSelectWithSearch that
// filters skills by case-insensitive substring match on name and description.
func skillSearchFunc(skills []discovery.Skill, descWidth int) func(string) prompter.MultiSelectSearchResult {
	return func(query string) prompter.MultiSelectSearchResult {
		var matched []discovery.Skill
		if query == "" {
			matched = skills
		} else {
			q := strings.ToLower(query)
			for _, s := range skills {
				if strings.Contains(strings.ToLower(s.DisplayName()), q) ||
					strings.Contains(strings.ToLower(s.Description), q) {
					matched = append(matched, s)
				}
			}
		}

		more := 0
		if len(matched) > maxSearchResults {
			more = len(matched) - maxSearchResults
			matched = matched[:maxSearchResults]
		}

		keys := make([]string, len(matched))
		labels := make([]string, len(matched))
		for i, s := range matched {
			keys[i] = s.DisplayName()
			if s.Description != "" {
				labels[i] = fmt.Sprintf("%s — %s", s.DisplayName(), truncateDescription(s.Description, descWidth))
			} else {
				labels[i] = s.DisplayName()
			}
		}

		return prompter.MultiSelectSearchResult{
			Keys:        keys,
			Labels:      labels,
			MoreResults: more,
		}
	}
}

// matchSelectedSkills maps display names back to skill structs.
func matchSelectedSkills(skills []discovery.Skill, selected []string) ([]discovery.Skill, error) {
	nameSet := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		nameSet[name] = struct{}{}
	}

	var result []discovery.Skill
	for _, s := range skills {
		if _, ok := nameSet[s.DisplayName()]; ok {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no matching skills found")
	}
	return result, nil
}

// collisionError checks for name collisions and returns an error with
// guidance on how to install skills individually.
func collisionError(ss []discovery.Skill, sourceHint string) error {
	collisions := discovery.FindNameCollisions(ss)
	if len(collisions) == 0 {
		return nil
	}
	return errors.New(heredoc.Docf(`
		cannot install skills with conflicting names — they would overwrite each other:
		  %s
		Install these skills individually using the full name:
		  gh skills install %s namespace/skill-name
	`, discovery.FormatCollisions(collisions), sourceHint))
}

func resolveHosts(opts *installOptions, canPrompt bool) ([]*registry.AgentHost, error) {
	if opts.Agent != "" {
		h, err := registry.FindByID(opts.Agent)
		if err != nil {
			return nil, err
		}
		return []*registry.AgentHost{h}, nil
	}

	if !canPrompt {
		h, err := registry.FindByID("github-copilot")
		if err != nil {
			return nil, err
		}
		return []*registry.AgentHost{h}, nil
	}

	fmt.Fprintln(opts.IO.ErrOut)
	names := registry.AgentNames()
	indices, err := opts.Prompter.MultiSelect("Select target agent(s):", []string{names[0]}, names)
	if err != nil {
		return nil, err
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("must select at least one target agent")
	}

	selected := make([]*registry.AgentHost, len(indices))
	for i, idx := range indices {
		selected[i] = &registry.Agents[idx]
	}
	return selected, nil
}

func resolveScope(opts *installOptions, canPrompt bool) (registry.Scope, error) {
	if opts.Dir != "" {
		return registry.Scope(opts.Scope), nil
	}

	if opts.ScopeChanged || !canPrompt {
		return registry.Scope(opts.Scope), nil
	}

	var repoName string
	if opts.Remotes != nil {
		if remotes, err := opts.Remotes(); err == nil && len(remotes) > 0 {
			repoName = ghrepo.FullName(remotes[0].Repo)
		}
	}
	idx, err := opts.Prompter.Select("Installation scope:", "", registry.ScopeLabels(repoName))
	if err != nil {
		return "", err
	}
	if idx == 0 {
		return registry.ScopeProject, nil
	}
	return registry.ScopeUser, nil
}

func truncateDescription(s string, maxWidth int) string {
	return text.Truncate(maxWidth, text.RemoveExcessiveWhitespace(s))
}

func checkOverwrite(opts *installOptions, skills []discovery.Skill, host *registry.AgentHost, scope registry.Scope, gitRoot, homeDir string, canPrompt bool) ([]discovery.Skill, error) {
	targetDir := opts.Dir
	if targetDir == "" {
		var err error
		targetDir, err = host.InstallDir(scope, gitRoot, homeDir)
		if err != nil {
			return nil, err
		}
	}

	var existing, fresh []discovery.Skill
	for _, s := range skills {
		dir := filepath.Join(targetDir, filepath.FromSlash(s.InstallName()))
		if _, err := os.Stat(dir); err == nil {
			existing = append(existing, s)
		} else {
			fresh = append(fresh, s)
		}
	}

	if len(existing) == 0 {
		return skills, nil
	}

	if opts.Force {
		return skills, nil
	}

	if !canPrompt {
		names := make([]string, len(existing))
		for i, s := range existing {
			names[i] = s.DisplayName()
		}
		return nil, fmt.Errorf("skills already installed: %s (use --force to overwrite)", strings.Join(names, ", "))
	}

	var confirmed []discovery.Skill
	for _, s := range existing {
		prompt := existingSkillPrompt(targetDir, s)
		ok, err := opts.Prompter.Confirm(prompt, false)
		if err != nil {
			return nil, err
		}
		if ok {
			confirmed = append(confirmed, s)
		} else {
			fmt.Fprintf(opts.IO.ErrOut, "Skipping %s\n", s.DisplayName())
		}
	}

	return append(fresh, confirmed...), nil
}

func existingSkillPrompt(targetDir string, incoming discovery.Skill) string {
	skillFile := filepath.Join(targetDir, filepath.FromSlash(incoming.InstallName()), "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return fmt.Sprintf("Skill %q already exists. Overwrite?", incoming.DisplayName())
	}

	result, err := frontmatter.Parse(string(data))
	if err != nil || result.Metadata.Meta == nil {
		return fmt.Sprintf("Skill %q already exists. Overwrite?", incoming.DisplayName())
	}

	owner, _ := result.Metadata.Meta["github-owner"].(string)
	repo, _ := result.Metadata.Meta["github-repo"].(string)
	ref, _ := result.Metadata.Meta["github-ref"].(string)

	if owner != "" && repo != "" {
		source := owner + "/" + repo
		if ref != "" {
			source += "@" + ref
		}
		return fmt.Sprintf("Skill %q already installed from %s. Overwrite?", incoming.DisplayName(), source)
	}

	return fmt.Sprintf("Skill %q already exists. Overwrite?", incoming.DisplayName())
}

func installProgress(io *iostreams.IOStreams, total int) func(done, total int) {
	if total <= 1 {
		return nil
	}
	return func(done, total int) {
		if done == 0 {
			io.StartProgressIndicator()
		} else if done >= total {
			io.StopProgressIndicator()
		}
	}
}

func friendlyDir(dir string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return filepath.Base(dir)
			}
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + dir[len(home):]
	}
	return dir
}

// printFileTree renders a text tree of the on-disk contents of each skill directory.
func printFileTree(w io.Writer, cs *iostreams.ColorScheme, dir string, skillNames []string) {
	if len(skillNames) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, name := range skillNames {
		skillDir := filepath.Join(dir, filepath.FromSlash(name))
		fmt.Fprintf(w, "  %s\n", cs.Bold(name+"/"))
		printTreeDir(w, cs, skillDir, "  ")
	}
}

func printTreeDir(w io.Writer, cs *iostreams.ColorScheme, dir, indent string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(w, "%s%s\n", indent, cs.Muted("(could not read directory)"))
		return
	}
	for i, entry := range entries {
		isLast := i == len(entries)-1
		connector := "├── "
		childIndent := "│   "
		if isLast {
			connector = "└── "
			childIndent = "    "
		}
		name := entry.Name()
		if entry.IsDir() {
			fmt.Fprintf(w, "%s%s%s\n", indent, cs.Muted(connector), cs.Bold(name+"/"))
			printTreeDir(w, cs, filepath.Join(dir, name), indent+cs.Muted(childIndent))
		} else {
			fmt.Fprintf(w, "%s%s%s\n", indent, cs.Muted(connector), name)
		}
	}
}

// printReviewHint warns the user to review installed skills and suggests preview commands.
func printReviewHint(w io.Writer, cs *iostreams.ColorScheme, repo string, skillNames []string) {
	if len(skillNames) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s Skills may contain prompt injections or malicious scripts.\n", cs.WarningIcon())
	if repo == "" {
		fmt.Fprintln(w, "  Review the installed files before use.")
		return
	}
	fmt.Fprintln(w, "  Review installed content before use:")
	fmt.Fprintln(w)
	for _, name := range skillNames {
		fmt.Fprintf(w, "    gh skills preview %s %s\n", repo, name)
	}
	fmt.Fprintln(w)
}
