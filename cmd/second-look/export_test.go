package main

// RefString parses a pull request reference and renders it back, which is both
// halves of the parser without exporting its type.
func RefString(s string) (string, error) {
	r, err := parseRef(s)
	if err != nil {
		return "", err
	}

	return r.String(), nil
}
