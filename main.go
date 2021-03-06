package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/tools/imports"
	"k8s.io/gengo/types"

	"github.com/iancoleman/strcase"
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

	pkg, topTypes, err := codegen.LoadStarlarkGenTypes(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	strcase.ConfigureAcronym("UIButtons", "uiButtons")
	strcase.ConfigureAcronym("UIButton", "uiButton")

	buf := bytes.NewBuffer(nil)
	err = codegen.WritePreamble(pkg, buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	memberTypes, err := codegen.FindStructMembers(topTypes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	registerTypes := append(append([]*types.Type{}, topTypes...), memberTypes...)
	err = codegen.WriteStarlarkRegistrationFunc(registerTypes, pkg, buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, t := range topTypes {
		err := codegen.WriteStarlarkAPIObjectFunction(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	for _, t := range memberTypes {
		err = codegen.WriteStarlarkStructFunction(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		err = codegen.WriteStarlarkStructListFunction(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// gofmt
	result, err := imports.Process("", buf.Bytes(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Problem gofmting output: %v\n", err)

		// If we have a formatting error, we should still treat
		// this as success and write to the file anyway.
		// The user will see an error downstream when they
		// try to compile the code, and giving them the code
		// makes it easier to see what went wrong.
		result = buf.Bytes()
	}

	out, outName, err := codegen.OpenOutputFile(args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	_, err = out.Write(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote output to %s\n", outName)

	closer, ok := out.(io.Closer)
	if ok {
		_ = closer.Close()
	}
}
