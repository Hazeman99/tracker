package main

// contentSecurityPolicy allows only the app's own bundled scripts, styles,
// fonts and images, and no remote connections of any kind.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"font-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
