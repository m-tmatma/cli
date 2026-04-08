package skills

import (
	"github.com/cli/cli/v2/pkg/cmd/skills/install"
	"github.com/cli/cli/v2/pkg/cmd/skills/preview"
	"github.com/cli/cli/v2/pkg/cmd/skills/publish"
	"github.com/cli/cli/v2/pkg/cmd/skills/search"
	"github.com/cli/cli/v2/pkg/cmd/skills/update"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdSkills returns the top-level "skill" command.
func NewCmdSkills(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skill <command>",
		Short:   "Install and manage agent skills",
		Long:    "Install and manage agent skills from GitHub repositories.",
		Aliases: []string{"skills"},
		GroupID: "core",
	}

	cmd.AddCommand(install.NewCmdInstall(f, nil))
	cmd.AddCommand(preview.NewCmdPreview(f, nil))
	cmd.AddCommand(publish.NewCmdPublish(f, nil))
	cmd.AddCommand(search.NewCmdSearch(f, nil))
	cmd.AddCommand(update.NewCmdUpdate(f, nil))

	return cmd
}
