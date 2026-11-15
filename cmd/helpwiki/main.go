// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Command helpwiki builds GitHub wiki pages from gtf's help topics (help/topics/*.md). It only writes files into -out; publishing the wiki
// (committing and pushing the gtf.wiki repository) is a separate, deliberate step.
//
//	go run ./cmd/helpwiki -out build/wiki
//	go run ./cmd/helpwiki -check        validate the topics and links, write nothing
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/itsnotyoutoday/gtf/help"
)

func main() {
	out := flag.String("out", "build/wiki", "directory to write the wiki pages into")
	check := flag.Bool("check", false, "only validate the topics and their links")
	flag.Parse()
	ix, err := help.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpwiki:", err)
		os.Exit(1)
	}
	if errs := ix.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "helpwiki:", e)
		}
		os.Exit(1)
	}
	if *check {
		fmt.Printf("helpwiki: %d topics, all links resolve\n", len(ix.Topics))
		return
	}
	names, err := ix.WriteWiki(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helpwiki:", err)
		os.Exit(1)
	}
	sort.Strings(names)
	fmt.Printf("helpwiki: wrote %d pages to %s\n", len(names), *out)
}
