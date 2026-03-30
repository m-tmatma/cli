package skills

import (
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmdSkills returns the top-level "skills" command.
func NewCmdSkills(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills <command>",
		Short:   "Install and manage agent skills",
		Long:    "Install and manage agent skills from GitHub repositories.",
		GroupID: "core",
	}

	return cmd
}
