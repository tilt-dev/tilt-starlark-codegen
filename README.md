# tilt-starlark-codegen

Generates starlark functions based on Kubernetes-style API models

This repo is intended for:

1) Developers who want to add new data models to Tilt, then auto-generate functions for the
Tiltfile DSL.

2) Developers who want to see a simple example of Go-based code generators based
on Kubernetes' [gengo](https://pkg.go.dev/k8s.io/gengo)

## Usage

```
# Sample input and output
tilt-starlark-codegen ./path/to/input ./path/to/output

# In the Tilt codebase
tilt-starlark-codegen ./pkg/apis/core/v1alpha1 ./internal/tiltfile/v1alpha1
```
