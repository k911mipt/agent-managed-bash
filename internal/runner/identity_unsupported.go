//go:build !linux && !darwin

package runner

func VerifyProcessIdentity(_ int, _ string) (bool, error) {
	return false, ErrUnsupported
}
