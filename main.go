package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tilt-dev/tilt-starlark-codegen/internal/codegen"
)

func main() {
	args := os.Args

	if len(args) != 3 {
		fmt.Fprintf(os.Stderr, `%s: requires exactly 2 arguments.

Usage:
# Sample input and output
tilt-starlark-codegen ./path/to/input ./path/to/output

# In the Tilt codebase
tilt-starlark-codegen ./pkg/apis/core/v1alpha1 ./internal/tiltfile/v1alpha1

# Dry run (print to stdout)
tilt-starlark-codegen ./pkg/apis/core/v1alpha1 -
`, filepath.Base(args[0]))
		os.Exit(1)
	}

	pkg, types, err := codegen.LoadStarlarkGenTypes(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}

	out, err := codegen.OpenOutputFile(args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}

	for _, t := range types {
		err := codegen.WriteStarlarkFunction(t, pkg, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v", err)
			os.Exit(1)
		}
	}

	err = codegen.CloseOutputFile(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}
}