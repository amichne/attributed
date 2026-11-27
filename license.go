// Package attributed carries the license notices that every Attributed
// binary distributes. Attributed derives from Comarch's git-byline, so the
// binary carries both MIT notices.
package attributed

import _ "embed"

//go:embed LICENSE
var license string

//go:embed LICENSE.upstream
var upstreamLicense string

// Notices returns the Attributed MIT notice followed by the git-byline MIT
// notice, separated by a heading that names the upstream source.
func Notices() string {
	return license + "\n---\n\nAttributed includes software derived from git-byline:\n" +
		"https://github.com/comarch/git-byline\n\n" + upstreamLicense
}
