package cmdutil

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func MinimumArgs(n int, msg string) cobra.PositionalArgs {
	if msg == "" {
		return cobra.MinimumNArgs(1)
	}

	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return FlagErrorf("%s", msg)
		}
		return nil
	}
}

func ExactArgs(n int, msg string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return FlagErrorf("too many arguments")
		}

		if len(args) < n {
			return FlagErrorf("%s", msg)
		}

		return nil
	}
}

func NoArgsQuoteReminder(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return nil
	}

	errMsg := fmt.Sprintf("unknown argument %q", args[0])
	if len(args) > 1 {
		errMsg = fmt.Sprintf("unknown arguments %q", args)
	}

	hasValueFlag := false
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Value.Type() != "bool" {
			hasValueFlag = true
		}
	})

	if hasValueFlag {
		errMsg += "; please quote all values that have spaces"
	}

	return FlagErrorf("%s", errMsg)
}

func GlobPaths(patterns []string) ([]string, error) {
	expansions := []string{}

	for _, pattern := range patterns {
		if pattern == "-" || strings.Contains(pattern, "#") {
			expansions = append(expansions, pattern)
		} else {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", pattern, err)
			}
			if len(matches) == 0 {
				return []string{}, fmt.Errorf("no matches found for `%s`", pattern)
			}
			expansions = append(expansions, matches...)
		}
	}

	return expansions, nil
}
