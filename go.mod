module github.com/itsnotyoutoday/gtf

go 1.24

// TEMPORARY: the engine release this needs (v0.0.5+) is not published yet, so build against the
// sibling checkout. Delete this line when the engine tag is on GitHub.
replace github.com/itsnotyoutoday/gtfparser => ../gtfparser

require (
	github.com/itsnotyoutoday/gtfparser v0.0.5
	golang.org/x/term v0.30.0
)

require (
	golang.org/x/image v0.24.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
)
