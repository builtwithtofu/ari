package main

import (
	"os"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/fakeharness"
)

func main() { os.Exit(fakeharness.Runner{}.Run(os.Args)) }
