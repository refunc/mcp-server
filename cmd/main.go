package main

import (
	"context"
	"runtime"

	"github.com/Arvintian/go-utils/cmdutil"
	"github.com/Arvintian/go-utils/cmdutil/flagtools"
	"github.com/Arvintian/go-utils/cmdutil/pflagenv"
	"github.com/refunc/mcp-server/pkg/mcpserver"
	"github.com/refunc/mcp-server/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	_ "github.com/refunc/refunc/pkg/env"
	"github.com/refunc/refunc/pkg/utils/cmdutil/sharedcfg"
)

var config struct {
	Addr      string
	Namespace string
}

func main() {
	runtime.GOMAXPROCS(16 * runtime.NumCPU())
	klog.CopyStandardLogTo("INFO")
	defer klog.Flush()

	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Start refunc mcp server.",
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sc := sharedcfg.New(ctx, config.Namespace)

			sc.AddController(func(cfg sharedcfg.Configs) sharedcfg.Runner {
				r, err := mcpserver.NewRefuncMCPServer(cfg, config.Addr, ctx.Done())
				if err != nil {
					klog.Fatalln("create refunc mcp server fatal")
				}
				return r
			})

			go func() {
				klog.Infof("Refunc MCP Server version: %s\n", version.Version)
				sc.Run(ctx.Done())
			}()

			klog.Infof(`Received signal "%v", exiting...`, <-cmdutil.GetSysSig())

		},
	}

	cmd.Flags().StringVar(&config.Addr, "addr", "0.0.0.0:9000", "ListenAndServe Address.")
	cmd.Flags().StringVarP(&config.Namespace, "namespace", "n", "", "The scope of namepsace to manipulate.")
	flagtools.BindFlags(cmd.PersistentFlags())

	// set global flags using env
	pflagenv.ParseSet(pflagenv.Prefix, cmd.PersistentFlags())

	if err := cmd.Execute(); err != nil {
		klog.Fatal(err)
	}

}
