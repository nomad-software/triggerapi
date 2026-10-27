// Package appicon holds the program's icon, built into the binary so that the
// program needs nothing beside it.
package appicon

import _ "embed"

// Size is the width and height of PNG.
const Size = 256

// PNG is the program's icon, a 256×256 PNG drawn by the ui package (make
// icons).
//
//go:embed appicon.png
var PNG []byte
