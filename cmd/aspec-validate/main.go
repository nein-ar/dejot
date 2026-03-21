package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nein-ar/dejot/aspec"
)

func main() {
	verboseFlag := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: aspec-validate [options] <file.aspec>\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -v          Verbose output\n")
		os.Exit(1)
	}

	inputPath := args[0]

	expanded, params, err := aspec.Expand(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("ASPEC expansion successful\n")
		fmt.Printf("  Expanded size: %d bytes\n", len(expanded))
		fmt.Printf("  Parameters: %d\n", len(params))
	}

	fmt.Println("ASPEC validation successful")
	os.Exit(0)
}
