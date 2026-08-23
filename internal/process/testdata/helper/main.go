// Command helper is a tiny portable test double used by the process
// package tests. It exists so the suite can exercise exit codes, output
// streams, working directories, timeouts, and output bounding without
// assuming any shell (bash, PowerShell, ...) is installed on the host.
//
// It lives under testdata so the Go toolchain ignores it as a package
// of its own during normal builds and ./... walks.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	stdout := flag.String("stdout", "", "text to print to stdout")
	stderr := flag.String("stderr", "", "text to print to stderr")
	fail := flag.Int("fail", 0, "exit with this code after printing")
	sleep := flag.Duration("sleep", 0, "sleep for this long before exiting")
	write := flag.String("write", "", "create this file (relative to cwd) with fixed content")
	spam := flag.Bool("spam", false, "write far more than the capture bound to stdout")
	head := flag.String("head-marker", "HEADMARKER", "marker expected at the head of spammed output")
	tail := flag.String("tail-marker", "TAILMARKER", "marker expected at the tail of spammed output")
	flag.Parse()

	if *stdout != "" {
		fmt.Println(*stdout)
	}
	if *stderr != "" {
		fmt.Fprintln(os.Stderr, *stderr)
	}
	if *write != "" {
		if err := os.WriteFile(*write, []byte("written by lato test helper\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write failed:", err)
			os.Exit(1)
		}
	}
	if *spam {
		const line = "0123456789012345678901234567890123456789\n" // 41 bytes
		total := 8000 * len(line)                                 // ~320 KiB, past the 128 KiB capture bound
		var written int
		for written < total {
			if written == 0 {
				fmt.Println(*head)
			}
			if total-written <= 2*len(line) {
				fmt.Println(*tail)
			}
			fmt.Print(line)
			written += len(line)
		}
	}
	if *sleep > 0 {
		time.Sleep(*sleep)
	}
	os.Exit(*fail)
}
