package pulse

import _ "embed"

//go:embed pulse_dashboard.html
var DashboardHTML []byte

// NoncePlaceholder is replaced with the per-request CSP nonce before the page
// is served, so the inline <script> satisfies the script-src nonce policy.
const NoncePlaceholder = "__CSP_NONCE_VALUE__"
