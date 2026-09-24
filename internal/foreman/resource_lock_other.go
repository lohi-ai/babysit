//go:build !unix && !windows

package foreman

import "fmt"

func lockResources(path string) (func(), error) {
	return nil, fmt.Errorf("foreman resource locking requires Unix or Windows")
}
