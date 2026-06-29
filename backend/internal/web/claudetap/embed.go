package claudetap

import "embed"

// Assets contains the vendored claude-tap viewer template and static assets.
//
//go:embed viewer.html viewer_i18n.json viewer_assets/viewer.css viewer_assets/*.js
var Assets embed.FS
