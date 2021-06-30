package main

import (
	goflag "flag"
	"fmt"
	"os"

	"github.com/dmage/ci-results/indexer"
	"github.com/dmage/ci-results/server"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci-results",
		Short: "CI results provides analytics over CI results",
	}

	cmd.AddCommand(indexer.NewCmdIndexer())
	cmd.AddCommand(server.NewCmdServer())

	return cmd
}

func main() {
	rootCmd := NewCmd()

	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
