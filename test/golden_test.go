package test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGolden(t *testing.T) {
	out := bytes.NewBuffer(nil)
	outErr := bytes.NewBuffer(nil)
	cmd := exec.Command("go", "run", "../main.go", "./example", "-")
	cmd.Stdout = out
	cmd.Stderr = outErr

	err := cmd.Run()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", outErr.String())
		t.Fatalf("codegen failed: %v", err)
	}

	log.Println(os.Getwd())
	goldenPath := filepath.Join("golden", "master.txt")
	write := os.Getenv("WRITE_GOLDEN_MASTER") != ""
	if write {
		err = ioutil.WriteFile(goldenPath, out.Bytes(), os.FileMode(0644))
		require.NoError(t, err)
		fmt.Println("GENERATED GOLDEN MASTER")
		return
	}

	golden, err := ioutil.ReadFile(goldenPath)
	assert.NoError(t, err)
	assert.Equal(t, string(golden), out.String())
}
