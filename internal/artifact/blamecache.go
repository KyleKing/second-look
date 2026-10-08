package artifact

// blameKind is the directory one file's blame rollup is cached under. Its key
// is the caller's hash of the file's old-side blob and the ranges asked, not a
// head: blame of an unchanged blob stands when a push lands elsewhere, where a
// head key would invalidate on every one.
const blameKind = "blame"

// SaveBlame caches one file's blame rollup under the caller's key.
func SaveBlame(root, key string, v any) error {
	return saveCached(root, blameKind, key, "blame", v)
}

// LoadBlame reads a cached rollup back into v, and reports nothing for a file
// whose blame was never asked.
func LoadBlame(root, key string, v any) error {
	return loadCached(root, blameKind, key, "blame", v)
}
