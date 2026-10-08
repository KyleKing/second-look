package versions

// Forget drops one package's memory of an answer, which is how a test stands
// up a second process's view of a shared disk store.
func Forget(eco, name string) { mem.Delete(eco + "/" + name) }
