## tflib (unreleased core change, pending commit in the engine repo)

Add package tflib: copy lib/tf/*.tf (unmodified, from /Users/rock/mush/tinyfugue/lib/tf) into tflib/lib/, plus tflib.go and tflib_test.go from this directory.
API: tflib.FS, Extract(dir), CacheDir(), Setup(in, dir), Version(). The engine loads library files from disk, so clients extract to a cache dir.
CORE-CHANGES.md entry: "Added tflib: the real tf library embedded with go:embed; no change to existing packages."
