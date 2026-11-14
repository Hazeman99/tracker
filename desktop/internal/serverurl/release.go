//go:build !dev

package serverurl

// Release builds accept https:// only.
const allowInsecureLocalhost = false
