// Command scoutbook-xls generates an XLSX progress report for a Cub Scout den
// from the unofficial advancements.scouting.org API.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pkaeding/scoutbook-xls/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd(cmd.Run)
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
