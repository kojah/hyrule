package hyrule

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/kojah/hyrule/internal/gogen"
)

// Version identifies the compiler release and its deterministic output contract.
const Version = "0.2.0"

// provenance keeps publication ownership compatible with previous releases.
// Paths are relative to the configuration directory, independent of checkout.
func provenance(source []byte, config, target string) []byte {
	quote := func(value string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strconv.Quote(value), "$", "\\$"), "`", "\\`")
	}
	header := fmt.Sprintf("%s// Hyrule v%s. Regenerate from the configuration directory:\n//   go run github.com/kojah/hyrule/cmd/hyrule@v%s generate --config %s --target %s\n", gogen.Header, Version, Version, quote(config), quote(target))
	return append([]byte(header), bytes.TrimPrefix(source, []byte(gogen.Header))...)
}
