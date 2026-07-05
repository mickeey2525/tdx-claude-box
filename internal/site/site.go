// Package site は TD site 名(us01, ap01 など)のバリデーションを提供する。
package site

import (
	"fmt"
	"regexp"
	"strings"
)

// site 名はコンテナ名・ボリューム名・ホスト名の一部になるため保守的に制限する。
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Validate は site 名として妥当か検証する。
func Validate(name string) error {
	if name == "" {
		return fmt.Errorf("site name is required")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid site name %q: must match %s (e.g. us01, ap01)", name, namePattern)
	}
	return nil
}

// knownTDSites は公式 TD site(tdx 2026.6.5 のヘルプに列挙されているもの)。
var knownTDSites = []string{"us01", "ap01", "ap02", "eu01"}

// DeriveTDSite は box 名から TD site を推定する。
// "us01-7060" のような "<公式site>-<suffix>" 形式なら prefix を返し、
// それ以外は box 名をそのまま返す(--site で常に上書き可能)。
func DeriveTDSite(box string) string {
	for _, s := range knownTDSites {
		if box == s || strings.HasPrefix(box, s+"-") {
			return s
		}
	}
	return box
}
