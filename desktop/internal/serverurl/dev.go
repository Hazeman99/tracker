//go:build dev

package serverurl

// Dev builds (`-tags dev`, used by `make desktop-dev`) may talk to a local
// API over plain HTTP.
const allowInsecureLocalhost = true
