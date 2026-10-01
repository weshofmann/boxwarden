//go:build (!darwin && !linux) || !(n1diagnostic || n1clipboarddiagnostic) || n1candidate

package pathmeta

import "os"

func CheckQualificationAncestor(p string, f os.FileInfo, i Inspector, _ func() error) error {
	return Check(p, f, i)
}
