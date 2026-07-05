// Package site は TD site 名(us01, ap01 など)のバリデーションを提供する。
package site

import (
	"fmt"
	"regexp"
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
