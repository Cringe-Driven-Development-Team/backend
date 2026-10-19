// Command apidog экспортирует контракт из Apidog.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/tooling/apidog"
)

func main() {
	out := flag.String("o", "openapi.yaml", "file to write the specification to")
	envFile := flag.String("env-file", ".env", "file with APIDOG_TOKEN and APIDOG_BRANCH_ID")
	flag.Parse()
	if err := apidog.Run(context.Background(), apidog.Options{Out: *out, EnvFile: *envFile}); err != nil {
		fmt.Fprintln(os.Stderr, "apidog:", err)
		os.Exit(1)
	}
}
