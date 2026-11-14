// Command gtf is a TinyFugue-compatible client. This is the batch-mode
// skeleton: it loads script files and runs commands, printing echo output.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/itsnotyoutoday/gtf/gtfparser"
)

type stdHost struct{ w *bufio.Writer }

func (h *stdHost) Echo(s string)         { fmt.Fprintln(h.w, s) }
func (h *stdHost) Send(w, s string) bool { fmt.Fprintln(h.w, "[send] "+s); return true }

func main() {
	file := flag.String("f", "", "script file to load")
	lib := flag.String("L", "", "library directory")
	nolib := flag.Bool("n", false, "do not load stdlib.tf")
	flag.Parse()
	h := &stdHost{bufio.NewWriter(os.Stdout)}
	defer h.w.Flush()
	in := gtfparser.New(h)
	in.Warn = func(m string) { fmt.Fprintln(os.Stderr, "% "+m) }
	if *lib != "" {
		in.LibDirs = []string{*lib}
	}
	if *lib != "" && !*nolib {
		if err := in.LoadFile(*lib + "/stdlib.tf"); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	if *file != "" {
		if err := in.LoadFile(*file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
