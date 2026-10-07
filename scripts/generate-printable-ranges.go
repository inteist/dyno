//go:build ignore

// Run from the repository root: go run scripts/generate-printable-ranges.go.
// Go is needed to regenerate this Unicode snapshot, not to run the installer.
package main

import (
	"fmt"
	"os"
	"unicode"
)

func main() {
	f, err := os.Create("scripts/printable-ranges.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "# Generated from Go unicode.IsPrint; Unicode", unicode.Version)
	start := -1
	for cp := 0; cp <= unicode.MaxRune+1; cp++ {
		printable := cp <= unicode.MaxRune && unicode.IsPrint(rune(cp))
		if printable && start < 0 {
			start = cp
		}
		if !printable && start >= 0 {
			fmt.Fprintln(f, start, cp-1)
			start = -1
		}
	}
}
