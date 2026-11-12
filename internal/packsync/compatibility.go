package packsync

import "strings"

func samePlatform(a, b Manifest) bool {
	if a.Game != b.Game || a.Loader != b.Loader {
		return false
	}
	left, right := a.LoaderVersion, b.LoaderVersion
	if a.Loader == "forge" {
		left = strings.TrimPrefix(left, a.Game+"-")
		right = strings.TrimPrefix(right, b.Game+"-")
	}
	return left == right
}

func contentFiles(m Manifest, modsOnly bool) map[string]File {
	files := make(map[string]File)
	for _, file := range m.Files {
		if !modsOnly || strings.HasPrefix(file.Path, "mods/") {
			files[file.Path] = file
		}
	}
	return files
}

func sameFiles(a, b Manifest, modsOnly, allowExtraConfigs bool) bool {
	if !samePlatform(a, b) {
		return false
	}
	local, expected := contentFiles(a, modsOnly), contentFiles(b, modsOnly)
	for name, want := range expected {
		got, exists := local[name]
		if !exists || got.SHA256 != want.SHA256 || got.Size != want.Size {
			return false
		}
	}
	for name := range local {
		if _, exists := expected[name]; !exists && (!allowExtraConfigs || strings.HasPrefix(name, "mods/")) {
			return false
		}
	}
	return true
}

// Auxiliary SHA-1/source metadata, names and format revisions do not change the
// actual pack. File identity is its path, SHA-256 and size.
func SameContent(a, b Manifest) bool { return sameFiles(a, b, false, false) }

// Joining requires the same game, loader and executable mods. Configs are copied
// when creating a synchronized instance, but local settings and runtime rewrites
// are never an admission requirement. In particular, logs/caches are not mods.
func Compatible(local, expected Manifest) bool { return sameFiles(local, expected, true, false) }
